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

func TestIsInsideWorkTreeTreatsNonRepositoryAsFalse(t *testing.T) {
	git := Git{Runner: helperRunner(t, map[string]string{"GITEXEC_HELPER_NOT_REPO": "1"})}

	inside, err := git.IsInsideWorkTree(context.Background())
	if err != nil {
		t.Fatalf("IsInsideWorkTree returned error: %v", err)
	}
	if inside {
		t.Fatal("IsInsideWorkTree = true, want false")
	}
}

func TestCommitVerboseAndMessageFileUseExpectedCleanup(t *testing.T) {
	git := Git{Runner: helperRunner(t, nil)}

	var stdout, stderr bytes.Buffer
	if err := git.CommitVerbose(context.Background(), strings.NewReader("plain input\n"), &stdout, &stderr); err != nil {
		t.Fatalf("CommitVerbose returned error: %v", err)
	}
	if got, want := strings.Split(strings.TrimSpace(stdout.String()), "\n"), []string{"commit", "-v"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CommitVerbose args = %#v, want %#v", got, want)
	}

	stdout.Reset()
	stderr.Reset()
	if err := git.CommitVerboseMessageFile(context.Background(), "/tmp/message.txt", strings.NewReader("message input\n"), &stdout, &stderr); err != nil {
		t.Fatalf("CommitVerboseMessageFile returned error: %v", err)
	}
	want := []string{"commit", "--cleanup=strip", "-v", "-F", "/tmp/message.txt", "-e"}
	if got := strings.Split(strings.TrimSpace(stdout.String()), "\n"); !reflect.DeepEqual(got, want) {
		t.Fatalf("CommitVerboseMessageFile args = %#v, want %#v", got, want)
	}
}

func TestCommitVerboseMessageFileRequiresPath(t *testing.T) {
	git := Git{Runner: helperRunner(t, nil)}

	err := git.CommitVerboseMessageFile(context.Background(), "", strings.NewReader("input\n"), io.Discard, io.Discard)

	if err == nil || !strings.Contains(err.Error(), "commit message path is required") {
		t.Fatalf("CommitVerboseMessageFile error = %v, want required path", err)
	}
}

func TestWriteTree(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, "tracked.txt", "base\n")
	realGit(t, repo, "add", ".")
	want := realGitOutput(t, repo, "write-tree")

	got, err := New(repo, false, nil).WriteTree(context.Background())

	if err != nil {
		t.Fatalf("WriteTree returned error: %v", err)
	}
	if got != want {
		t.Fatalf("WriteTree = %q, want %q", got, want)
	}
}

func TestCoreCommentChar(t *testing.T) {
	repo := initRealRepo(t)
	git := New(repo, false, nil)

	if value, ok, err := git.CoreCommentChar(context.Background()); err != nil || ok || value != "" {
		t.Fatalf("unset CoreCommentChar = %q, %v, %v; want unset nil", value, ok, err)
	}

	realGit(t, repo, "config", "--local", "core.commentChar", ";")
	if value, ok, err := git.CoreCommentChar(context.Background()); err != nil || !ok || value != ";" {
		t.Fatalf("single-character CoreCommentChar = %q, %v, %v; want ; true nil", value, ok, err)
	}

	realGit(t, repo, "config", "--local", "core.commentChar", " ")
	if value, ok, err := git.CoreCommentChar(context.Background()); err != nil || !ok || value != " " {
		t.Fatalf("space CoreCommentChar = %q, %v, %v; want space true nil", value, ok, err)
	}

	realGit(t, repo, "config", "--local", "core.commentChar", "auto")
	if value, ok, err := git.CoreCommentChar(context.Background()); err != nil || !ok || value != "auto" {
		t.Fatalf("auto CoreCommentChar = %q, %v, %v; want auto true nil", value, ok, err)
	}
}

