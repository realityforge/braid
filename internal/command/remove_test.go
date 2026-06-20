package command

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/testutil"
)

func TestRemoveCommandDeletesContentConfigAndRemote(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	runCommandOK(t, repo, []string{"setup", "vendor/basic"})
	remote := loadMirror(t, repo, "vendor/basic").Remote()
	writeFailingPreCommitHook(t, repo)

	runCommandOK(t, repo, []string{"remove", "vendor/basic"})
	assertPathMissing(t, repo, "vendor/basic")
	assertMirrorMissing(t, repo, "vendor/basic")
	assertNoRemote(t, repo, remote)
	assertCommitSubject(t, repo, "Braid: Remove mirror 'vendor/basic'")
	assertClean(t, repo)
}

func TestRemoveCommandPreservesUnrelatedIndexAndWorktreeState(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "tracked.txt", "tracked base\n")
	testutil.Git(t, repo, "add", "tracked.txt")
	testutil.Git(t, repo, "commit", "-m", "add unrelated tracked file")

	testutil.WriteFile(t, repo, "staged.txt", "staged content\n")
	testutil.Git(t, repo, "add", "staged.txt")
	testutil.WriteFile(t, repo, "tracked.txt", "tracked dirty\n")
	testutil.WriteFile(t, repo, "untracked.txt", "untracked content\n")

	runCommandOK(t, repo, []string{"remove", "vendor/basic"})

	changed := strings.Fields(testutil.Git(t, repo, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Stdout)
	wantChanged := []string{".braids.json", "vendor/basic/README.md"}
	if strings.Join(changed, "\n") != strings.Join(wantChanged, "\n") {
		t.Fatalf("Braid commit changed %#v, want %#v", changed, wantChanged)
	}
	assertPathMissing(t, repo, "vendor/basic")
	assertMirrorMissing(t, repo, "vendor/basic")
	if got := strings.TrimSpace(testutil.Git(t, repo, "show", ":staged.txt").Stdout); got != "staged content" {
		t.Fatalf("staged blob = %q, want staged content", got)
	}
	assertFile(t, repo, "tracked.txt", "tracked dirty\n")
	assertFile(t, repo, "untracked.txt", "untracked content\n")
	status := testutil.Git(t, repo, "status", "--porcelain").Stdout
	assertContains(t, status, "A  staged.txt")
	assertContains(t, status, " M tracked.txt")
	assertContains(t, status, "?? untracked.txt")
}

func TestRemoveCommandKeepsRemote(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	runCommandOK(t, repo, []string{"setup", "vendor/basic"})
	remote := loadMirror(t, repo, "vendor/basic").Remote()

	runCommandOK(t, repo, []string{"remove", "vendor/basic", "--keep"})
	remotes := testutil.Git(t, repo, "remote").Stdout
	assertContains(t, remotes, remote)
	assertMirrorMissing(t, repo, "vendor/basic")
	assertClean(t, repo)
}

func TestRemoveCommandPathVariants(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(t *testing.T, upstream string)
		addArgs   func(upstream string) []string
		localPath string
	}{
		{
			name: "subdirectory",
			prepare: func(t *testing.T, upstream string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "lib/component.txt", "component\n")
			},
			addArgs:   func(upstream string) []string { return []string{"add", upstream, "vendor/lib", "--path", "lib"} },
			localPath: "vendor/lib",
		},
		{
			name: "path with spaces",
			prepare: func(t *testing.T, upstream string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "spaces\n")
			},
			addArgs:   func(upstream string) []string { return []string{"add", upstream, "vendor/path with spaces"} },
			localPath: "vendor/path with spaces",
		},
		{
			name: "single file",
			prepare: func(t *testing.T, upstream string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "LICENSE.txt", "license\n")
			},
			addArgs: func(upstream string) []string {
				return []string{"add", upstream, "licenses/THIRD_PARTY.txt", "--path", "LICENSE.txt"}
			},
			localPath: "licenses/THIRD_PARTY.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			test.prepare(t, upstream)
			testutil.CommitAll(t, upstream, test.name)
			repo := initDownstream(t)
			runCommandOK(t, repo, test.addArgs(upstream))

			runCommandOK(t, repo, []string{"remove", test.localPath})
			assertPathMissing(t, repo, test.localPath)
			assertMirrorMissing(t, repo, test.localPath)
			assertClean(t, repo)
		})
	}
}

