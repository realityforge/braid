package gitexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunnerCapturesOutputAndStatus(t *testing.T) {
	runner := helperRunner(t, map[string]string{"GITEXEC_HELPER_EXIT": "7"})

	result, err := runner.Run(context.Background(), "mock-output")

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
	if result.Stdout != "helper stdout\n" {
		t.Fatalf("Stdout = %q", result.Stdout)
	}
	if result.Stderr != "helper stderr\n" {
		t.Fatalf("Stderr = %q", result.Stderr)
	}
	if !reflect.DeepEqual(result.GitArgs, []string{"mock-output"}) {
		t.Fatalf("GitArgs = %#v", result.GitArgs)
	}
}

func TestRunnerRunOKMapsNonZeroToExitError(t *testing.T) {
	runner := helperRunner(t, map[string]string{"GITEXEC_HELPER_EXIT": "9"})

	result, err := runner.RunOK(context.Background(), "mock-output")

	if result.ExitCode != 9 {
		t.Fatalf("ExitCode = %d, want 9", result.ExitCode)
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T %v, want *ExitError", err, err)
	}
	if !strings.Contains(err.Error(), "helper stderr") {
		t.Fatalf("error = %q, want stderr included", err.Error())
	}
}

func TestRunnerRunInteractivePassesStdioAndStatus(t *testing.T) {
	runner := helperRunner(t, map[string]string{"GITEXEC_HELPER_EXIT": "7"})
	var stdout, stderr bytes.Buffer

	result, err := runner.RunInteractive(context.Background(), strings.NewReader("input text\n"), &stdout, &stderr, "interactive")

	if err != nil {
		t.Fatalf("RunInteractive returned error: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
	if stdout.String() != "helper stdout: input text\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "helper stderr: input text\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if result.Stdout != "" {
		t.Fatalf("Result.Stdout = %q, want empty for interactive run", result.Stdout)
	}
	if result.Stderr != "" {
		t.Fatalf("Result.Stderr = %q, want empty for interactive run", result.Stderr)
	}
	if !reflect.DeepEqual(result.GitArgs, []string{"interactive"}) {
		t.Fatalf("GitArgs = %#v", result.GitArgs)
	}
}

func TestRunnerRunInteractiveOKMapsNonZeroToExitError(t *testing.T) {
	runner := helperRunner(t, map[string]string{"GITEXEC_HELPER_EXIT": "9"})
	var stdout, stderr bytes.Buffer

	result, err := runner.RunInteractiveOK(context.Background(), strings.NewReader("failure\n"), &stdout, &stderr, "interactive")

	if result.ExitCode != 9 {
		t.Fatalf("ExitCode = %d, want 9", result.ExitCode)
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T %v, want *ExitError", err, err)
	}
	if stdout.String() != "helper stdout: failure\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "helper stderr: failure\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunnerUsesArgvWithoutShellInterpretation(t *testing.T) {
	runner := helperRunner(t, nil)
	dangerous := "semi;colon $(echo no) && still-one-arg"

	result, err := runner.RunOK(context.Background(), "argv", dangerous, "plain")

	if err != nil {
		t.Fatalf("RunOK returned error: %v", err)
	}
	got := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	want := []string{"argv", dangerous, "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestRunnerEnvironmentOverridesAreScoped(t *testing.T) {
	const key = "GITEXEC_SCOPED_VALUE"
	if old := os.Getenv(key); old != "" {
		t.Fatalf("%s unexpectedly set in test environment", key)
	}

	runner := helperRunner(t, map[string]string{key: "from-runner"})
	result, err := runner.RunOK(context.Background(), "env", key)

	if err != nil {
		t.Fatalf("RunOK returned error: %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != "from-runner" {
		t.Fatalf("env output = %q, want from-runner", got)
	}
	if got := os.Getenv(key); got != "" {
		t.Fatalf("process env leaked %s=%q", key, got)
	}
}

func TestRunnerVerboseTraceUsesDeterministicArgv(t *testing.T) {
	var trace bytes.Buffer
	dir := t.TempDir()
	runner := helperRunner(t, nil)
	runner.WorkDir = dir
	runner.Verbose = true
	runner.Trace = &trace

	_, err := runner.RunOK(context.Background(), "status", "--short", "path with space")

	if err != nil {
		t.Fatalf("RunOK returned error: %v", err)
	}
	want := fmt.Sprintf("Braid: Executing [\"git\", \"status\", \"--short\", \"path with space\"] in %s\n", dir)
	if got := trace.String(); got != want {
		t.Fatalf("trace = %q, want %q", got, want)
	}
}

func TestGitVersionAndRequirement(t *testing.T) {
	git := Git{Runner: helperRunner(t, map[string]string{"GITEXEC_HELPER_VERSION": "git version 2.51.0 (Apple Git-171)\n"})}

	version, err := git.Version(context.Background())
	if err != nil {
		t.Fatalf("Version returned error: %v", err)
	}
	if version != "2.51.0" {
		t.Fatalf("version = %q, want 2.51.0", version)
	}
	if err := git.RequireVersion(context.Background(), MinimumGitVersion); err != nil {
		t.Fatalf("RequireVersion returned error: %v", err)
	}
	if err := git.RequireVersion(context.Background(), "2.60.0"); err == nil {
		t.Fatal("RequireVersion returned nil for too-low version")
	}
}

func TestParseAndCompareVersions(t *testing.T) {
	parseCases := map[string]string{
		"git version 1.5.5.1.98.gf0ec4\n":    "1.5.5.1.98.gf0ec4",
		"git version 2.39.3 (Apple Git-146)": "2.39.3",
		"not git":                            "",
	}
	for input, want := range parseCases {
		if got := ParseVersion(input); got != want {
			t.Fatalf("ParseVersion(%q) = %q, want %q", input, got, want)
		}
	}

	if CompareVersions("1.5.5.1.98.gf0ec4", "1.5.4.5") <= 0 {
		t.Fatal("expected 1.5.5.1.98.gf0ec4 to be greater than 1.5.4.5")
	}
	if CompareVersions("2.38.9", MinimumGitVersion) >= 0 {
		t.Fatal("expected 2.38.9 to be less than minimum")
	}
	if CompareVersions("2.39.0", MinimumGitVersion) != 0 {
		t.Fatal("expected 2.39.0 to equal minimum")
	}
}

func TestGitPreflightWrappers(t *testing.T) {
	git := Git{Runner: helperRunner(t, nil)}

	inside, err := git.IsInsideWorkTree(context.Background())
	if err != nil {
		t.Fatalf("IsInsideWorkTree returned error: %v", err)
	}
	if !inside {
		t.Fatal("IsInsideWorkTree = false, want true")
	}

	prefix, err := git.RelativeWorkingDir(context.Background())
	if err != nil {
		t.Fatalf("RelativeWorkingDir returned error: %v", err)
	}
	if prefix != "sub/" {
		t.Fatalf("RelativeWorkingDir = %q, want sub/", prefix)
	}

	root, err := git.WorkTreeRoot(context.Background())
	if err != nil {
		t.Fatalf("WorkTreeRoot returned error: %v", err)
	}
	if root != "/repo" {
		t.Fatalf("WorkTreeRoot = %q, want /repo", root)
	}

	path, err := git.RepoFilePath(context.Background(), "MERGE_MSG")
	if err != nil {
		t.Fatalf("RepoFilePath returned error: %v", err)
	}
	if path != ".git/MERGE_MSG" {
		t.Fatalf("RepoFilePath = %q, want .git/MERGE_MSG", path)
	}
}

func TestWorkTreeRootFromRootAndSubdirectory(t *testing.T) {
	repo := initRealRepo(t)
	subdir := filepath.Join(repo, "nested", "dir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	want := realGitOutput(t, repo, "rev-parse", "--show-toplevel")
	for _, workDir := range []string{repo, subdir} {
		t.Run(workDir, func(t *testing.T) {
			got, err := New(workDir, false, nil).WorkTreeRoot(context.Background())
			if err != nil {
				t.Fatalf("WorkTreeRoot returned error: %v", err)
			}
			if got != want {
				t.Fatalf("WorkTreeRoot = %q, want %q", got, want)
			}
		})
	}
}

func TestStatusPorcelainPathspecsIgnoresUnrelatedPaths(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, "target.txt", "base\n")
	writeRealFile(t, repo, "unrelated.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base")

	writeRealFile(t, repo, "target.txt", "target change\n")
	writeRealFile(t, repo, "unrelated.txt", "unrelated change\n")
	writeRealFile(t, repo, "other/untracked.txt", "untracked\n")

	status, err := New(repo, false, nil).StatusPorcelainPathspecs(context.Background(), "target.txt")
	if err != nil {
		t.Fatalf("StatusPorcelainPathspecs returned error: %v", err)
	}
	if !strings.Contains(status, "target.txt") {
		t.Fatalf("status = %q, want target path", status)
	}
	if strings.Contains(status, "unrelated") || strings.Contains(status, "other/") {
		t.Fatalf("status = %q, want unrelated paths ignored", status)
	}
}

func TestBlockingOperationDetectsSentinelAndUnmergedEntries(t *testing.T) {
	t.Run("sentinel", func(t *testing.T) {
		repo := initRealRepo(t)
		writeRealFile(t, repo, "file.txt", "base\n")
		realGit(t, repo, "add", ".")
		realGit(t, repo, "commit", "-m", "base")
		writeRealFile(t, filepath.Join(repo, ".git"), "MERGE_HEAD", "abc123\n")

		state, blocked, err := New(repo, false, nil).BlockingOperation(context.Background())
		if err != nil {
			t.Fatalf("BlockingOperation returned error: %v", err)
		}
		if !blocked || state != "MERGE_HEAD" {
			t.Fatalf("state = %q blocked = %v, want MERGE_HEAD block", state, blocked)
		}
	})

	t.Run("unmerged", func(t *testing.T) {
		repo := initRealRepo(t)
		writeRealFile(t, repo, "file.txt", "base\n")
		realGit(t, repo, "add", ".")
		realGit(t, repo, "commit", "-m", "base")
		realGit(t, repo, "checkout", "-b", "left")
		writeRealFile(t, repo, "file.txt", "left\n")
		realGit(t, repo, "commit", "-am", "left")
		realGit(t, repo, "checkout", "main")
		writeRealFile(t, repo, "file.txt", "right\n")
		realGit(t, repo, "commit", "-am", "right")
		result, err := Runner{WorkDir: repo}.Run(context.Background(), "merge", "left")
		if err != nil {
			t.Fatalf("merge command failed to run: %v", err)
		}
		if result.ExitCode == 0 {
			t.Fatal("merge unexpectedly succeeded")
		}

		state, blocked, err := New(repo, false, nil).BlockingOperation(context.Background())
		if err != nil {
			t.Fatalf("BlockingOperation returned error: %v", err)
		}
		if !blocked || state != "unmerged index entries" {
			t.Fatalf("state = %q blocked = %v, want unmerged block", state, blocked)
		}
	})
}

func TestMergeTreeWriteReturnsMergedTreeAndConflictDetails(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		repo := initRealRepo(t)
		writeRealFile(t, repo, "base.txt", "base\n")
		realGit(t, repo, "add", ".")
		realGit(t, repo, "commit", "-m", "base")
		base := realGitOutput(t, repo, "rev-parse", "HEAD")
		realGit(t, repo, "checkout", "-b", "local")
		writeRealFile(t, repo, "local.txt", "local\n")
		realGit(t, repo, "add", ".")
		realGit(t, repo, "commit", "-m", "local")
		local := realGitOutput(t, repo, "rev-parse", "HEAD")
		realGit(t, repo, "checkout", "main")
		realGit(t, repo, "checkout", "-b", "remote")
		writeRealFile(t, repo, "remote.txt", "remote\n")
		realGit(t, repo, "add", ".")
		realGit(t, repo, "commit", "-m", "remote")
		remote := realGitOutput(t, repo, "rev-parse", "HEAD")

		merged, err := New(repo, false, nil).MergeTreeWrite(context.Background(), base, local, remote)
		if err != nil {
			t.Fatalf("MergeTreeWrite returned error: %v", err)
		}
		if merged.Tree == "" {
			t.Fatal("merged tree is empty")
		}
		tree := realGitOutput(t, repo, "ls-tree", "-r", "--name-only", merged.Tree)
		if !strings.Contains(tree, "local.txt") || !strings.Contains(tree, "remote.txt") {
			t.Fatalf("merged tree contents = %q, want local and remote files", tree)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		repo := initRealRepo(t)
		writeRealFile(t, repo, "file.txt", "base\n")
		realGit(t, repo, "add", ".")
		realGit(t, repo, "commit", "-m", "base")
		base := realGitOutput(t, repo, "rev-parse", "HEAD")
		realGit(t, repo, "checkout", "-b", "local")
		writeRealFile(t, repo, "file.txt", "local\n")
		realGit(t, repo, "commit", "-am", "local")
		local := realGitOutput(t, repo, "rev-parse", "HEAD")
		realGit(t, repo, "checkout", "main")
		realGit(t, repo, "checkout", "-b", "remote")
		writeRealFile(t, repo, "file.txt", "remote\n")
		realGit(t, repo, "commit", "-am", "remote")
		remote := realGitOutput(t, repo, "rev-parse", "HEAD")

		before := realGitOutput(t, repo, "status", "--porcelain")
		merged, err := New(repo, false, nil).MergeTreeWrite(context.Background(), base, local, remote)
		if err == nil {
			t.Fatal("MergeTreeWrite returned nil error for conflict")
		}
		if merged.Tree == "" {
			t.Fatal("conflicted merged tree is empty")
		}
		if !strings.Contains(merged.Details, "CONFLICT") || !strings.Contains(merged.Details, "file.txt") {
			t.Fatalf("conflict details = %q, want conflict path", merged.Details)
		}
		after := realGitOutput(t, repo, "status", "--porcelain")
		if after != before {
			t.Fatalf("status changed from %q to %q", before, after)
		}
	})
}

func TestCommitTreeWithTemporaryIndexExcludesRealIndexAndPreservesHookBehavior(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, "mirror.txt", "base\n")
	writeRealFile(t, repo, "unrelated.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base")

	writeRealFile(t, repo, "mirror.txt", "updated\n")
	blob := realGitOutput(t, repo, "hash-object", "-w", "mirror.txt")
	git := New(repo, false, nil)
	tree, err := git.MakeTreeWithItemIn(context.Background(), "HEAD", "mirror.txt", TreeItem{Mode: "100644", Type: "blob", Hash: blob})
	if err != nil {
		t.Fatalf("MakeTreeWithItemIn returned error: %v", err)
	}

	writeRealFile(t, repo, "unrelated.txt", "staged\n")
	realGit(t, repo, "add", "unrelated.txt")
	writeRealFile(t, repo, ".git/hooks/pre-commit", "#!/bin/sh\nexit 1\n")
	chmodRealFile(t, repo, ".git/hooks/pre-commit", 0o755)
	writeRealFile(t, repo, ".git/hooks/post-commit", "#!/bin/sh\nprintf ran > post-commit-ran\n")
	chmodRealFile(t, repo, ".git/hooks/post-commit", 0o755)

	committed, err := git.CommitTreeWithTemporaryIndex(context.Background(), tree, "temp index commit")
	if err != nil {
		t.Fatalf("CommitTreeWithTemporaryIndex returned error: %v", err)
	}
	if !committed {
		t.Fatal("CommitTreeWithTemporaryIndex committed = false, want true")
	}
	if got := realGitOutput(t, repo, "show", "HEAD:mirror.txt"); got != "updated" {
		t.Fatalf("HEAD:mirror.txt = %q, want updated", got)
	}
	if got := realGitOutput(t, repo, "show", "HEAD:unrelated.txt"); got != "base" {
		t.Fatalf("HEAD:unrelated.txt = %q, want base", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "post-commit-ran")); err != nil {
		t.Fatalf("post-commit hook did not run: %v", err)
	}
	if err := git.RestorePathspecsFromHead(context.Background(), "mirror.txt"); err != nil {
		t.Fatalf("RestorePathspecsFromHead returned error: %v", err)
	}
	if staged := realGitOutput(t, repo, "diff", "--cached", "--name-only"); staged != "unrelated.txt" {
		t.Fatalf("cached diff = %q, want unrelated.txt", staged)
	}
}

func TestHashBytesStoresBlobWithoutWorktreeChanges(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, "tracked.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base")
	before := realGitOutput(t, repo, "status", "--porcelain")

	item, err := New(repo, false, nil).HashBytes(context.Background(), []byte("config bytes\n"))
	if err != nil {
		t.Fatalf("HashBytes returned error: %v", err)
	}
	if item.Mode != "100644" || item.Type != "blob" || item.Hash == "" {
		t.Fatalf("HashBytes item = %#v, want blob item", item)
	}
	if got := realGitOutput(t, repo, "cat-file", "-p", item.Hash); got != "config bytes" {
		t.Fatalf("blob content = %q, want config bytes", got)
	}
	after := realGitOutput(t, repo, "status", "--porcelain")
	if after != before {
		t.Fatalf("status changed from %q to %q", before, after)
	}
}

func TestMakeTreeWithoutPathRemovesPathAndPreservesRealIndexAndWorktree(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, "vendor/remove/file.txt", "remove\n")
	writeRealFile(t, repo, "vendor/keep/file.txt", "keep\n")
	writeRealFile(t, repo, "unrelated.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base")

	writeRealFile(t, repo, "unrelated.txt", "staged\n")
	realGit(t, repo, "add", "unrelated.txt")
	before := realGitOutput(t, repo, "status", "--porcelain")

	tree, err := New(repo, false, nil).MakeTreeWithoutPath(context.Background(), "HEAD", "vendor/remove")
	if err != nil {
		t.Fatalf("MakeTreeWithoutPath returned error: %v", err)
	}
	names := realGitOutput(t, repo, "ls-tree", "-r", "--name-only", tree)
	if strings.Contains(names, "vendor/remove/file.txt") {
		t.Fatalf("tree names = %q, want vendor/remove removed", names)
	}
	if !strings.Contains(names, "vendor/keep/file.txt") {
		t.Fatalf("tree names = %q, want vendor/keep retained", names)
	}
	after := realGitOutput(t, repo, "status", "--porcelain")
	if after != before {
		t.Fatalf("status changed from %q to %q", before, after)
	}
	if got := readRealFile(t, repo, "vendor/remove/file.txt"); got != "remove\n" {
		t.Fatalf("worktree vendor/remove/file.txt = %q, want unchanged", got)
	}
	if got := strings.TrimSpace(realGitOutput(t, repo, "show", ":unrelated.txt")); got != "staged" {
		t.Fatalf("staged unrelated blob = %q, want staged", got)
	}
}

func TestRestorePathspecsFromHeadUpdatesOnlyExplicitPathspecs(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, "restore.txt", "base\n")
	writeRealFile(t, repo, "keep.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base")
	writeRealFile(t, repo, "restore.txt", "changed\n")
	writeRealFile(t, repo, "keep.txt", "changed\n")
	realGit(t, repo, "add", "restore.txt", "keep.txt")

	if err := New(repo, false, nil).RestorePathspecsFromHead(context.Background(), "restore.txt"); err != nil {
		t.Fatalf("RestorePathspecsFromHead returned error: %v", err)
	}
	if got := readRealFile(t, repo, "restore.txt"); got != "base\n" {
		t.Fatalf("restore.txt = %q, want base", got)
	}
	if got := readRealFile(t, repo, "keep.txt"); got != "changed\n" {
		t.Fatalf("keep.txt = %q, want changed", got)
	}
	if staged := realGitOutput(t, repo, "diff", "--cached", "--name-only"); staged != "keep.txt" {
		t.Fatalf("cached diff = %q, want keep.txt only", staged)
	}
}