func TestHistoryHelpersReadCommitsFilesAndTrees(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, "mirror/file.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base mirror")
	writeRealFile(t, repo, "mirror/file.txt", "changed\n")
	realGit(t, repo, "commit", "-am", "change mirror", "-m", "body line\n\nlast line")
	head := realGitOutput(t, repo, "rev-parse", "HEAD")

	git := New(repo, false, nil)
	commits, err := git.FirstParentCommits(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("FirstParentCommits returned error: %v", err)
	}
	if len(commits) != 2 || commits[0] != head {
		t.Fatalf("FirstParentCommits = %#v, want HEAD first", commits)
	}

	pathCommits, err := git.LogCommitsTouchingPath(context.Background(), "HEAD", "mirror/file.txt")
	if err != nil {
		t.Fatalf("LogCommitsTouchingPath returned error: %v", err)
	}
	if len(pathCommits) != 2 {
		t.Fatalf("path commits = %#v, want two commits", pathCommits)
	}
	got := pathCommits[1]
	if got.Hash != head || got.Subject != "change mirror" {
		t.Fatalf("latest path commit = %#v, want head/change mirror", got)
	}
	if want := "change mirror\n\nbody line\n\nlast line"; got.Message != want {
		t.Fatalf("commit message = %q, want %q", got.Message, want)
	}

	data, ok, err := git.ShowFile(context.Background(), "HEAD", "mirror/file.txt")
	if err != nil {
		t.Fatalf("ShowFile returned error: %v", err)
	}
	if !ok || string(data) != "changed\n" {
		t.Fatalf("ShowFile = %q, %v; want changed true", data, ok)
	}
	if data, ok, err := git.ShowFile(context.Background(), "HEAD", "missing.txt"); err != nil || ok || data != nil {
		t.Fatalf("missing ShowFile = %q, %v, %v; want nil false nil", data, ok, err)
	}

	item, err := git.TreeItem(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("TreeItem returned error: %v", err)
	}
	if item.Mode != "040000" || item.Type != "tree" || item.Hash == "" {
		t.Fatalf("TreeItem = %#v, want tree item", item)
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

func TestStatusPorcelainPathspecsWithIgnoredReportsIgnoredOnlyState(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, ".gitignore", "target/ignored.log\n")
	writeRealFile(t, repo, "target/tracked.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base")
	writeRealFile(t, repo, "target/ignored.log", "ignored\n")

	git := New(repo, false, nil)
	defaultStatus, err := git.StatusPorcelainPathspecs(context.Background(), "target")
	if err != nil {
		t.Fatalf("StatusPorcelainPathspecs returned error: %v", err)
	}
	if defaultStatus != "" {
		t.Fatalf("default status = %q, want ignored-only path omitted", defaultStatus)
	}

	ignoredStatus, err := git.StatusPorcelainPathspecsWithIgnored(context.Background(), "target")
	if err != nil {
		t.Fatalf("StatusPorcelainPathspecsWithIgnored returned error: %v", err)
	}
	if !strings.Contains(ignoredStatus, "!! target/ignored.log") {
		t.Fatalf("ignored status = %q, want ignored path", ignoredStatus)
	}
}

