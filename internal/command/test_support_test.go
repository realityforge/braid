package command

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
	"braid/internal/testutil"
)

func initDownstream(t *testing.T) string {
	t.Helper()
	repo := testutil.InitRepo(t)
	testutil.WriteFile(t, repo, "README.md", "downstream\n")
	testutil.CommitAll(t, repo, "downstream")
	return repo
}

func runCommandOK(t *testing.T, repo string, args []string) string {
	t.Helper()
	return runCommandOKInDir(t, repo, repo, args)
}

func runCommandOKInDir(t *testing.T, repo, dir string, args []string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_LOCAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: dir}).Run(args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("braid %v exit = %d, stderr = %q", args, code, stderr.String())
	}
	return stdout.String()
}

func runCommandOKInDirWithOptions(t *testing.T, repo, dir string, options Options, args []string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_LOCAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
	t.Chdir(dir)
	options.WorkDir = dir
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(options).Run(args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("braid %v exit = %d, stderr = %q", args, code, stderr.String())
	}
	return stdout.String()
}

func runCommandOKWithOutput(t *testing.T, repo string, args []string) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_LOCAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
	t.Chdir(repo)
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: repo}).Run(args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("braid %v exit = %d, stderr = %q", args, code, stderr.String())
	}
	return stdout.String(), stderr.String()
}

func runCommandError(t *testing.T, repo string, args []string) string {
	t.Helper()
	return runCommandErrorInDir(t, repo, repo, args)
}

func runCommandErrorInDir(t *testing.T, repo, dir string, args []string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_LOCAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: dir}).Run(args, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("braid %v succeeded unexpectedly, stdout = %q", args, stdout.String())
	}
	return stderr.String()
}

func runCommandErrorWithOutput(t *testing.T, repo string, args []string) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_LOCAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
	t.Chdir(repo)
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: repo}).Run(args, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("braid %v succeeded unexpectedly, stdout = %q", args, stdout.String())
	}
	return stdout.String(), stderr.String()
}

func testRepoContext(repo string, git Git) RepoContext {
	return RepoContext{
		ProcessWorkDir:      repo,
		GitWorkTreeRoot:     repo,
		LogicalWorkTreeRoot: repo,
		RootGit:             git,
		ProcessGit:          git,
	}
}

func loadMirror(t *testing.T, repo, localPath string) mirror.Mirror {
	t.Helper()
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	m, err := cfg.GetRequired(localPath)
	if err != nil {
		t.Fatalf("GetRequired(%q): %v", localPath, err)
	}
	return m
}

func assertFile(t *testing.T, repo, relativePath, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", relativePath, string(data), want)
	}
}

func assertNoFile(t *testing.T, repo, relativePath string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(repo, filepath.FromSlash(relativePath)))
	if err == nil {
		t.Fatalf("%s exists, want absent", relativePath)
	}
	if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", relativePath, err)
	}
}

func assertCommitSubject(t *testing.T, repo, want string) {
	t.Helper()
	got := strings.TrimSpace(testutil.Git(t, repo, "log", "-1", "--pretty=%s").Stdout)
	if got != want {
		t.Fatalf("commit subject = %q, want %q", got, want)
	}
}

func assertCommitIdentity(t *testing.T, repo string) {
	t.Helper()
	got := strings.TrimSpace(testutil.Git(t, repo, "log", "-1", "--pretty=%an <%ae>").Stdout)
	want := testutil.DefaultName + " <" + testutil.DefaultEmail + ">"
	if got != want {
		t.Fatalf("commit identity = %q, want %q", got, want)
	}
}

func assertNoRemote(t *testing.T, repo, remote string) {
	t.Helper()
	remotes := strings.Fields(testutil.Git(t, repo, "remote").Stdout)
	for _, got := range remotes {
		if got == remote {
			t.Fatalf("remote %q still exists", remote)
		}
	}
}

func assertClean(t *testing.T, repo string) {
	t.Helper()
	if status := strings.TrimSpace(testutil.Git(t, repo, "status", "--porcelain").Stdout); status != "" {
		t.Fatalf("status --porcelain = %q, want clean", status)
	}
}

func assertContains(t *testing.T, value, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("output does not contain %q:\n%s", want, value)
	}
}

func assertNotContains(t *testing.T, value, unwanted string) {
	t.Helper()
	if strings.Contains(value, unwanted) {
		t.Fatalf("output contains %q unexpectedly:\n%s", unwanted, value)
	}
}