func helperRunner(t *testing.T, env map[string]string) Runner {
	t.Helper()
	merged := map[string]string{
		"GITEXEC_HELPER_PROCESS": "1",
	}
	for key, value := range env {
		merged[key] = value
	}
	return Runner{
		Executable: os.Args[0],
		PrefixArgs: []string{
			"-test.run=TestHelperProcess",
			"--",
		},
		Env: merged,
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GITEXEC_HELPER_PROCESS") != "1" {
		return
	}

	args := helperGitArgs(os.Args)
	if len(args) == 0 {
		os.Exit(0)
	}

	switch args[0] {
	case "mock-output":
		helperFprintln(os.Stdout, "helper stdout")
		helperFprintln(os.Stderr, "helper stderr")
		os.Exit(helperExitCode())
	case "interactive":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			helperFprintf(os.Stderr, "read stdin: %v\n", err)
			os.Exit(2)
		}
		helperFprintf(os.Stdout, "helper stdout: %s", input)
		helperFprintf(os.Stderr, "helper stderr: %s", input)
		os.Exit(helperExitCode())
	case "argv":
		for _, arg := range args {
			helperFprintln(os.Stdout, arg)
		}
	case "env":
		if len(args) != 2 {
			helperFprintln(os.Stderr, "env requires key")
			os.Exit(2)
		}
		helperFprintln(os.Stdout, os.Getenv(args[1]))
	case "status":
	case "--version":
		version := os.Getenv("GITEXEC_HELPER_VERSION")
		if version == "" {
			version = "git version 2.51.0\n"
		}
		helperFprint(os.Stdout, version)
	case "rev-parse":
		helperRevParse(args[1:])
	default:
		helperFprintf(os.Stderr, "unknown helper command %s\n", args[0])
		os.Exit(127)
	}
	os.Exit(0)
}

