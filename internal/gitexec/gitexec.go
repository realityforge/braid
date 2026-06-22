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

type TreeItemNotFoundError struct {
	Treeish string
	Path    string
}

func (e *TreeItemNotFoundError) Error() string {
	return fmt.Sprintf("no tree item exists at %q in %s", e.Path, e.Treeish)
}

func IsTreeItemNotFound(err error) bool {
	var notFound *TreeItemNotFoundError
	return errors.As(err, &notFound)
}

type Commit struct {
	Hash    string
	Subject string
	Message string
}

type MergeTreeResult struct {
	Tree          string
	ConflictPaths []string
	Details       string
}

type StashEntry struct {
	OID     string
	Message string
}

type StashListEntry struct {
	Selector string
	OID      string
	Subject  string
}

type StashLookupError struct {
	Entry   StashEntry
	Matches []StashListEntry
}

func (e *StashLookupError) Error() string {
	if len(e.Matches) == 0 {
		return fmt.Sprintf("could not find saved stash entry %s", e.Entry.OID)
	}
	return fmt.Sprintf("found multiple saved stash entries for %s", e.Entry.OID)
}

var blockingOperationSentinels = []string{
	"MERGE_HEAD",
	"CHERRY_PICK_HEAD",
	"REVERT_HEAD",
	"REBASE_HEAD",
	"rebase-merge",
	"rebase-apply",
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
	result, err := g.Run(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, err
	}
	if result.ExitCode != 0 {
		if result.ExitCode == 128 && strings.Contains(result.Stderr, "not a git repository") {
			return false, nil
		}
		return false, &ExitError{Result: result}
	}
	return strings.TrimSpace(result.Stdout) == "true", nil
}

func (g Git) RelativeWorkingDir(ctx context.Context) (string, error) {
	return g.Output(ctx, "rev-parse", "--show-prefix")
}

