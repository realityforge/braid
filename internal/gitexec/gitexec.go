package gitexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	DefaultGitExecutable = "git"
	MinimumGitVersion    = "2.39.0"
)

type Runner struct {
	Executable string
	PrefixArgs []string
	WorkDir    string
	Env        map[string]string
	Verbose    bool
	Trace      io.Writer
}

type Result struct {
	Command  []string
	GitArgs  []string
	WorkDir  string
	Stdout   string
	Stderr   string
	ExitCode int
}

type ExitError struct {
	Result Result
}

func (e *ExitError) Error() string {
	if strings.TrimSpace(e.Result.Stderr) != "" {
		return fmt.Sprintf("%s failed with exit code %d: %s", FormatArgv(e.Result.Command), e.Result.ExitCode, strings.TrimSpace(e.Result.Stderr))
	}
	return fmt.Sprintf("%s failed with exit code %d", FormatArgv(e.Result.Command), e.Result.ExitCode)
}

type VersionTooLowError struct {
	Actual   string
	Required string
}

func (e *VersionTooLowError) Error() string {
	return fmt.Sprintf("git version too low: %s. %s needed", e.Actual, e.Required)
}

type Git struct {
	Runner Runner
}

type TreeItem struct {
	Mode string
	Type string
	Hash string
}

func New(workDir string, verbose bool, trace io.Writer) Git {
	return Git{
		Runner: Runner{
			WorkDir: workDir,
			Verbose: verbose,
			Trace:   trace,
		},
	}
}

func (g Git) Run(ctx context.Context, args ...string) (Result, error) {
	return g.Runner.Run(ctx, args...)
}

func (g Git) RunOK(ctx context.Context, args ...string) (Result, error) {
	return g.Runner.RunOK(ctx, args...)
}