func helperFprint(w io.Writer, a ...any) {
	if _, err := fmt.Fprint(w, a...); err != nil {
		os.Exit(2)
	}
}

func helperFprintf(w io.Writer, format string, a ...any) {
	if _, err := fmt.Fprintf(w, format, a...); err != nil {
		os.Exit(2)
	}
}

func helperFprintln(w io.Writer, a ...any) {
	if _, err := fmt.Fprintln(w, a...); err != nil {
		os.Exit(2)
	}
}

func helperGitArgs(args []string) []string {
	for i, arg := range args {
		if arg == "--" {
			return args[i+1:]
		}
	}
	return nil
}

func initRealRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	realGit(t, dir, "init", "--initial-branch=main")
	realGit(t, dir, "config", "--local", "user.name", "Braid Test")
	realGit(t, dir, "config", "--local", "user.email", "braid-test@example.invalid")
	realGit(t, dir, "config", "--local", "commit.gpgsign", "false")
	return dir
}

func realGit(t *testing.T, dir string, args ...string) Result {
	t.Helper()
	result, err := Runner{WorkDir: dir}.RunOK(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %v failed in %s: %v\nstdout:\n%s\nstderr:\n%s", args, dir, err, result.Stdout, result.Stderr)
	}
	return result
}

func realGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(realGit(t, dir, args...).Stdout)
}

func writeRealFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", relativePath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relativePath, err)
	}
}

func chmodRealFile(t *testing.T, root, relativePath string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(filepath.Join(root, filepath.FromSlash(relativePath)), mode); err != nil {
		t.Fatalf("chmod %s: %v", relativePath, err)
	}
}

func readRealFile(t *testing.T, root, relativePath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(data)
}

func helperExitCode() int {
	if value := os.Getenv("GITEXEC_HELPER_EXIT"); value != "" {
		var code int
		if _, err := fmt.Sscanf(value, "%d", &code); err == nil {
			return code
		}
	}
	return 0
}

func helperRevParse(args []string) {
	if len(args) == 0 {
		helperFprintln(os.Stderr, "missing rev-parse arg")
		os.Exit(2)
	}
	switch args[0] {
	case "--is-inside-work-tree":
		helperFprintln(os.Stdout, "true")
	case "--show-prefix":
		helperFprintln(os.Stdout, "sub/")
	case "--show-toplevel":
		helperFprintln(os.Stdout, "/repo")
	case "--git-path":
		if len(args) != 2 {
			helperFprintln(os.Stderr, "--git-path requires path")
			os.Exit(2)
		}
		helperFprintf(os.Stdout, ".git/%s\n", args[1])
	default:
		helperFprintf(os.Stdout, "%s-resolved\n", args[0])
	}
}