func TestStashPushAllPathspecsRestoresSelectedStateAndPreservesUnrelatedState(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, ".gitignore", "mirror/ignored.log\noutside-ignored.log\n")
	writeRealFile(t, repo, "mirror/file.txt", "base\n")
	writeRealFile(t, repo, "mirror/delete.txt", "delete\n")
	writeRealFile(t, repo, "outside.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base")

	writeRealFile(t, repo, "mirror/file.txt", "staged\n")
	realGit(t, repo, "add", "mirror/file.txt")
	writeRealFile(t, repo, "mirror/file.txt", "unstaged\n")
	if err := os.Remove(filepath.Join(repo, "mirror", "delete.txt")); err != nil {
		t.Fatalf("remove selected tracked file: %v", err)
	}
	writeRealFile(t, repo, "mirror/new.txt", "new\n")
	writeRealFile(t, repo, "mirror/ignored.log", "ignored\n")
	writeRealFile(t, repo, "outside.txt", "outside staged\n")
	realGit(t, repo, "add", "outside.txt")
	writeRealFile(t, repo, "outside.txt", "outside unstaged\n")
	writeRealFile(t, repo, "outside-ignored.log", "outside ignored\n")

	git := New(repo, false, nil)
	entry, saved, err := git.StashPushAllPathspecs(context.Background(), "braid sync autostash test", "mirror")
	if err != nil {
		t.Fatalf("StashPushAllPathspecs returned error: %v", err)
	}
	if !saved {
		t.Fatal("StashPushAllPathspecs saved = false, want true")
	}
	if entry.OID == "" || entry.Message != "braid sync autostash test" {
		t.Fatalf("stash entry = %#v, want oid and message", entry)
	}

	selectedStatus, err := git.StatusPorcelainPathspecsWithIgnored(context.Background(), "mirror")
	if err != nil {
		t.Fatalf("selected status after stash returned error: %v", err)
	}
	if selectedStatus != "" {
		t.Fatalf("selected status after stash = %q, want clean", selectedStatus)
	}
	assertNoRealFile(t, repo, "mirror/new.txt")
	assertNoRealFile(t, repo, "mirror/ignored.log")
	if got := readRealFile(t, repo, "mirror/delete.txt"); got != "delete\n" {
		t.Fatalf("mirror/delete.txt after stash = %q, want restored tracked file", got)
	}

	outsideStatus, err := git.StatusPorcelainPathspecsWithIgnored(context.Background(), "outside.txt", "outside-ignored.log")
	if err != nil {
		t.Fatalf("outside status after stash returned error: %v", err)
	}
	if !strings.Contains(outsideStatus, "MM outside.txt") || !strings.Contains(outsideStatus, "!! outside-ignored.log") {
		t.Fatalf("outside status after stash = %q, want staged/unstaged and ignored outside state", outsideStatus)
	}
	if got := realGitOutput(t, repo, "show", ":outside.txt"); got != "outside staged" {
		t.Fatalf("staged outside.txt = %q, want outside staged", got)
	}
	if got := readRealFile(t, repo, "outside.txt"); got != "outside unstaged\n" {
		t.Fatalf("worktree outside.txt = %q, want outside unstaged", got)
	}

	if err := git.StashApply(context.Background(), entry.OID); err != nil {
		t.Fatalf("StashApply returned error: %v", err)
	}
	if err := git.RestoreStashIndexPathspecs(context.Background(), entry.OID, "mirror"); err != nil {
		t.Fatalf("RestoreStashIndexPathspecs returned error: %v", err)
	}
	restoredStatus, err := git.StatusPorcelainPathspecsWithIgnored(context.Background(), "mirror")
	if err != nil {
		t.Fatalf("selected status after apply returned error: %v", err)
	}
	for _, want := range []string{"MM mirror/file.txt", " D mirror/delete.txt", "?? mirror/new.txt", "!! mirror/ignored.log"} {
		if !strings.Contains(restoredStatus, want) {
			t.Fatalf("selected status after apply = %q, want %q", restoredStatus, want)
		}
	}
	if got := realGitOutput(t, repo, "show", ":mirror/file.txt"); got != "staged" {
		t.Fatalf("staged mirror/file.txt = %q, want staged", got)
	}
	if got := readRealFile(t, repo, "mirror/file.txt"); got != "unstaged\n" {
		t.Fatalf("worktree mirror/file.txt = %q, want unstaged", got)
	}

	selector, err := git.DropStashEntry(context.Background(), entry)
	if err != nil {
		t.Fatalf("DropStashEntry returned error: %v", err)
	}
	if selector == "" {
		t.Fatal("DropStashEntry selector is empty")
	}
	entries, err := git.StashList(context.Background())
	if err != nil {
		t.Fatalf("StashList returned error: %v", err)
	}
	for _, stash := range entries {
		if stash.OID == entry.OID {
			t.Fatalf("stash list still contains dropped entry: %#v", entries)
		}
	}
	outsideStatusAfterApply, err := git.StatusPorcelainPathspecsWithIgnored(context.Background(), "outside.txt", "outside-ignored.log")
	if err != nil {
		t.Fatalf("outside status after apply returned error: %v", err)
	}
	if outsideStatusAfterApply != outsideStatus {
		t.Fatalf("outside status changed from %q to %q", outsideStatus, outsideStatusAfterApply)
	}
}