func TestRemoveCommandScopedPrecheckBlocksDirtyConfigAndTarget(t *testing.T) {
	tests := []struct {
		name    string
		dirty   func(t *testing.T, repo string)
		wantErr string
	}{
		{
			name: "config",
			dirty: func(t *testing.T, repo string) {
				t.Helper()
				path := filepath.Join(repo, config.FileName)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read config: %v", err)
				}
				if err := os.WriteFile(path, append(data, []byte(" \n")...), 0o644); err != nil {
					t.Fatalf("dirty config: %v", err)
				}
			},
			wantErr: "local changes are present in .braids.json",
		},
		{
			name: "staged mirror change",
			dirty: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor/basic/README.md", "staged\n")
				testutil.Git(t, repo, "add", "vendor/basic/README.md")
			},
			wantErr: "local changes are present in vendor/basic",
		},
		{
			name: "unstaged mirror change",
			dirty: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor/basic/README.md", "unstaged\n")
			},
			wantErr: "local changes are present in vendor/basic",
		},
		{
			name: "missing tracked mirror content",
			dirty: func(t *testing.T, repo string) {
				t.Helper()
				if err := os.Remove(filepath.Join(repo, "vendor", "basic", "README.md")); err != nil {
					t.Fatalf("remove mirror file: %v", err)
				}
			},
			wantErr: "local changes are present in vendor/basic",
		},
		{
			name: "untracked mirror content",
			dirty: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor/basic/untracked.txt", "untracked\n")
			},
			wantErr: "local changes are present in vendor/basic",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			testutil.WriteFile(t, upstream, "README.md", "base\n")
			testutil.CommitAll(t, upstream, "base")
			repo := initDownstream(t)
			runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

			test.dirty(t, repo)
			stderr := runCommandError(t, repo, []string{"remove", "vendor/basic"})
			assertContains(t, stderr, test.wantErr)
		})
	}
}

func TestRemoveCommandBlocksUnresolvedGitOperationBeforeScopedStatus(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	if err := os.WriteFile(filepath.Join(repo, ".git", "MERGE_HEAD"), []byte("abc123\n"), 0o644); err != nil {
		t.Fatalf("write MERGE_HEAD: %v", err)
	}

	stderr := runCommandError(t, repo, []string{"remove", "vendor/basic"})
	assertContains(t, stderr, "unresolved git operation state is present: MERGE_HEAD")
}

func TestRemoveCommandReportsPostCommitRestoreFailure(t *testing.T) {
	repo := initDownstream(t)
	writeRemoveMirrorConfig(t, repo)
	git := &fakeRemoveGit{restoreErr: errors.New("restore failed")}

	err := RemoveHandler{Options: Options{WorkDir: repo, ConfigRoot: repo}}.remove(context.Background(), git, cli.RemoveOptions{LocalPath: "vendor/basic"})
	if err == nil || !strings.Contains(err.Error(), "restore failed") {
		t.Fatalf("remove error = %v, want restore failure", err)
	}
	if !git.committed {
		t.Fatal("CommitTreeWithTemporaryIndex was not called before restore failure")
	}
}

func TestRemoveCommandReportsPostCommitRemoteInspectFailure(t *testing.T) {
	repo := initDownstream(t)
	writeRemoveMirrorConfig(t, repo)
	git := &fakeRemoveGit{remoteURLErr: errors.New("inspect failed")}

	err := RemoveHandler{Options: Options{WorkDir: repo, ConfigRoot: repo}}.remove(context.Background(), git, cli.RemoveOptions{LocalPath: "vendor/basic"})
	if err == nil || !strings.Contains(err.Error(), `remove committed but failed to inspect Braid remote "main_braid_vendor_basic"`) || !strings.Contains(err.Error(), "inspect failed") {
		t.Fatalf("remove error = %v, want post-commit remote inspect failure", err)
	}
	if !git.committed {
		t.Fatal("CommitTreeWithTemporaryIndex was not called before remote inspect failure")
	}
}