func (g Git) WorkTreeRoot(ctx context.Context) (string, error) {
	return g.Output(ctx, "rev-parse", "--show-toplevel")
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

func (g Git) CoreCommentChar(ctx context.Context) (string, bool, error) {
	result, err := g.RunOK(ctx, "config", "--get", "core.commentChar")
	if err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) && exitErr.Result.ExitCode == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimRight(result.Stdout, "\r\n"), true, nil
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

func (g Git) StatusPorcelainPathspecs(ctx context.Context, pathspecs ...string) (string, error) {
	args := []string{"status", "--porcelain", "--"}
	args = append(args, pathspecs...)
	result, err := g.RunOK(ctx, args...)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (g Git) StatusPorcelainPathspecsWithIgnored(ctx context.Context, pathspecs ...string) (string, error) {
	args := []string{"status", "--porcelain", "--ignored", "--"}
	args = append(args, pathspecs...)
	result, err := g.RunOK(ctx, args...)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (g Git) StashPushAllPathspecs(ctx context.Context, message string, pathspecs ...string) (StashEntry, bool, error) {
	if message == "" {
		return StashEntry{}, false, errors.New("stash message is required")
	}
	if len(pathspecs) == 0 {
		return StashEntry{}, false, errors.New("at least one stash pathspec is required")
	}

	pathspecFile, cleanup, err := writePathspecFile(pathspecs)
	if err != nil {
		return StashEntry{}, false, err
	}
	defer cleanup()

	before, err := g.optionalRevParse(ctx, "refs/stash")
	if err != nil {
		return StashEntry{}, false, err
	}
	_, err = g.RunOK(ctx, "stash", "push", "--all", "-m", message, "--pathspec-from-file="+pathspecFile, "--pathspec-file-nul")
	if err != nil {
		return StashEntry{}, false, err
	}
	after, err := g.optionalRevParse(ctx, "refs/stash")
	if err != nil {
		return StashEntry{}, false, err
	}
	if after == "" || after == before {
		return StashEntry{}, false, nil
	}
	return StashEntry{OID: after, Message: message}, true, nil
}

func (g Git) StashApply(ctx context.Context, ref string) error {
	if ref == "" {
		return errors.New("stash ref is required")
	}
	_, err := g.RunOK(ctx, "stash", "apply", ref)
	return err
}

func (g Git) RestoreStashIndexPathspecs(ctx context.Context, stashOID string, pathspecs ...string) error {
	if stashOID == "" {
		return errors.New("stash oid is required")
	}
	if len(pathspecs) == 0 {
		return errors.New("at least one stash index pathspec is required")
	}
	pathspecFile, cleanup, err := writePathspecFile(pathspecs)
	if err != nil {
		return err
	}
	defer cleanup()

	_, err = g.RunOK(ctx, "restore", "--source="+stashOID+"^2", "--staged", "--pathspec-from-file="+pathspecFile, "--pathspec-file-nul")
	return err
}

func (g Git) StashList(ctx context.Context) ([]StashListEntry, error) {
	result, err := g.RunOK(ctx, "stash", "list", "--format=%gd%x00%H%x00%gs")
	if err != nil {
		return nil, err
	}
	output := strings.TrimRight(result.Stdout, "\n")
	if output == "" {
		return nil, nil
	}
	lines := strings.Split(output, "\n")
	entries := make([]StashListEntry, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\x00")
		if len(parts) != 3 {
			return nil, fmt.Errorf("could not parse stash list entry %q", line)
		}
		entries = append(entries, StashListEntry{
			Selector: parts[0],
			OID:      parts[1],
			Subject:  parts[2],
		})
	}
	return entries, nil
}

func (g Git) ResolveStashSelector(ctx context.Context, entry StashEntry) (string, error) {
	entries, err := g.StashList(ctx)
	if err != nil {
		return "", err
	}
	return resolveStashSelectorFromList(entry, entries)
}

func resolveStashSelectorFromList(entry StashEntry, entries []StashListEntry) (string, error) {
	var matches []StashListEntry
	for _, candidate := range entries {
		if candidate.OID == entry.OID && stashSubjectMatchesMessage(candidate.Subject, entry.Message) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return "", &StashLookupError{Entry: entry, Matches: matches}
	}
	return matches[0].Selector, nil
}

func (g Git) DropStashEntry(ctx context.Context, entry StashEntry) (string, error) {
	selector, err := g.ResolveStashSelector(ctx, entry)
	if err != nil {
		return "", err
	}
	_, err = g.RunOK(ctx, "stash", "drop", selector)
	return selector, err
}

func (g Git) BlockingOperation(ctx context.Context) (string, bool, error) {
	result, err := g.RunOK(ctx, "ls-files", "-u")
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(result.Stdout) != "" {
		return "unmerged index entries", true, nil
	}

	for _, sentinel := range blockingOperationSentinels {
		path, err := g.gitPath(ctx, sentinel)
		if err != nil {
			return "", false, err
		}
		if _, err := os.Stat(path); err == nil {
			return sentinel, true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false, err
		}
	}
	return "", false, nil
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
	if strings.TrimSpace(out) == "" {
		return TreeItem{}, &TreeItemNotFoundError{Treeish: treeish, Path: path}
	}
	meta, _, ok := strings.Cut(out, "\t")
	if !ok {
		return TreeItem{}, fmt.Errorf("could not parse tree item %q", out)
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

func (g Git) TreeItem(ctx context.Context, treeish string) (TreeItem, error) {
	hash, err := g.RevParse(ctx, treeish+"^{tree}")
	if err != nil {
		return TreeItem{}, err
	}
	return TreeItem{Mode: "040000", Type: "tree", Hash: hash}, nil
}

func (g Git) ShowFile(ctx context.Context, treeish, path string) ([]byte, bool, error) {
	result, err := g.RunOK(ctx, "ls-tree", treeish, path)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(result.Stdout) == "" {
		return nil, false, nil
	}
	result, err = g.RunOK(ctx, "show", treeish+":"+path)
	if err != nil {
		return nil, false, err
	}
	return []byte(result.Stdout), true, nil
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

func (g Git) RemoveCachedRecursiveIfExists(ctx context.Context, path string) error {
	_, err := g.RunOK(ctx, "rm", "-r", "--cached", "--ignore-unmatch", "--", path)
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

func (g Git) HashFile(ctx context.Context, path string) (TreeItem, error) {
	hash, err := g.Output(ctx, "hash-object", "-w", "--", path)
	if err != nil {
		return TreeItem{}, err
	}
	return TreeItem{Mode: "100644", Type: "blob", Hash: hash}, nil
}

func (g Git) HashBytes(ctx context.Context, data []byte) (TreeItem, error) {
	var stdout, stderr bytes.Buffer
	result, err := g.Runner.RunInteractive(ctx, bytes.NewReader(data), &stdout, &stderr, "hash-object", "-w", "--stdin")
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if err != nil {
		return TreeItem{}, err
	}
	if result.ExitCode != 0 {
		return TreeItem{}, &ExitError{Result: result}
	}
	return TreeItem{Mode: "100644", Type: "blob", Hash: strings.TrimSpace(result.Stdout)}, nil
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

func (g Git) CommitTreeWithTemporaryIndex(ctx context.Context, treeish, message string) (bool, error) {
	dir, err := os.MkdirTemp("", "braid-index")
	if err != nil {
		return false, err
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	tempGit := g.withIndex(filepath.Join(dir, "index"))
	if err := tempGit.ReadTreeIndexMerge(ctx, treeish); err != nil {
		return false, err
	}
	return tempGit.CommitMessage(ctx, message)
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

func (g Git) WriteTree(ctx context.Context) (string, error) {
	return g.Output(ctx, "write-tree")
}

func (g Git) RestorePathspecsFromHead(ctx context.Context, pathspecs ...string) error {
	return g.RestorePathspecsFromTree(ctx, "HEAD", true, true, pathspecs...)
}

func (g Git) RestorePathspecsFromTree(ctx context.Context, treeish string, staged, worktree bool, pathspecs ...string) error {
	args := []string{"restore", "--source=" + treeish}
	if staged {
		args = append(args, "--staged")
	}
	if worktree {
		args = append(args, "--worktree")
	}
	args = append(args, "--")
	args = append(args, pathspecs...)
	_, err := g.RunOK(ctx, args...)
	return err
}

func (g Git) CommitVerbose(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	_, err := g.Runner.RunInteractiveOK(ctx, stdin, stdout, stderr, "commit", "-v")
	return err
}

func (g Git) CommitVerboseMessageFile(ctx context.Context, messagePath string, stdin io.Reader, stdout, stderr io.Writer) error {
	if messagePath == "" {
		return errors.New("commit message path is required")
	}
	_, err := g.Runner.RunInteractiveOK(ctx, stdin, stdout, stderr, "commit", "--cleanup=strip", "-v", "-F", messagePath, "-e")
	return err
}

func (g Git) Push(ctx context.Context, args ...string) error {
	gitArgs := append([]string{"push"}, args...)
	_, err := g.RunOK(ctx, gitArgs...)
	return err
}

func (g Git) FirstParentCommits(ctx context.Context, rev string) ([]string, error) {
	if rev == "" {
		return nil, errors.New("revision is required")
	}
	result, err := g.RunOK(ctx, "rev-list", "--first-parent", rev)
	if err != nil {
		return nil, err
	}
	return splitNonEmptyLines(result.Stdout), nil
}

func (g Git) LogCommitsTouchingPath(ctx context.Context, revisionRange, path string) ([]Commit, error) {
	if path == "" {
		return nil, errors.New("path is required")
	}
	args := []string{"log", "--full-history", "--reverse", "--format=%x1f%H%x00%s%x00%B%x1e"}
	if revisionRange != "" {
		args = append(args, revisionRange)
	} else {
		args = append(args, "HEAD")
	}
	args = append(args, "--", path)
	result, err := g.RunOK(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseLogCommits(result.Stdout)
}

func (g Git) MakeTreeWithItem(ctx context.Context, itemPath string, item TreeItem) (string, error) {
	return g.MakeTreeWithItemIn(ctx, "", itemPath, item)
}

func (g Git) MakeTreeWithoutPath(ctx context.Context, mainContent, itemPath string) (string, error) {
	if mainContent == "" {
		return "", errors.New("base treeish is required")
	}
	if itemPath == "" {
		return "", errors.New("item path is required")
	}

	dir, err := os.MkdirTemp("", "braid-index")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	tempGit := g.withIndex(filepath.Join(dir, "index"))
	if err := tempGit.ReadTreeIndexMerge(ctx, mainContent); err != nil {
		return "", err
	}
	if err := tempGit.RemoveCachedRecursive(ctx, itemPath); err != nil {
		return "", err
	}
	return tempGit.Output(ctx, "write-tree")
}

func (g Git) MakeTreeWithItemIn(ctx context.Context, mainContent, itemPath string, item TreeItem) (string, error) {
	dir, err := os.MkdirTemp("", "braid-index")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	tempGit := g.withIndex(filepath.Join(dir, "index"))

	if mainContent != "" && itemPath != "" {
		// Use a temporary index to compose synthetic trees without disturbing the caller's index.
		if err := tempGit.ReadTreeIndexMerge(ctx, mainContent); err != nil {
			return "", err
		}
		if err := tempGit.RemoveCachedRecursiveIfExists(ctx, itemPath); err != nil {
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

func (g Git) MergeTreeWrite(ctx context.Context, baseTreeish, localTreeish, remoteTreeish string) (MergeTreeResult, error) {
	result, runErr := g.Run(ctx, mergeTreeWriteArgs(baseTreeish, localTreeish, remoteTreeish, true)...)
	parsed := parseMergeTreeOutput(result.Stdout)
	if runErr != nil {
		return parsed, runErr
	}
	if result.ExitCode != 0 {
		if result.ExitCode != 1 {
			return parsed, &ExitError{Result: result}
		}
		if len(parsed.ConflictPaths) == 0 {
			parsed = g.mergeTreeConflictPathFallback(ctx, parsed, baseTreeish, localTreeish, remoteTreeish)
		}
		if len(parsed.ConflictPaths) == 0 {
			parsed.ConflictPaths = []string{"(unknown path)"}
		}
		return parsed, &ExitError{Result: result}
	}
	return parsed, nil
}

func IsMergeTreeConflict(err error) bool {
	var exitErr *ExitError
	return errors.As(err, &exitErr) && exitErr.Result.ExitCode == 1
}

func mergeTreeWriteArgs(baseTreeish, localTreeish, remoteTreeish string, messages bool) []string {
	messageArg := "--no-messages"
	if messages {
		messageArg = "--messages"
	}
	return []string{"merge-tree", "--write-tree", "--name-only", "-z", messageArg, "--merge-base=" + baseTreeish, localTreeish, remoteTreeish}
}

func (g Git) mergeTreeConflictPathFallback(ctx context.Context, parsed MergeTreeResult, baseTreeish, localTreeish, remoteTreeish string) MergeTreeResult {
	result, runErr := g.Run(ctx, mergeTreeWriteArgs(baseTreeish, localTreeish, remoteTreeish, false)...)
	if runErr != nil {
		return parsed
	}
	fallback := parseMergeTreeOutput(result.Stdout)
	if parsed.Tree == "" {
		parsed.Tree = fallback.Tree
	}
	if len(fallback.ConflictPaths) != 0 {
		parsed.ConflictPaths = fallback.ConflictPaths
	}
	return parsed
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

func (g Git) optionalRevParse(ctx context.Context, rev string) (string, error) {
	result, err := g.RunOK(ctx, "rev-parse", "--verify", "-q", rev)
	if err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) && exitErr.Result.ExitCode == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func writePathspecFile(pathspecs []string) (string, func(), error) {
	file, err := os.CreateTemp("", "braid-pathspecs")
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	cleanup := func() {
		_ = os.Remove(path)
	}
	for _, pathspec := range pathspecs {
		if strings.Contains(pathspec, "\x00") {
			_ = file.Close()
			cleanup()
			return "", func() {}, fmt.Errorf("pathspec contains NUL byte: %q", pathspec)
		}
		if _, err := file.WriteString(pathspec); err != nil {
			_ = file.Close()
			cleanup()
			return "", func() {}, err
		}
		if _, err := file.Write([]byte{0}); err != nil {
			_ = file.Close()
			cleanup()
			return "", func() {}, err
		}
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func splitNonEmptyLines(output string) []string {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

func parseLogCommits(output string) ([]Commit, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}
	records := strings.Split(output, "\x1e")
	commits := make([]Commit, 0, len(records))
	for _, record := range records {
		record = strings.TrimPrefix(record, "\n")
		record = strings.TrimPrefix(record, "\x1f")
		record = strings.TrimSuffix(record, "\n")
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\x00", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("could not parse log commit record %q", record)
		}
		commits = append(commits, Commit{
			Hash:    fields[0],
			Subject: fields[1],
			Message: strings.TrimSuffix(fields[2], "\n"),
		})
	}
	return commits, nil
}

func stashSubjectMatchesMessage(subject, message string) bool {
	return subject == message || strings.HasSuffix(subject, ": "+message)
}

func (g Git) withIndex(indexPath string) Git {
	tempGit := g
	tempGit.Runner.Env = copyEnv(g.Runner.Env)
	tempGit.Runner.Env["GIT_INDEX_FILE"] = indexPath
	return tempGit
}

func (g Git) gitPath(ctx context.Context, path string) (string, error) {
	gitPath, err := g.RepoFilePath(ctx, path)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(gitPath) {
		return gitPath, nil
	}
	return filepath.Join(workDir(g.Runner.WorkDir), gitPath), nil
}

func workDir(value string) string {
	if value == "" {
		return "."
	}
	return value
}

func parseMergeTreeOutput(output string) MergeTreeResult {
	if strings.Contains(output, "\x00") {
		return parseMergeTreeOutputZ(output)
	}
	first, rest, ok := strings.Cut(output, "\n")
	if !ok {
		return MergeTreeResult{Tree: strings.TrimSpace(output)}
	}
	return MergeTreeResult{
		Tree:          strings.TrimSpace(first),
		ConflictPaths: nil,
		Details:       rest,
	}
}

func parseMergeTreeOutputZ(output string) MergeTreeResult {
	records := strings.Split(output, "\x00")
	if len(records) == 0 {
		return MergeTreeResult{}
	}
	result := MergeTreeResult{Tree: strings.TrimSpace(records[0])}
	seenPaths := map[string]bool{}

	i := 1
	for ; i < len(records); i++ {
		path := records[i]
		if path == "" {
			i++
			break
		}
		if !seenPaths[path] {
			seenPaths[path] = true
			result.ConflictPaths = append(result.ConflictPaths, path)
		}
	}
	details, messageConflictPaths := parseMergeTreeMessageDetails(records[i:])
	result.Details = details
	if len(result.ConflictPaths) == 0 {
		result.ConflictPaths = messageConflictPaths
	}
	return result
}

func parseMergeTreeMessageDetails(records []string) (string, []string) {
	var lines []string
	var conflictPaths []string
	seenConflictPaths := map[string]bool{}
	for i := 0; i < len(records); {
		record := records[i]
		if record == "" {
			i++
			continue
		}
		if pathCount, err := strconv.Atoi(record); err == nil && pathCount >= 0 {
			pathStart := i + 1
			reasonIndex := i + pathCount + 1
			messageIndex := reasonIndex + 1
			if messageIndex >= len(records) {
				lines = append(lines, strings.TrimRight(record, "\n"))
				i++
				continue
			}
			reason := records[reasonIndex]
			message := strings.TrimRight(records[messageIndex], "\n")
			if message != "" {
				lines = append(lines, message)
			}
			if isMergeTreeConflictMessage(reason, message) {
				for _, path := range records[pathStart:reasonIndex] {
					if path != "" && !seenConflictPaths[path] {
						seenConflictPaths[path] = true
						conflictPaths = append(conflictPaths, path)
					}
				}
			}
			i = messageIndex + 1
			continue
		}
		lines = append(lines, strings.TrimRight(record, "\n"))
		i++
	}
	if len(lines) == 0 {
		return "", conflictPaths
	}
	return strings.Join(lines, "\n") + "\n", conflictPaths
}

func isMergeTreeConflictMessage(reason, message string) bool {
	return strings.HasPrefix(reason, "CONFLICT") || strings.HasPrefix(message, "CONFLICT")
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