func TestStashDropResolvesCurrentSelectorWithExistingUserStashes(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, "tracked.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base")

	writeRealFile(t, repo, "tracked.txt", "user before\n")
	realGit(t, repo, "stash", "push", "-m", "user before")

	writeRealFile(t, repo, "tracked.txt", "braid\n")
	git := New(repo, false, nil)
	entry, saved, err := git.StashPushAllPathspecs(context.Background(), "braid sync autostash selector test", "tracked.txt")
	if err != nil {
		t.Fatalf("StashPushAllPathspecs returned error: %v", err)
	}
	if !saved {
		t.Fatal("StashPushAllPathspecs saved = false, want true")
	}

	writeRealFile(t, repo, "tracked.txt", "user after\n")
	realGit(t, repo, "stash", "push", "-m", "user after")

	selector, err := git.ResolveStashSelector(context.Background(), entry)
	if err != nil {
		t.Fatalf("ResolveStashSelector returned error: %v", err)
	}
	if selector != "stash@{1}" {
		t.Fatalf("selector = %q, want stash@{1}", selector)
	}
	droppedSelector, err := git.DropStashEntry(context.Background(), entry)
	if err != nil {
		t.Fatalf("DropStashEntry returned error: %v", err)
	}
	if droppedSelector != selector {
		t.Fatalf("dropped selector = %q, want %q", droppedSelector, selector)
	}
	entries, err := git.StashList(context.Background())
	if err != nil {
		t.Fatalf("StashList returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("stash entries = %#v, want two user stashes", entries)
	}
	for _, stash := range entries {
		if stash.OID == entry.OID || strings.Contains(stash.Subject, "braid sync autostash selector test") {
			t.Fatalf("stash list still contains Braid entry: %#v", entries)
		}
	}
}

func TestStashLookupErrorsLeaveEntriesRecoverable(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		repo := initRealRepo(t)
		writeRealFile(t, repo, "tracked.txt", "base\n")
		realGit(t, repo, "add", ".")
		realGit(t, repo, "commit", "-m", "base")
		writeRealFile(t, repo, "tracked.txt", "change\n")
		git := New(repo, false, nil)
		entry, saved, err := git.StashPushAllPathspecs(context.Background(), "braid sync autostash missing test", "tracked.txt")
		if err != nil {
			t.Fatalf("StashPushAllPathspecs returned error: %v", err)
		}
		if !saved {
			t.Fatal("StashPushAllPathspecs saved = false, want true")
		}
		realGit(t, repo, "stash", "drop", "stash@{0}")

		_, err = git.ResolveStashSelector(context.Background(), entry)
		var lookupErr *StashLookupError
		if !errors.As(err, &lookupErr) || len(lookupErr.Matches) != 0 {
			t.Fatalf("ResolveStashSelector error = %#v, want missing StashLookupError", err)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		repo := initRealRepo(t)
		writeRealFile(t, repo, "tracked.txt", "base\n")
		realGit(t, repo, "add", ".")
		realGit(t, repo, "commit", "-m", "base")
		writeRealFile(t, repo, "tracked.txt", "change\n")
		git := New(repo, false, nil)
		entry, saved, err := git.StashPushAllPathspecs(context.Background(), "braid sync autostash ambiguous test", "tracked.txt")
		if err != nil {
			t.Fatalf("StashPushAllPathspecs returned error: %v", err)
		}
		if !saved {
			t.Fatal("StashPushAllPathspecs saved = false, want true")
		}
		_, err = resolveStashSelectorFromList(entry, []StashListEntry{
			{Selector: "stash@{0}", OID: entry.OID, Subject: "On main: " + entry.Message},
			{Selector: "stash@{1}", OID: entry.OID, Subject: "On main: " + entry.Message},
		})
		var lookupErr *StashLookupError
		if !errors.As(err, &lookupErr) || len(lookupErr.Matches) != 2 {
			t.Fatalf("ResolveStashSelector error = %#v, want ambiguous StashLookupError", err)
		}
		entries, listErr := git.StashList(context.Background())
		if listErr != nil {
			t.Fatalf("StashList returned error: %v", listErr)
		}
		if len(entries) != 1 || entries[0].OID != entry.OID {
			t.Fatalf("stash entries = %#v, want saved entry recoverable", entries)
		}
	})
}

