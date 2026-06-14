package gitexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	DefaultGitExecutable = "git"
	MinimumGitVersion    = "2.43.0"
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

func (g Git) RevParse(ctx context.Context, rev string) (string, error) {
	return g.Output(ctx, "rev-parse", rev)
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
		fmt.Fprintf(trace, "Braid: Executing %s in %s\n", FormatArgv(append([]string{DefaultGitExecutable}, args...)), displayDir(r.WorkDir))
	}

	cmd := exec.CommandContext(ctx, executable, commandArgs...)
	cmd.Dir = r.WorkDir
	cmd.Env = r.environment()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.ExitCode = exitCode(err)

	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return result, err
	}
	return result, nil
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