func TestRemoveCommandReportsPostCommitRemoteCleanupFailure(t *testing.T) {
	repo := initDownstream(t)
	writeRemoveMirrorConfig(t, repo)
	git := &fakeRemoveGit{remoteExists: true, remoteRemoveErr: errors.New("remove remote failed")}

	err := RemoveHandler{Options: Options{WorkDir: repo, ConfigRoot: repo}}.remove(context.Background(), git, cli.RemoveOptions{LocalPath: "vendor/basic"})
	if err == nil || !strings.Contains(err.Error(), `remove committed but failed to remove Braid remote "main_braid_vendor_basic"`) || !strings.Contains(err.Error(), "remove remote failed") {
		t.Fatalf("remove error = %v, want post-commit remote cleanup failure", err)
	}
	if !git.committed {
		t.Fatal("CommitTreeWithTemporaryIndex was not called before remote cleanup failure")
	}
}

func writeRemoveMirrorConfig(t *testing.T, repo string) {
	t.Helper()
	data := []byte(`{
  "config_version": 1,
  "mirrors": {
    "vendor/basic": {
      "url": "https://example.invalid/upstream.git",
      "branch": "main",
      "revision": "abcdef1234567890"
    }
  }
}
`)
	if err := os.WriteFile(filepath.Join(repo, config.FileName), data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func assertPathMissing(t *testing.T, repo, relativePath string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(repo, filepath.FromSlash(relativePath)))
	if !os.IsNotExist(err) {
		t.Fatalf("%s exists after remove, stat err = %v", relativePath, err)
	}
}

func assertMirrorMissing(t *testing.T, repo, localPath string) {
	t.Helper()
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if _, ok := cfg.Get(localPath); ok {
		t.Fatalf("mirror %q still exists in config", localPath)
	}
}

type fakeRemoveGit struct {
	remoteExists    bool
	remoteURLErr    error
	remoteRemoveErr error
	restoreErr      error
	committed       bool
}

func (f *fakeRemoveGit) RequireVersion(context.Context, string) error { return nil }
func (f *fakeRemoveGit) IsInsideWorkTree(context.Context) (bool, error) {
	return true, nil
}
func (f *fakeRemoveGit) RelativeWorkingDir(context.Context) (string, error) { return "", nil }
func (f *fakeRemoveGit) RemoteURL(context.Context, string) (string, bool, error) {
	return "https://example.invalid/upstream.git", f.remoteExists, f.remoteURLErr
}
func (f *fakeRemoveGit) RemoteAdd(context.Context, string, string) error { return nil }
func (f *fakeRemoveGit) RemoteRemove(context.Context, string) error {
	return f.remoteRemoveErr
}
func (f *fakeRemoveGit) StatusPorcelainPathspecs(context.Context, ...string) (string, error) {
	return "", nil
}
func (f *fakeRemoveGit) BlockingOperation(context.Context) (string, bool, error) {
	return "", false, nil
}
func (f *fakeRemoveGit) HashBytes(context.Context, []byte) (gitexec.TreeItem, error) {
	return gitexec.TreeItem{Mode: "100644", Type: "blob", Hash: "config"}, nil
}
func (f *fakeRemoveGit) MakeTreeWithoutPath(context.Context, string, string) (string, error) {
	return "without-mirror", nil
}
func (f *fakeRemoveGit) MakeTreeWithItemIn(context.Context, string, string, gitexec.TreeItem) (string, error) {
	return "final-tree", nil
}
func (f *fakeRemoveGit) CommitTreeWithTemporaryIndex(context.Context, string, string) (bool, error) {
	f.committed = true
	return true, nil
}
func (f *fakeRemoveGit) RestorePathspecsFromHead(context.Context, ...string) error {
	return f.restoreErr
}

var _ RemoveGit = (*fakeRemoveGit)(nil)