func TestStashPushAllPathspecsReturnsUnsavedForCleanPathspec(t *testing.T) {
	repo := initRealRepo(t)
	writeRealFile(t, repo, "tracked.txt", "base\n")
	realGit(t, repo, "add", ".")
	realGit(t, repo, "commit", "-m", "base")

	entry, saved, err := New(repo, false, nil).StashPushAllPathspecs(context.Background(), "braid sync autostash clean test", "tracked.txt")
	if err != nil {
		t.Fatalf("StashPushAllPathspecs returned error: %v", err)
	}
	if saved || entry.OID != "" {
		t.Fatalf("entry = %#v saved = %v, want no saved entry", entry, saved)
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
		if !reflect.DeepEqual(merged.ConflictPaths, []string{"file.txt"}) {
			t.Fatalf("conflict paths = %#v, want file.txt", merged.ConflictPaths)
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

func TestParseMergeTreeOutputZUsesStructuredConflictPaths(t *testing.T) {
	output := "merged-tree\x00" +
		"file.txt\x00" +
		"dir/path with spaces.txt\x00" +
		"file.txt\x00" +
		"\x00" +
		"1\x00message-only.txt\x00Auto-merging\x00Auto-merging message-only.txt\n\x00" +
		"1\x00message-only.txt\x00CONFLICT (contents)\x00CONFLICT (content): Merge conflict in message-only.txt\n\x00"

	merged := parseMergeTreeOutput(output)

	if merged.Tree != "merged-tree" {
		t.Fatalf("tree = %q, want merged-tree", merged.Tree)
	}
	wantPaths := []string{"file.txt", "dir/path with spaces.txt"}
	if !reflect.DeepEqual(merged.ConflictPaths, wantPaths) {
		t.Fatalf("conflict paths = %#v, want %#v", merged.ConflictPaths, wantPaths)
	}
	if strings.Contains(strings.Join(merged.ConflictPaths, "\n"), "message-only.txt") {
		t.Fatalf("message path was parsed as conflict path: %#v", merged.ConflictPaths)
	}
	if !strings.Contains(merged.Details, "Auto-merging message-only.txt") || !strings.Contains(merged.Details, "CONFLICT (content): Merge conflict in message-only.txt") {
		t.Fatalf("details = %q, want informational messages", merged.Details)
	}
}

func TestParseMergeTreeOutputZEmptyConflictPathSectionUsesConflictMessagePaths(t *testing.T) {
	output := "merged-tree\x00" +
		"\x00" +
		"1\x00message-only.txt\x00CONFLICT (contents)\x00CONFLICT (content): Merge conflict in message-only.txt\n\x00"

	merged := parseMergeTreeOutput(output)

	if merged.Tree != "merged-tree" {
		t.Fatalf("tree = %q, want merged-tree", merged.Tree)
	}
	if !reflect.DeepEqual(merged.ConflictPaths, []string{"message-only.txt"}) {
		t.Fatalf("conflict paths = %#v, want message-only.txt", merged.ConflictPaths)
	}
	if !strings.Contains(merged.Details, "CONFLICT (content): Merge conflict in message-only.txt") {
		t.Fatalf("details = %q, want informational conflict message", merged.Details)
	}
}

func TestParseMergeTreeOutputZMessageRecordWithMultiplePaths(t *testing.T) {
	output := "merged-tree\x00" +
		"file.txt\x00" +
		"\x00" +
		"2\x00ours.txt\x00theirs.txt\x00CONFLICT (rename/delete)\x00CONFLICT (rename/delete): ours.txt renamed to theirs.txt\n\x00"

	merged := parseMergeTreeOutput(output)

	if !reflect.DeepEqual(merged.ConflictPaths, []string{"file.txt"}) {
		t.Fatalf("conflict paths = %#v, want file.txt", merged.ConflictPaths)
	}
	if merged.Details != "CONFLICT (rename/delete): ours.txt renamed to theirs.txt\n" {
		t.Fatalf("details = %q, want multi-path message text", merged.Details)
	}
}

func TestMergeTreeWriteConflictWithoutStructuredPathsUsesFallback(t *testing.T) {
	git := Git{Runner: helperRunner(t, map[string]string{
		"GITEXEC_HELPER_EXIT":   "1",
		"GITEXEC_HELPER_STDOUT": "merged-tree\n",
	})}

	merged, err := git.MergeTreeWrite(context.Background(), "base", "local", "remote")

	if err == nil {
		t.Fatal("MergeTreeWrite returned nil error for conflict exit")
	}
	if merged.Tree != "merged-tree" {
		t.Fatalf("tree = %q, want merged-tree", merged.Tree)
	}
	if !reflect.DeepEqual(merged.ConflictPaths, []string{"(unknown path)"}) {
		t.Fatalf("conflict paths = %#v, want fallback unknown path", merged.ConflictPaths)
	}
}

func TestMergeTreeWriteConflictWithoutMessagePathsRetriesWithoutMessages(t *testing.T) {
	git := Git{Runner: helperRunner(t, map[string]string{
		"GITEXEC_HELPER_EXIT":                         "1",
		"GITEXEC_HELPER_EMPTY_MESSAGE_CONFLICT_PATHS": "1",
		"GITEXEC_HELPER_FALLBACK_CONFLICT_PATH":       "file.txt",
	})}

	merged, err := git.MergeTreeWrite(context.Background(), "base", "local", "remote")

	if err == nil {
		t.Fatal("MergeTreeWrite returned nil error for conflict exit")
	}
	if merged.Tree != "merged-tree" {
		t.Fatalf("tree = %q, want merged-tree", merged.Tree)
	}
	if !reflect.DeepEqual(merged.ConflictPaths, []string{"file.txt"}) {
		t.Fatalf("conflict paths = %#v, want file.txt", merged.ConflictPaths)
	}
}

func TestParseMergeTreeOutputZEmptyConflictPathSectionIgnoresNonConflictMessagePaths(t *testing.T) {
	output := "merged-tree\x00" +
		"\x00" +
		"1\x00message-only.txt\x00Auto-merging\x00Auto-merging message-only.txt\n\x00"

	merged := parseMergeTreeOutput(output)

	if merged.Tree != "merged-tree" {
		t.Fatalf("tree = %q, want merged-tree", merged.Tree)
	}
	if len(merged.ConflictPaths) != 0 {
		t.Fatalf("conflict paths = %#v, want none from non-conflict messages", merged.ConflictPaths)
	}
	if !strings.Contains(merged.Details, "Auto-merging message-only.txt") {
		t.Fatalf("details = %q, want informational auto-merge message", merged.Details)
	}
}

func TestMergeTreeWriteConflictWithEmptyStructuredPathSectionUsesMessagePaths(t *testing.T) {
	git := Git{Runner: helperRunner(t, map[string]string{
		"GITEXEC_HELPER_EXIT":                        "1",
		"GITEXEC_HELPER_EMPTY_CONFLICT_PATH_SECTION": "1",
	})}

	merged, err := git.MergeTreeWrite(context.Background(), "base", "local", "remote")

	if err == nil {
		t.Fatal("MergeTreeWrite returned nil error for conflict exit")
	}
	if merged.Tree != "merged-tree" {
		t.Fatalf("tree = %q, want merged-tree", merged.Tree)
	}
	if !reflect.DeepEqual(merged.ConflictPaths, []string{"message-only.txt"}) {
		t.Fatalf("conflict paths = %#v, want message-only.txt", merged.ConflictPaths)
	}
	if !strings.Contains(merged.Details, "CONFLICT (content): Merge conflict in message-only.txt") {
		t.Fatalf("details = %q, want informational conflict message", merged.Details)
	}
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
	case "merge-tree":
		if os.Getenv("GITEXEC_HELPER_EMPTY_MESSAGE_CONFLICT_PATHS") == "1" {
			if helperHasArg(args, "--messages") {
				helperFprint(os.Stdout, "merged-tree\x00\x00")
			} else if helperHasArg(args, "--no-messages") {
				helperFprint(os.Stdout, "merged-tree\x00", os.Getenv("GITEXEC_HELPER_FALLBACK_CONFLICT_PATH"), "\x00")
			}
			os.Exit(helperExitCode())
		}
		if os.Getenv("GITEXEC_HELPER_EMPTY_CONFLICT_PATH_SECTION") == "1" {
			helperFprint(os.Stdout,
				"merged-tree\x00"+
					"\x00"+
					"1\x00message-only.txt\x00CONFLICT (contents)\x00CONFLICT (content): Merge conflict in message-only.txt\n\x00")
		} else {
			helperFprint(os.Stdout, os.Getenv("GITEXEC_HELPER_STDOUT"))
		}
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
	case "commit":
		for _, arg := range args {
			helperFprintln(os.Stdout, arg)
		}
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

func helperHasArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
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

func assertNoRealFile(t *testing.T, root, relativePath string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err == nil {
		t.Fatalf("%s exists, want absent", relativePath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", relativePath, err)
	}
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
		if os.Getenv("GITEXEC_HELPER_NOT_REPO") == "1" {
			helperFprintln(os.Stderr, "fatal: not a git repository (or any of the parent directories): .git")
			os.Exit(128)
		}
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