func (g Git) Output(ctx context.Context, args ...string) (string, error) {
	result, err := g.RunOK(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (g Git) Version(ctx context.Context) (string, error) {
	result, err := g.RunOK(ctx, "--version")
	if err != nil {
		return "", err
	}
	version := ParseVersion(result.Stdout)
	if version == "" {
		return "", fmt.Errorf("could not parse git version from %q", strings.TrimSpace(result.Stdout))
	}
	return version, nil
}

func (g Git) RequireVersion(ctx context.Context, required string) error {
	actual, err := g.Version(ctx)
	if err != nil {
		return err
	}
	if CompareVersions(actual, required) < 0 {
		return &VersionTooLowError{Actual: actual, Required: required}
	}
	return nil
}

func (g Git) IsInsideWorkTree(ctx context.Context) (bool, error) {
	out, err := g.Output(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, err
	}
	return out == "true", nil
}

func (g Git) RelativeWorkingDir(ctx context.Context) (string, error) {
	return g.Output(ctx, "rev-parse", "--show-prefix")
}

func (g Git) RepoFilePath(ctx context.Context, path string) (string, error) {
	return g.Output(ctx, "rev-parse", "--git-path", path)
}

func (g Git) ConfigGet(ctx context.Context, args ...string) (string, bool, error) {
	gitArgs := append([]string{"config"}, args...)
	result, err := g.RunOK(ctx, gitArgs...)
	if err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) && exitErr.Result.ExitCode == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(result.Stdout), true, nil
}

func (g Git) ConfigSet(ctx context.Context, args ...string) error {
	gitArgs := append([]string{"config"}, args...)
	_, err := g.RunOK(ctx, gitArgs...)
	return err
}

func (g Git) RevParse(ctx context.Context, rev string) (string, error) {
	return g.Output(ctx, "rev-parse", rev)
}

func (g Git) Head(ctx context.Context) (string, error) {
	return g.RevParse(ctx, "HEAD")
}

func (g Git) StatusPorcelain(ctx context.Context) (string, error) {
	result, err := g.RunOK(ctx, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (g Git) RemoteURL(ctx context.Context, remote string) (string, bool, error) {
	result, err := g.RunOK(ctx, "config", "--get", "remote."+remote+".url")
	if err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) && exitErr.Result.ExitCode == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(result.Stdout), true, nil
}

func (g Git) RemoteAdd(ctx context.Context, remote, url string) error {
	_, err := g.RunOK(ctx, "remote", "add", remote, url)
	return err
}

func (g Git) RemoteRemove(ctx context.Context, remote string) error {
	_, err := g.RunOK(ctx, "remote", "rm", remote)
	return err
}

func (g Git) Fetch(ctx context.Context, args ...string) error {
	gitArgs := append([]string{"fetch"}, args...)
	_, err := g.RunOK(ctx, gitArgs...)
	return err
}

func (g Git) CloneMirror(ctx context.Context, url, dir string) error {
	_, err := g.RunOK(ctx, "clone", "--mirror", url, dir)
	return err
}

func (g Git) LsRemote(ctx context.Context, args ...string) (string, error) {
	gitArgs := append([]string{"ls-remote"}, args...)
	result, err := g.RunOK(ctx, gitArgs...)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (g Git) LsTreeItem(ctx context.Context, treeish, path string) (TreeItem, error) {
	out, err := g.Output(ctx, "ls-tree", treeish, path)
	if err != nil {
		return TreeItem{}, err
	}
	meta, _, ok := strings.Cut(out, "\t")
	if !ok {
		return TreeItem{}, fmt.Errorf("no tree item exists at %q in %s", path, treeish)
	}
	fields := strings.Fields(meta)
	if len(fields) != 3 {
		return TreeItem{}, fmt.Errorf("could not parse tree item %q", out)
	}
	item := TreeItem{Mode: fields[0], Type: fields[1], Hash: fields[2]}
	if item.Type != "tree" && item.Type != "blob" {
		return TreeItem{}, fmt.Errorf("tree item %q is %s, not tree or blob", path, item.Type)
	}
	return item, nil
}

func (g Git) ReadTreePrefix(ctx context.Context, prefix, treeish string, updateWorktree bool) error {
	mode := "-i"
	if updateWorktree {
		mode = "-u"
	}
	_, err := g.RunOK(ctx, "read-tree", "--prefix="+prefix, mode, treeish)
	return err
}

func (g Git) ReadTreeIndexMerge(ctx context.Context, treeish string) error {
	_, err := g.RunOK(ctx, "read-tree", "-im", treeish)
	return err
}

func (g Git) UpdateIndexCacheInfo(ctx context.Context, mode, hash, path string) error {
	_, err := g.RunOK(ctx, "update-index", "--add", "--cacheinfo", mode+","+hash+","+path)
	return err
}

func (g Git) RemoveCachedRecursive(ctx context.Context, path string) error {
	_, err := g.RunOK(ctx, "rm", "-r", "--cached", "--", path)
	return err
}

func (g Git) RemoveRecursive(ctx context.Context, path string) error {
	_, err := g.RunOK(ctx, "rm", "-r", "--", path)
	return err
}

func (g Git) CheckoutIndex(ctx context.Context, path string) error {
	_, err := g.RunOK(ctx, "checkout-index", "--", path)
	return err
}

func (g Git) Add(ctx context.Context, path string) error {
	_, err := g.RunOK(ctx, "add", "--", path)
	return err
}

func (g Git) CommitMessage(ctx context.Context, message string) (bool, error) {
	result, err := g.Run(ctx, "commit", "--no-verify", "-m", message)
	if err != nil {
		return false, err
	}
	if result.ExitCode == 0 {
		return true, nil
	}
	if strings.Contains(result.Stdout, "nothing") && strings.Contains(result.Stdout, "to commit") {
		return false, nil
	}
	return false, &ExitError{Result: result}
}

func (g Git) ResetHard(ctx context.Context, target string) error {
	_, err := g.RunOK(ctx, "reset", "--hard", target)
	return err
}

func (g Git) Init(ctx context.Context) error {
	_, err := g.RunOK(ctx, "init")
	return err
}

func (g Git) UpdateRef(ctx context.Context, args ...string) error {
	gitArgs := append([]string{"update-ref"}, args...)
	_, err := g.RunOK(ctx, gitArgs...)
	return err
}

func (g Git) ReadTreeUpdateMerge(ctx context.Context, treeish string) error {
	_, err := g.RunOK(ctx, "read-tree", "-um", treeish)
	return err
}

func (g Git) CommitVerbose(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	_, err := g.Runner.RunInteractiveOK(ctx, stdin, stdout, stderr, "commit", "-v")
	return err
}

func (g Git) Push(ctx context.Context, args ...string) error {
	gitArgs := append([]string{"push"}, args...)
	_, err := g.RunOK(ctx, gitArgs...)
	return err
}

func (g Git) MakeTreeWithItem(ctx context.Context, itemPath string, item TreeItem) (string, error) {
	return g.MakeTreeWithItemIn(ctx, "", itemPath, item)
}

func (g Git) MakeTreeWithItemIn(ctx context.Context, mainContent, itemPath string, item TreeItem) (string, error) {
	dir, err := os.MkdirTemp("", "braid-index")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	tempGit := g
	tempGit.Runner.Env = copyEnv(g.Runner.Env)
	tempGit.Runner.Env["GIT_INDEX_FILE"] = filepath.Join(dir, "index")

	if mainContent != "" && itemPath != "" {
		// Use a temporary index to compose synthetic trees without disturbing the caller's index.
		if err := tempGit.ReadTreeIndexMerge(ctx, mainContent); err != nil {
			return "", err
		}
		if err := tempGit.RemoveCachedRecursive(ctx, itemPath); err != nil {
			return "", err
		}
	}

	switch item.Type {
	case "blob":
		if err := tempGit.UpdateIndexCacheInfo(ctx, item.Mode, item.Hash, itemPath); err != nil {
			return "", err
		}
	case "tree":
		if err := tempGit.ReadTreePrefix(ctx, itemPath, item.Hash, false); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("tree item type %q is not supported", item.Type)
	}
	return tempGit.Output(ctx, "write-tree")
}

func (g Git) MergeTrees(ctx context.Context, env map[string]string, baseTreeish, localTreeish, remoteTreeish string) (string, error) {
	mergeGit := g
	mergeGit.Runner.Env = copyEnv(g.Runner.Env)
	for key, value := range env {
		mergeGit.Runner.Env[key] = value
	}

	// merge-recursive gives Braid a three-way tree merge without checking out either synthetic tree.
	result, runErr := mergeGit.Run(ctx, "merge-recursive", baseTreeish, "--", localTreeish, remoteTreeish)
	if runErr != nil {
		return result.Stdout, runErr
	}
	if result.ExitCode != 0 {
		return result.Stdout, &ExitError{Result: result}
	}
	return result.Stdout, nil
}

func (g Git) Diff(ctx context.Context, args ...string) (string, error) {
	gitArgs := append([]string{"diff"}, args...)
	result, err := g.RunOK(ctx, gitArgs...)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (g Git) LsFiles(ctx context.Context, path string) (string, error) {
	result, err := g.RunOK(ctx, "ls-files", "--", path)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (r Runner) RunOK(ctx context.Context, args ...string) (Result, error) {
	result, err := r.Run(ctx, args...)
	if err != nil {
		return result, err
	}
	if result.ExitCode != 0 {
		return result, &ExitError{Result: result}
	}
	return result, nil
}

func (r Runner) Run(ctx context.Context, args ...string) (Result, error) {
	cmd, result, err := r.command(ctx, args...)
	if err != nil {
		return result, err
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.ExitCode = exitCode(err)

	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return result, err
	}
	return result, nil
}

func (r Runner) RunInteractiveOK(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) (Result, error) {
	result, err := r.RunInteractive(ctx, stdin, stdout, stderr, args...)
	if err != nil {
		return result, err
	}
	if result.ExitCode != 0 {
		return result, &ExitError{Result: result}
	}
	return result, nil
}

func (r Runner) RunInteractive(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) (Result, error) {
	cmd, result, err := r.command(ctx, args...)
	if err != nil {
		return result, err
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()
	result.ExitCode = exitCode(err)

	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return result, err
	}
	return result, nil
}

func (r Runner) command(ctx context.Context, args ...string) (*exec.Cmd, Result, error) {
	executable := r.executable()
	commandArgs := append([]string{}, r.PrefixArgs...)
	commandArgs = append(commandArgs, args...)

	result := Result{
		Command:  append([]string{executable}, commandArgs...),
		GitArgs:  append([]string{}, args...),
		WorkDir:  r.WorkDir,
		ExitCode: -1,
	}

	if r.Verbose {
		trace := r.Trace
		if trace == nil {
			trace = io.Discard
		}
		if _, err := fmt.Fprintf(trace, "Braid: Executing %s in %s\n", FormatArgv(append([]string{DefaultGitExecutable}, args...)), displayDir(r.WorkDir)); err != nil {
			return nil, result, err
		}
	}

	// Braid intentionally delegates Git operations to the user's Git executable.
	// Arguments are passed as argv rather than through a shell, so shell
	// metacharacters are not interpreted here. The trust boundary is the resolved
	// executable, the user's Git configuration, and Git's handling of the target
	// repository.
	cmd := exec.CommandContext(ctx, executable, commandArgs...)
	cmd.Dir = r.WorkDir
	cmd.Env = r.environment()
	return cmd, result, nil
}

func (r Runner) executable() string {
	if r.Executable != "" {
		return r.Executable
	}
	return DefaultGitExecutable
}

func (r Runner) environment() []string {
	env := os.Environ()
	env = upsertEnv(env, "LANG", "C")
	if len(r.Env) == 0 {
		return env
	}

	keys := make([]string, 0, len(r.Env))
	for key := range r.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = upsertEnv(env, key, r.Env[key])
	}
	return env
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func copyEnv(env map[string]string) map[string]string {
	copied := make(map[string]string, len(env)+1)
	for key, value := range env {
		copied[key] = value
	}
	return copied
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func displayDir(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

func FormatArgv(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = strconv.Quote(arg)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func ParseVersion(output string) string {
	idx := strings.Index(output, "version")
	if idx == -1 {
		return ""
	}
	rest := strings.TrimSpace(output[idx+len("version"):])
	if rest == "" {
		return ""
	}
	return strings.Fields(rest)[0]
}

func CompareVersions(actual, required string) int {
	actualParts := versionParts(actual)
	requiredParts := versionParts(required)
	length := len(actualParts)
	if len(requiredParts) > length {
		length = len(requiredParts)
	}
	for i := 0; i < length; i++ {
		actualPart := 0
		if i < len(actualParts) {
			actualPart = actualParts[i]
		}
		requiredPart := 0
		if i < len(requiredParts) {
			requiredPart = requiredParts[i]
		}
		if actualPart < requiredPart {
			return -1
		}
		if actualPart > requiredPart {
			return 1
		}
	}
	return 0
}

func versionParts(version string) []int {
	var parts []int
	for _, token := range strings.FieldsFunc(version, func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == '+'
	}) {
		numeric := leadingDigits(token)
		if numeric == "" {
			continue
		}
		part, err := strconv.Atoi(numeric)
		if err == nil {
			parts = append(parts, part)
		}
	}
	return parts
}

func leadingDigits(token string) string {
	for i, r := range token {
		if !unicode.IsDigit(r) {
			return token[:i]
		}
	}
	return token
}