func assertInOrder(t *testing.T, value string, needles ...string) {
	t.Helper()
	offset := 0
	for _, needle := range needles {
		index := strings.Index(value[offset:], needle)
		if index < 0 {
			t.Fatalf("%q does not appear after offset %d in:\n%s", needle, offset, value)
		}
		offset += index + len(needle)
	}
}

func writeFailingPreCommitHook(t *testing.T, repo string) {
	t.Helper()
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write pre-commit hook: %v", err)
	}
}

func writePostCommitHook(t *testing.T, repo string) {
	t.Helper()
	hook := filepath.Join(repo, ".git", "hooks", "post-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf 'ran\\n' > post-commit-ran\n"), 0o755); err != nil {
		t.Fatalf("write post-commit hook: %v", err)
	}
}

func withUserCacheDir(t *testing.T, dir string, err error) {
	t.Helper()
	previous := userCacheDir
	userCacheDir = func() (string, error) {
		return dir, err
	}
	t.Cleanup(func() {
		userCacheDir = previous
	})
}

func envLookup(values map[string]string) EnvLookup {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func writeEditor(t *testing.T, message string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\nprintf '" + message + "\\n' > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write editor: %v", err)
	}
	return gitEditorCommand(path)
}

func writeGenerator(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "generator.sh")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write generator: %v", err)
	}
	return path
}

func writeStdinEditor(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\nIFS= read -r message || exit 1\nprintf '%s\\n' \"$message\" > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stdin editor: %v", err)
	}
	return gitEditorCommand(path)
}

func writeFailingEditor(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write failing editor: %v", err)
	}
	return path
}

func writeCapturingEditor(t *testing.T, message string) (string, string) {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "commit-message.txt")
	t.Setenv("BRAID_EDITOR_CAPTURE", capture)
	t.Setenv("BRAID_EDITOR_MESSAGE", message)
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\ncp \"$1\" \"$BRAID_EDITOR_CAPTURE\" || exit 1\nprintf '%s\\n' \"$BRAID_EDITOR_MESSAGE\" > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write capturing editor: %v", err)
	}
	return capture, gitEditorCommand(path)
}

func writePrependCapturingEditor(t *testing.T, message string) (string, string) {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "commit-message.txt")
	t.Setenv("BRAID_EDITOR_CAPTURE", capture)
	t.Setenv("BRAID_EDITOR_MESSAGE", message)
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\ncp \"$1\" \"$BRAID_EDITOR_CAPTURE\" || exit 1\ntmp=\"$1.tmp\"\nprintf '%s\\n\\n' \"$BRAID_EDITOR_MESSAGE\" > \"$tmp\" || exit 1\ncat \"$1\" >> \"$tmp\" || exit 1\nmv \"$tmp\" \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write prepend editor: %v", err)
	}
	return capture, gitEditorCommand(path)
}

func writeSequenceCapturingEditor(t *testing.T, messagePrefix string) (string, string) {
	t.Helper()
	captureDir := t.TempDir()
	t.Setenv("BRAID_EDITOR_CAPTURE_DIR", captureDir)
	t.Setenv("BRAID_EDITOR_MESSAGE_PREFIX", messagePrefix)
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\ndir=\"$BRAID_EDITOR_CAPTURE_DIR\"\ncount_file=\"$dir/count\"\nif [ -f \"$count_file\" ]; then count=$(cat \"$count_file\"); else count=0; fi\ncount=$((count + 1))\nprintf '%s\\n' \"$count\" > \"$count_file\" || exit 1\ncp \"$1\" \"$dir/template-$count.txt\" || exit 1\nprintf '%s %s\\n' \"$BRAID_EDITOR_MESSAGE_PREFIX\" \"$count\" > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write sequence editor: %v", err)
	}
	return captureDir, gitEditorCommand(path)
}

func writeSequenceCapturingEditorFailAt(t *testing.T, messagePrefix string, failAt int) string {
	t.Helper()
	captureDir := t.TempDir()
	t.Setenv("BRAID_EDITOR_CAPTURE_DIR", captureDir)
	t.Setenv("BRAID_EDITOR_MESSAGE_PREFIX", messagePrefix)
	t.Setenv("BRAID_EDITOR_FAIL_AT", fmt.Sprintf("%d", failAt))
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\ndir=\"$BRAID_EDITOR_CAPTURE_DIR\"\ncount_file=\"$dir/count\"\nif [ -f \"$count_file\" ]; then count=$(cat \"$count_file\"); else count=0; fi\ncount=$((count + 1))\nprintf '%s\\n' \"$count\" > \"$count_file\" || exit 1\ncp \"$1\" \"$dir/template-$count.txt\" || exit 1\nif [ \"$count\" = \"$BRAID_EDITOR_FAIL_AT\" ]; then exit 1; fi\nprintf '%s %s\\n' \"$BRAID_EDITOR_MESSAGE_PREFIX\" \"$count\" > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write failing sequence editor: %v", err)
	}
	return gitEditorCommand(path)
}

func gitEditorCommand(path string) string {
	if runtime.GOOS == "windows" {
		return shellQuote(path)
	}
	return path
}

func commitAllWithMessage(t *testing.T, repo, subject string, bodies ...string) string {
	t.Helper()
	testutil.Git(t, repo, "add", ".")
	args := []string{"commit", "-m", subject}
	for _, body := range bodies {
		args = append(args, "-m", body)
	}
	testutil.Git(t, repo, args...)
	return testutil.CurrentRevision(t, repo)
}

func writeSingleMirrorConfig(t *testing.T, repo string, m mirror.Mirror) {
	t.Helper()
	cfg := config.Empty()
	if err := cfg.Add(m); err != nil {
		t.Fatalf("add mirror config: %v", err)
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("write mirror config: %v", err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func skippedLockedOutput(paths ...string) string {
	var out strings.Builder
	out.WriteString("Braid: skipped revision-locked mirrors:\n")
	for _, path := range paths {
		out.WriteString("  ")
		out.WriteString(path)
		out.WriteString("\n")
	}
	return out.String()
}

func writeLockedMirrorConfig(t *testing.T, repo string, paths ...string) {
	t.Helper()
	cfg := config.Empty()
	for _, path := range paths {
		if err := cfg.Add(mirror.Mirror{
			Path:     path,
			URL:      filepath.Join(t.TempDir(), "missing-upstream"),
			Revision: strings.Repeat("1", 40),
		}); err != nil {
			t.Fatalf("add locked mirror config: %v", err)
		}
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("write config: %v", err)
	}
	testutil.Git(t, repo, "add", config.FileName)
	testutil.Git(t, repo, "commit", "-m", "configure locked mirrors")
}

type mergeTreeForbiddenGit struct {
	gitexec.Git
	t *testing.T
}

func forbidMergeTreeGit(t *testing.T, repo string) *mergeTreeForbiddenGit {
	t.Helper()
	return &mergeTreeForbiddenGit{Git: gitexec.New(repo, false, nil), t: t}
}

func (g *mergeTreeForbiddenGit) MergeTreeWrite(context.Context, string, string, string) (gitexec.MergeTreeResult, error) {
	g.t.Helper()
	g.t.Fatal("MergeTreeWrite was called for update fast path")
	return gitexec.MergeTreeResult{}, nil
}

type processAwareMergeTreeForbiddenGit struct {
	*mergeTreeForbiddenGit
	processGit gitexec.Git
}

func forbidMergeTreeGitFromProcessDir(t *testing.T, repo, processDir string) *processAwareMergeTreeForbiddenGit {
	t.Helper()
	return &processAwareMergeTreeForbiddenGit{
		mergeTreeForbiddenGit: forbidMergeTreeGit(t, repo),
		processGit:            gitexec.New(processDir, false, nil),
	}
}

func (g *processAwareMergeTreeForbiddenGit) RequireVersion(ctx context.Context, required string) error {
	return g.processGit.RequireVersion(ctx, required)
}

func (g *processAwareMergeTreeForbiddenGit) IsInsideWorkTree(ctx context.Context) (bool, error) {
	return g.processGit.IsInsideWorkTree(ctx)
}

func (g *processAwareMergeTreeForbiddenGit) RelativeWorkingDir(ctx context.Context) (string, error) {
	return g.processGit.RelativeWorkingDir(ctx)
}

func (g *processAwareMergeTreeForbiddenGit) WorkTreeRoot(ctx context.Context) (string, error) {
	return g.processGit.WorkTreeRoot(ctx)
}

func (g *processAwareMergeTreeForbiddenGit) RepoFilePath(ctx context.Context, path string) (string, error) {
	return g.processGit.RepoFilePath(ctx, path)
}
