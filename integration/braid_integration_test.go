package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	defaultName  = "Braid Integration"
	defaultEmail = "braid-integration@example.invalid"
)

func expectedBraidVersion() string {
	if version := os.Getenv("BRAID_EXPECTED_VERSION"); version != "" {
		return version
	}
	return "0.0.0-dev"
}

func TestExecutablePrimaryLifecycle(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream repo")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "lib dir/component.txt", "base\n")
	writeFile(t, upstream, "lib dir/kept.txt", "kept\n")
	baseRevision := commitAll(t, env, upstream, "seed upstream")
	gitOK(t, env, upstream, "config", "--local", "receive.denyCurrentBranch", "updateInstead")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	writeFailingPreCommitHook(t, downstream)

	version := runBraid(t, env, root, braid, "version")
	assertResult(t, version, 0, "braid "+expectedBraidVersion()+"\n", "")

	localPath := "vendor/lib with spaces"
	remote := remoteName("main", localPath)
	cacheRemoteURL := cachePath(env.braidCacheDir(), upstream)

	add := runBraid(t, env, downstream, braid, "add", upstream, localPath, "--path", "lib dir")
	assertResult(t, add, 0, "", "")
	assertFile(t, downstream, "vendor/lib with spaces/component.txt", "base\n")
	assertFile(t, downstream, "vendor/lib with spaces/kept.txt", "kept\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		localPath: {URL: upstream, Branch: "main", Path: "lib dir", Revision: baseRevision},
	})
	assertLatestCommit(t, env, downstream, defaultName+" <"+defaultEmail+">", "Braid: Add mirror '"+localPath+"' at '"+shortRevision(baseRevision)+"'")
	assertNoRemote(t, env, downstream, remote)
	assertPathExists(t, filepath.Join(cacheRemoteURL, "HEAD"))
	assertClean(t, env, downstream)

	setup := runBraid(t, env, downstream, braid, "setup", localPath)
	assertResult(t, setup, 0, "", "")
	assertRemoteURL(t, env, downstream, remote, cacheRemoteURL)

	gitOK(t, env, downstream, "remote", "set-url", remote, "manual-url")
	setupReuse := runBraid(t, env, downstream, braid, "setup", localPath)
	assertResult(t, setupReuse, 0, "", "")
	assertRemoteURL(t, env, downstream, remote, "manual-url")

	setupForce := runBraid(t, env, downstream, braid, "setup", localPath, "--force")
	assertResult(t, setupForce, 0, "", "")
	assertRemoteURL(t, env, downstream, remote, cacheRemoteURL)

	status := runBraid(t, env, downstream, braid, "status", localPath)
	assertExit(t, status, 0)
	assertEmpty(t, "status stderr", status.stderr)
	assertContains(t, status.stdout, localPath+" ("+baseRevision+") [BRANCH=main]")
	assertNoRemote(t, env, downstream, remote)

	writeFile(t, downstream, "vendor/lib with spaces/component.txt", "local\n")
	diff := runBraid(t, env, downstream, braid, "diff", localPath)
	assertExit(t, diff, 0)
	assertEmpty(t, "diff stderr", diff.stderr)
	assertContains(t, diff.stdout, "diff --git a/component.txt b/component.txt")
	assertContains(t, diff.stdout, "local")
	assertNoRemote(t, env, downstream, remote)

	commitAll(t, env, downstream, "local mirror change")
	pushEnv := env.with("GIT_EDITOR", editorCommand(t, root, "Executable push"))
	push := runBraid(t, pushEnv, downstream, braid, "push", localPath)
	assertExit(t, push, 0)
	assertEmpty(t, "push stderr", push.stderr)
	assertContains(t, push.stdout, "Executable push")
	assertFile(t, upstream, "lib dir/component.txt", "local\n")
	pushedRevision := gitOutput(t, env, upstream, "rev-parse", "HEAD")
	assertLatestCommit(t, env, upstream, defaultName+" <"+defaultEmail+">", "Executable push")
	assertConfigRaw(t, downstream, map[string]configMirror{
		localPath: {URL: upstream, Branch: "main", Path: "lib dir", Revision: baseRevision},
	})
	assertNoRemote(t, env, downstream, remote)
	assertClean(t, env, downstream)

	update := runBraid(t, env, downstream, braid, "update", localPath)
	assertResult(t, update, 0, "", "")
	assertFile(t, downstream, "vendor/lib with spaces/component.txt", "local\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		localPath: {URL: upstream, Branch: "main", Path: "lib dir", Revision: pushedRevision},
	})
	assertLatestCommit(t, env, downstream, defaultName+" <"+defaultEmail+">", "Braid: Update mirror '"+localPath+"' to '"+shortRevision(pushedRevision)+"'")
	assertNoRemote(t, env, downstream, remote)
	assertClean(t, env, downstream)

	remove := runBraid(t, env, downstream, braid, "remove", localPath)
	assertResult(t, remove, 0, "", "")
	assertPathMissing(t, downstream, localPath)
	assertConfigRaw(t, downstream, map[string]configMirror{})
	assertLatestCommit(t, env, downstream, defaultName+" <"+defaultEmail+">", "Braid: Remove mirror '"+localPath+"'")
	assertNoRemote(t, env, downstream, remote)
	assertClean(t, env, downstream)
}

func TestExecutableSetupCacheModes(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "cache modes\n")
	revision := commitAll(t, env, upstream, "seed upstream")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	writeConfig(t, downstream, map[string]configMirror{
		"vendor/repo": {URL: upstream, Branch: "main", Revision: revision},
	})
	commitAll(t, env, downstream, "configure mirror")

	remote := remoteName("main", "vendor/repo")

	defaultEnv := env.without("BRAID_LOCAL_CACHE_DIR")
	setupDefault := runBraid(t, defaultEnv, downstream, braid, "setup", "vendor/repo")
	assertResult(t, setupDefault, 0, "", "")
	defaultCacheURL := cachePath(defaultEnv.defaultBraidCacheDir(), upstream)
	assertRemoteURL(t, defaultEnv, downstream, remote, defaultCacheURL)
	statusDefault := runBraid(t, defaultEnv, downstream, braid, "status", "vendor/repo")
	assertExit(t, statusDefault, 0)
	assertEmpty(t, "default cache status stderr", statusDefault.stderr)
	assertContains(t, statusDefault.stdout, "(Removed Locally)")
	assertPathExists(t, filepath.Join(defaultCacheURL, "HEAD"))
	assertNoRemote(t, defaultEnv, downstream, remote)

	envDisabled := defaultEnv.with("BRAID_USE_LOCAL_CACHE", "false")
	setupDisabled := runBraid(t, envDisabled, downstream, braid, "setup", "vendor/repo")
	assertResult(t, setupDisabled, 0, "", "")
	assertRemoteURL(t, envDisabled, downstream, remote, upstream)

	gitOK(t, envDisabled, downstream, "remote", "rm", remote)
	setupCacheDir := runBraid(t, envDisabled, downstream, braid, "--cache-dir", "explicit-cache", "setup", "vendor/repo")
	assertResult(t, setupCacheDir, 0, "", "")
	assertRemoteURL(t, envDisabled, downstream, remote, cachePath(filepath.Join(processWorkingDir(t, downstream), "explicit-cache"), upstream))

	gitOK(t, envDisabled, downstream, "remote", "rm", remote)
	setupNoCache := runBraid(t, env, downstream, braid, "--no-cache", "setup", "vendor/repo")
	assertResult(t, setupNoCache, 0, "", "")
	assertRemoteURL(t, env, downstream, remote, upstream)

	invalid := runBraid(t, env, downstream, braid, "--no-cache", "--cache-dir", "cache", "setup")
	assertExit(t, invalid, 2)
	assertEmpty(t, "invalid cache stdout", invalid.stdout)
	assertContains(t, invalid.stderr, "--no-cache and --cache-dir cannot be used together")
}

func TestExecutableFailurePaths(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "base\n")
	commitAll(t, env, upstream, "seed upstream")

	missingConfigRepo := filepath.Join(root, "missing-config")
	initRepo(t, env, missingConfigRepo)
	writeFile(t, missingConfigRepo, "README.md", "downstream\n")
	commitAll(t, env, missingConfigRepo, "seed downstream")
	missingConfig := runBraid(t, env, missingConfigRepo, braid, "status")
	assertExit(t, missingConfig, 1)
	assertEmpty(t, "missing config stdout", missingConfig.stdout)
	assertContains(t, missingConfig.stderr, "missing .braids.json")

	legacyRepo := filepath.Join(root, "legacy-config")
	initRepo(t, env, legacyRepo)
	writeFile(t, legacyRepo, "README.md", "downstream\n")
	commitAll(t, env, legacyRepo, "seed downstream")
	writeFile(t, legacyRepo, ".braids", "legacy: true\n")
	legacy := runBraid(t, env, legacyRepo, braid, "add", upstream, "vendor/basic")
	assertExit(t, legacy, 1)
	assertEmpty(t, "legacy stdout", legacy.stdout)
	assertContains(t, legacy.stderr, "legacy .braids config is unsupported")
	assertPathMissing(t, legacyRepo, ".braids.json")

	subdirRepo := filepath.Join(root, "subdir")
	initRepo(t, env, subdirRepo)
	writeFile(t, subdirRepo, "README.md", "downstream\n")
	commitAll(t, env, subdirRepo, "seed downstream")
	subdir := filepath.Join(subdirRepo, "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}
	wrongDir := runBraid(t, env, subdir, braid, "status")
	assertExit(t, wrongDir, 1)
	assertEmpty(t, "wrong dir stdout", wrongDir.stdout)
	assertContains(t, wrongDir.stderr, "working tree root")

	rollbackRepo := filepath.Join(root, "rollback")
	initRepo(t, env, rollbackRepo)
	writeFile(t, rollbackRepo, "README.md", "downstream\n")
	commitAll(t, env, rollbackRepo, "seed downstream")
	head := gitOutput(t, env, rollbackRepo, "rev-parse", "HEAD")
	failedAdd := runBraid(t, env, rollbackRepo, braid, "add", upstream, "vendor/missing", "--path", "does-not-exist")
	assertExit(t, failedAdd, 1)
	assertEmpty(t, "failed add stdout", failedAdd.stdout)
	assertContains(t, failedAdd.stderr, "no tree item exists")
	if got := gitOutput(t, env, rollbackRepo, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD after failed add = %s, want %s", got, head)
	}
	assertPathMissing(t, rollbackRepo, ".braids.json")
	assertNoRemote(t, env, rollbackRepo, remoteName("main", "vendor/missing"))
	assertClean(t, env, rollbackRepo)
}

func TestExecutableScopedAddPreservesUnrelatedState(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "base\n")
	revision := commitAll(t, env, upstream, "base")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	writeFile(t, downstream, "tracked.txt", "tracked base\n")
	gitOK(t, env, downstream, "add", "tracked.txt")
	gitOK(t, env, downstream, "commit", "-m", "add tracked file")

	writeFile(t, downstream, "staged.txt", "staged content\n")
	gitOK(t, env, downstream, "add", "staged.txt")
	writeFile(t, downstream, "tracked.txt", "tracked dirty\n")
	writeFile(t, downstream, "untracked.txt", "untracked content\n")

	add := runBraid(t, env, downstream, braid, "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")
	assertFile(t, downstream, "vendor/basic/README.md", "base\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: revision},
	})
	changed := strings.Fields(gitOK(t, env, downstream, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").stdout)
	if strings.Join(changed, "\n") != ".braids.json\nvendor/basic/README.md" {
		t.Fatalf("Braid commit changed %#v, want config and mirror only", changed)
	}
	if got := strings.TrimSpace(gitOK(t, env, downstream, "show", ":staged.txt").stdout); got != "staged content" {
		t.Fatalf("staged blob = %q, want staged content", got)
	}
	assertFile(t, downstream, "tracked.txt", "tracked dirty\n")
	assertFile(t, downstream, "untracked.txt", "untracked content\n")
	status := gitOK(t, env, downstream, "status", "--porcelain").stdout
	assertContains(t, status, "A  staged.txt")
	assertContains(t, status, " M tracked.txt")
	assertContains(t, status, "?? untracked.txt")
	assertNoRemote(t, env, downstream, remoteName("main", "vendor/basic"))
}

func TestExecutableScopedRemovePreservesUnrelatedState(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "base\n")
	commitAll(t, env, upstream, "base")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	add := runBraid(t, env, downstream, braid, "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")
	writeFile(t, downstream, "tracked.txt", "tracked base\n")
	gitOK(t, env, downstream, "add", "tracked.txt")
	gitOK(t, env, downstream, "commit", "-m", "add tracked file")

	writeFile(t, downstream, "staged.txt", "staged content\n")
	gitOK(t, env, downstream, "add", "staged.txt")
	writeFile(t, downstream, "tracked.txt", "tracked dirty\n")
	writeFile(t, downstream, "untracked.txt", "untracked content\n")

	remove := runBraid(t, env, downstream, braid, "remove", "vendor/basic")
	assertResult(t, remove, 0, "", "")
	assertPathMissing(t, downstream, "vendor/basic")
	assertConfigRaw(t, downstream, map[string]configMirror{})
	changed := strings.Fields(gitOK(t, env, downstream, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").stdout)
	if strings.Join(changed, "\n") != ".braids.json\nvendor/basic/README.md" {
		t.Fatalf("Braid commit changed %#v, want config and mirror only", changed)
	}
	if got := strings.TrimSpace(gitOK(t, env, downstream, "show", ":staged.txt").stdout); got != "staged content" {
		t.Fatalf("staged blob = %q, want staged content", got)
	}
	assertFile(t, downstream, "tracked.txt", "tracked dirty\n")
	assertFile(t, downstream, "untracked.txt", "untracked content\n")
	status := gitOK(t, env, downstream, "status", "--porcelain").stdout
	assertContains(t, status, "A  staged.txt")
	assertContains(t, status, " M tracked.txt")
	assertContains(t, status, "?? untracked.txt")
}

func TestExecutableScopedAddRemovePrecheckBlockers(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, env processEnv, braid, upstream, downstream string)
		dirty   func(t *testing.T, downstream string)
		args    []string
		wantErr string
	}{
		{
			name: "add dirty config",
			setup: func(t *testing.T, env processEnv, braid, upstream, downstream string) {
				t.Helper()
				writeConfig(t, downstream, map[string]configMirror{})
				gitOK(t, env, downstream, "add", ".braids.json")
				gitOK(t, env, downstream, "commit", "-m", "add empty braid config")
			},
			dirty: func(t *testing.T, downstream string) {
				t.Helper()
				writeFile(t, downstream, ".braids.json", "{\"config_version\":1,\"mirrors\":{}}\n")
			},
			args:    []string{"add", "$upstream", "vendor/basic"},
			wantErr: "local changes are present in .braids.json",
		},
		{
			name: "add existing target",
			setup: func(t *testing.T, env processEnv, braid, upstream, downstream string) {
				t.Helper()
				writeFile(t, downstream, "vendor/basic/README.md", "existing\n")
				gitOK(t, env, downstream, "add", "vendor/basic/README.md")
				gitOK(t, env, downstream, "commit", "-m", "tracked target")
			},
			args:    []string{"add", "$upstream", "vendor/basic"},
			wantErr: `add target path "vendor/basic" already exists in git index`,
		},
		{
			name: "remove dirty mirror",
			setup: func(t *testing.T, env processEnv, braid, upstream, downstream string) {
				t.Helper()
				add := runBraid(t, env, downstream, braid, "add", upstream, "vendor/basic")
				assertResult(t, add, 0, "", "")
			},
			dirty: func(t *testing.T, downstream string) {
				t.Helper()
				writeFile(t, downstream, "vendor/basic/README.md", "dirty\n")
			},
			args:    []string{"remove", "vendor/basic"},
			wantErr: "local changes are present in vendor/basic",
		},
		{
			name: "remove untracked mirror content",
			setup: func(t *testing.T, env processEnv, braid, upstream, downstream string) {
				t.Helper()
				add := runBraid(t, env, downstream, braid, "add", upstream, "vendor/basic")
				assertResult(t, add, 0, "", "")
			},
			dirty: func(t *testing.T, downstream string) {
				t.Helper()
				writeFile(t, downstream, "vendor/basic/untracked.txt", "untracked\n")
			},
			args:    []string{"remove", "vendor/basic"},
			wantErr: "local changes are present in vendor/basic",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			env := newProcessEnv(t, root)
			braid := braidBinary(t)
			upstream := filepath.Join(root, "upstream")
			initRepo(t, env, upstream)
			writeFile(t, upstream, "README.md", "base\n")
			commitAll(t, env, upstream, "base")
			downstream := filepath.Join(root, "downstream")
			initRepo(t, env, downstream)
			writeFile(t, downstream, "README.md", "downstream\n")
			commitAll(t, env, downstream, "seed downstream")
			if test.setup != nil {
				test.setup(t, env, braid, upstream, downstream)
			}
			writeFile(t, downstream, "tracked.txt", "tracked base\n")
			gitOK(t, env, downstream, "add", "tracked.txt")
			gitOK(t, env, downstream, "commit", "-m", "add tracked file")
			writeFile(t, downstream, "staged.txt", "staged content\n")
			gitOK(t, env, downstream, "add", "staged.txt")
			writeFile(t, downstream, "tracked.txt", "tracked dirty\n")
			writeFile(t, downstream, "untracked.txt", "untracked content\n")
			if test.dirty != nil {
				test.dirty(t, downstream)
			}

			args := append([]string(nil), test.args...)
			for i, arg := range args {
				if arg == "$upstream" {
					args[i] = upstream
				}
			}
			result := runBraid(t, env, downstream, braid, args...)
			assertExit(t, result, 1)
			assertEmpty(t, test.name+" stdout", result.stdout)
			assertContains(t, result.stderr, test.wantErr)
			if got := strings.TrimSpace(gitOK(t, env, downstream, "show", ":staged.txt").stdout); got != "staged content" {
				t.Fatalf("staged blob = %q, want staged content", got)
			}
			assertFile(t, downstream, "tracked.txt", "tracked dirty\n")
			assertFile(t, downstream, "untracked.txt", "untracked content\n")
		})
	}
}

func TestExecutableScopedUpdatePreservesUnrelatedState(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "base\n")
	commitAll(t, env, upstream, "base")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	add := runBraid(t, env, downstream, braid, "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")
	writeFile(t, downstream, "tracked.txt", "tracked base\n")
	gitOK(t, env, downstream, "add", "tracked.txt")
	gitOK(t, env, downstream, "commit", "-m", "add tracked file")

	writeFile(t, downstream, "staged.txt", "staged content\n")
	gitOK(t, env, downstream, "add", "staged.txt")
	writeFile(t, downstream, "tracked.txt", "tracked dirty\n")
	writeFile(t, downstream, "untracked.txt", "untracked content\n")
	writeFile(t, upstream, "README.md", "updated\n")
	remoteRevision := commitAll(t, env, upstream, "updated")

	update := runBraid(t, env, downstream, braid, "update", "vendor/basic")
	assertResult(t, update, 0, "", "")
	assertFile(t, downstream, "vendor/basic/README.md", "updated\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: remoteRevision},
	})
	changed := strings.Fields(gitOK(t, env, downstream, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").stdout)
	if strings.Join(changed, "\n") != ".braids.json\nvendor/basic/README.md" {
		t.Fatalf("Braid commit changed %#v, want config and mirror only", changed)
	}
	if got := strings.TrimSpace(gitOK(t, env, downstream, "show", ":staged.txt").stdout); got != "staged content" {
		t.Fatalf("staged blob = %q, want staged content", got)
	}
	assertFile(t, downstream, "tracked.txt", "tracked dirty\n")
	assertFile(t, downstream, "untracked.txt", "untracked content\n")
	status := gitOK(t, env, downstream, "status", "--porcelain").stdout
	assertContains(t, status, "A  staged.txt")
	assertContains(t, status, " M tracked.txt")
	assertContains(t, status, "?? untracked.txt")
}

func TestExecutableUpdateConflictWritesMergeMessage(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "base\n")
	baseRevision := commitAll(t, env, upstream, "base")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")

	add := runBraid(t, env, downstream, braid, "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: baseRevision},
	})

	writeFile(t, downstream, "vendor/basic/README.md", "local\n")
	commitAll(t, env, downstream, "local mirror change")
	writeFile(t, downstream, "staged.txt", "staged content\n")
	gitOK(t, env, downstream, "add", "staged.txt")
	writeFile(t, upstream, "README.md", "remote\n")
	remoteRevision := commitAll(t, env, upstream, "remote change")

	update := runBraid(t, env, downstream, braid, "update", "vendor/basic")
	assertExit(t, update, 0)
	assertEmpty(t, "conflict update stderr", update.stderr)
	assertContains(t, update.stdout, "CONFLICT")
	assertContains(t, update.stdout, "Braid: warning: unrelated staged changes are present")
	assertContains(t, update.stdout, "git commit -F .git/MERGE_MSG")
	conflicted := readFile(t, downstream, "vendor/basic/README.md")
	assertContains(t, conflicted, "<<<<<<<")
	assertContains(t, conflicted, "local")
	assertContains(t, conflicted, "remote")
	assertContains(t, readFile(t, downstream, ".git/MERGE_MSG"), "Braid: Update mirror 'vendor/basic' to '"+shortRevision(remoteRevision)+"'")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: remoteRevision},
	})
	if unmerged := strings.TrimSpace(gitOK(t, env, downstream, "ls-files", "-u").stdout); unmerged != "" {
		t.Fatalf("unmerged entries = %q, want marker fallback without unmerged entries", unmerged)
	}
	if cached := strings.Fields(gitOK(t, env, downstream, "diff", "--cached", "--name-only").stdout); strings.Join(cached, "\n") != ".braids.json\nstaged.txt" {
		t.Fatalf("cached names = %#v, want config and staged unrelated file", cached)
	}
	if got := strings.TrimSpace(gitOK(t, env, downstream, "show", ":staged.txt").stdout); got != "staged content" {
		t.Fatalf("staged blob = %q, want staged content", got)
	}
	assertContains(t, gitOK(t, env, downstream, "status", "--porcelain").stdout, "README.md")
}

func TestExecutableUpdateAllScopedPrecheckStopsBeforeUpdates(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstreamA := filepath.Join(root, "upstream-a")
	initRepo(t, env, upstreamA)
	writeFile(t, upstreamA, "README.md", "a base\n")
	aBase := commitAll(t, env, upstreamA, "a base")
	upstreamB := filepath.Join(root, "upstream-b")
	initRepo(t, env, upstreamB)
	writeFile(t, upstreamB, "README.md", "b base\n")
	bBase := commitAll(t, env, upstreamB, "b base")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	assertResult(t, runBraid(t, env, downstream, braid, "add", upstreamA, "vendor/a"), 0, "", "")
	assertResult(t, runBraid(t, env, downstream, braid, "add", upstreamB, "vendor/b"), 0, "", "")
	writeFile(t, upstreamA, "README.md", "a updated\n")
	commitAll(t, env, upstreamA, "a updated")
	writeFile(t, upstreamB, "README.md", "b updated\n")
	commitAll(t, env, upstreamB, "b updated")
	writeFile(t, downstream, "vendor/b/README.md", "dirty b\n")

	update := runBraid(t, env, downstream, braid, "--no-cache", "update")
	assertExit(t, update, 1)
	assertEmpty(t, "update-all precheck stdout", update.stdout)
	assertContains(t, update.stderr, "local changes are present in vendor/b")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/a": {URL: upstreamA, Branch: "main", Revision: aBase},
		"vendor/b": {URL: upstreamB, Branch: "main", Revision: bBase},
	})
	assertNoRemote(t, env, downstream, remoteName("main", "vendor/a"))
	assertNoRemote(t, env, downstream, remoteName("main", "vendor/b"))
}

type processEnv struct {
	values map[string]string
}

func newProcessEnv(t *testing.T, root string) processEnv {
	t.Helper()
	dirs := []string{
		filepath.Join(root, "home"),
		filepath.Join(root, "userprofile"),
		filepath.Join(root, "xdg-config"),
		filepath.Join(root, "xdg-cache"),
		filepath.Join(root, "appdata"),
		filepath.Join(root, "local-appdata"),
		filepath.Join(root, "tmp"),
		filepath.Join(root, "braid-cache"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create env dir %s: %v", dir, err)
		}
	}
	globalConfig := filepath.Join(root, "xdg-config", "gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0o644); err != nil {
		t.Fatalf("write global gitconfig: %v", err)
	}

	values := map[string]string{
		"APPDATA":               filepath.Join(root, "appdata"),
		"BRAID_LOCAL_CACHE_DIR": filepath.Join(root, "braid-cache"),
		"GIT_AUTHOR_DATE":       "2001-02-03T04:05:06Z",
		"GIT_COMMITTER_DATE":    "2001-02-03T04:05:06Z",
		"GIT_CONFIG_GLOBAL":     globalConfig,
		"GIT_CONFIG_NOSYSTEM":   "1",
		"GIT_TERMINAL_PROMPT":   "0",
		"HOME":                  filepath.Join(root, "home"),
		"LANG":                  "C",
		"LC_ALL":                "C",
		"LOCALAPPDATA":          filepath.Join(root, "local-appdata"),
		"TEMP":                  filepath.Join(root, "tmp"),
		"TMP":                   filepath.Join(root, "tmp"),
		"TMPDIR":                filepath.Join(root, "tmp"),
		"USERPROFILE":           filepath.Join(root, "userprofile"),
		"XDG_CACHE_HOME":        filepath.Join(root, "xdg-cache"),
		"XDG_CONFIG_HOME":       filepath.Join(root, "xdg-config"),
	}
	if runtime.GOOS == "windows" {
		if value, ok := os.LookupEnv("Path"); ok {
			values["Path"] = value
		} else if value, ok := os.LookupEnv("PATH"); ok {
			values["Path"] = value
		}
		for _, key := range []string{"COMSPEC", "PATHEXT", "SystemRoot", "WINDIR"} {
			if value, ok := os.LookupEnv(key); ok {
				values[key] = value
			}
		}
	} else if value, ok := os.LookupEnv("PATH"); ok {
		values["PATH"] = value
	}
	return processEnv{values: values}
}

func (e processEnv) list() []string {
	keys := make([]string, 0, len(e.values))
	for key := range e.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+e.values[key])
	}
	return env
}

func (e processEnv) with(key, value string) processEnv {
	values := make(map[string]string, len(e.values)+1)
	for existingKey, existingValue := range e.values {
		values[existingKey] = existingValue
	}
	values[key] = value
	return processEnv{values: values}
}

func (e processEnv) without(keys ...string) processEnv {
	values := make(map[string]string, len(e.values))
	for existingKey, existingValue := range e.values {
		values[existingKey] = existingValue
	}
	for _, key := range keys {
		delete(values, key)
	}
	return processEnv{values: values}
}

func (e processEnv) braidCacheDir() string {
	return e.values["BRAID_LOCAL_CACHE_DIR"]
}

func (e processEnv) defaultBraidCacheDir() string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(e.values["HOME"], "Library", "Caches", "braid")
	case "windows":
		return filepath.Join(e.values["LOCALAPPDATA"], "braid")
	default:
		return filepath.Join(e.values["XDG_CACHE_HOME"], "braid")
	}
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runBraid(t *testing.T, env processEnv, workdir, braid string, args ...string) commandResult {
	t.Helper()
	return runProcess(t, env, workdir, braid, args...)
}

func gitOK(t *testing.T, env processEnv, workdir string, args ...string) commandResult {
	t.Helper()
	result := runProcess(t, env, workdir, "git", args...)
	if result.exitCode != 0 {
		t.Fatalf("git %v failed in %s with exit %d\nstdout:\n%s\nstderr:\n%s", args, workdir, result.exitCode, result.stdout, result.stderr)
	}
	return result
}

func runProcess(t *testing.T, env processEnv, workdir, executable string, args ...string) commandResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = workdir
	cmd.Env = env.list()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %v timed out in %s", executable, args, workdir)
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("%s %v failed to start in %s: %v", executable, args, workdir, err)
		}
	}
	return commandResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

func braidBinary(t *testing.T) string {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	candidates := []string{
		"_main/cmd/braid/braid_/braid" + suffix,
		"braid/cmd/braid/braid_/braid" + suffix,
		"cmd/braid/braid_/braid" + suffix,
	}
	if manifest := os.Getenv("RUNFILES_MANIFEST_FILE"); manifest != "" {
		data, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatalf("read runfiles manifest %s: %v", manifest, err)
		}
		entries := map[string]string{}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}
			logical, actual, ok := strings.Cut(line, " ")
			if ok {
				entries[logical] = actual
			}
		}
		for _, candidate := range candidates {
			if path, ok := entries[candidate]; ok {
				return path
			}
		}
	}
	for _, rootEnv := range []string{"RUNFILES_DIR", "TEST_SRCDIR"} {
		if dir := os.Getenv(rootEnv); dir != "" {
			for _, candidate := range candidates {
				path := filepath.Join(dir, filepath.FromSlash(candidate))
				if _, err := os.Stat(path); err == nil {
					return path
				}
			}
		}
	}
	t.Fatalf("could not locate //cmd/braid:braid in Bazel runfiles; checked %v", candidates)
	return ""
}

func initRepo(t *testing.T, env processEnv, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
		t.Fatalf("create repo parent: %v", err)
	}
	gitOK(t, env, filepath.Dir(repo), "init", "--initial-branch=main", repo)
	gitOK(t, env, repo, "config", "--local", "user.name", defaultName)
	gitOK(t, env, repo, "config", "--local", "user.email", defaultEmail)
	gitOK(t, env, repo, "config", "--local", "commit.gpgsign", "false")
}

func processWorkingDir(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func commitAll(t *testing.T, env processEnv, repo, message string) string {
	t.Helper()
	gitOK(t, env, repo, "add", ".")
	gitOK(t, env, repo, "commit", "--no-verify", "-m", message)
	return gitOutput(t, env, repo, "rev-parse", "HEAD")
}

func gitOutput(t *testing.T, env processEnv, repo string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(gitOK(t, env, repo, args...).stdout)
}

func writeFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", relativePath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relativePath, err)
	}
}

func readFile(t *testing.T, root, relativePath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(data)
}

func writeConfig(t *testing.T, repo string, mirrors map[string]configMirror) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, ".braids.json"), []byte(expectedConfigRaw(t, mirrors)), 0o644); err != nil {
		t.Fatalf("write .braids.json: %v", err)
	}
}

type configFile struct {
	ConfigVersion int                     `json:"config_version"`
	Mirrors       map[string]configMirror `json:"mirrors"`
}

type configMirror struct {
	URL      string `json:"url"`
	Branch   string `json:"branch,omitempty"`
	Path     string `json:"path,omitempty"`
	Tag      string `json:"tag,omitempty"`
	Revision string `json:"revision"`
}

func expectedConfigRaw(t *testing.T, mirrors map[string]configMirror) string {
	t.Helper()
	data, err := json.MarshalIndent(configFile{ConfigVersion: 1, Mirrors: mirrors}, "", "  ")
	if err != nil {
		t.Fatalf("marshal expected config: %v", err)
	}
	return string(data) + "\n"
}

func assertConfigRaw(t *testing.T, repo string, mirrors map[string]configMirror) {
	t.Helper()
	path := filepath.Join(repo, ".braids.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .braids.json: %v", err)
	}
	want := expectedConfigRaw(t, mirrors)
	if string(data) != want {
		t.Fatalf(".braids.json raw =\n%s\nwant:\n%s", string(data), want)
	}
	var parsed configFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse .braids.json: %v", err)
	}
	if parsed.ConfigVersion != 1 || len(parsed.Mirrors) != len(mirrors) {
		t.Fatalf(".braids.json semantic parse = %#v, want version 1 and %d mirrors", parsed, len(mirrors))
	}
}

func cachePath(cacheDir, url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(cacheDir, hex.EncodeToString(sum[:]))
}

func remoteName(tracking, localPath string) string {
	var b strings.Builder
	for _, r := range tracking + "_braid_" + localPath {
		if r == '-' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func editorCommand(t *testing.T, root, message string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(root, "editor.cmd")
		body := "@echo off\r\n> \"%~1\" echo " + message + "\r\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write editor script: %v", err)
		}
		return `"` + path + `"`
	}
	path := filepath.Join(root, "editor.sh")
	body := "#!/bin/sh\nprintf '%s\\n' " + shellQuote(message) + " > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write editor script: %v", err)
	}
	return shellQuote(path)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeFailingPreCommitHook(t *testing.T, repo string) {
	t.Helper()
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	body := "#!/bin/sh\nexit 1\n"
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		body = "@echo off\r\nexit /b 1\r\n"
		mode = 0o644
	}
	if err := os.WriteFile(hook, []byte(body), mode); err != nil {
		t.Fatalf("write pre-commit hook: %v", err)
	}
}

func assertResult(t *testing.T, result commandResult, exitCode int, stdout, stderr string) {
	t.Helper()
	assertExit(t, result, exitCode)
	if result.stdout != stdout || result.stderr != stderr {
		t.Fatalf("result stdout/stderr = %q / %q, want %q / %q", result.stdout, result.stderr, stdout, stderr)
	}
}

func assertExit(t *testing.T, result commandResult, want int) {
	t.Helper()
	if result.exitCode != want {
		t.Fatalf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", result.exitCode, want, result.stdout, result.stderr)
	}
}

func assertEmpty(t *testing.T, label, value string) {
	t.Helper()
	if value != "" {
		t.Fatalf("%s = %q, want empty", label, value)
	}
}

func assertContains(t *testing.T, value, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("value does not contain %q:\n%s", want, value)
	}
}

func assertFile(t *testing.T, root, relativePath, want string) {
	t.Helper()
	got := readFile(t, root, relativePath)
	if got != want {
		t.Fatalf("%s = %q, want %q", relativePath, got, want)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, root, relativePath string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(relativePath)))
	if !os.IsNotExist(err) {
		t.Fatalf("%s exists or returned unexpected stat error: %v", relativePath, err)
	}
}

func assertRemoteURL(t *testing.T, env processEnv, repo, remote, want string) {
	t.Helper()
	got, ok := remoteURL(t, env, repo, remote)
	if !ok {
		t.Fatalf("remote %q missing, want URL %q", remote, want)
	}
	if got != want {
		t.Fatalf("remote %q URL = %q, want %q", remote, got, want)
	}
}

func remoteURL(t *testing.T, env processEnv, repo, remote string) (string, bool) {
	t.Helper()
	result := runProcess(t, env, repo, "git", "config", "--get", "remote."+remote+".url")
	if result.exitCode == 1 {
		return "", false
	}
	if result.exitCode != 0 {
		t.Fatalf("git config remote %q failed with exit %d\nstdout:\n%s\nstderr:\n%s", remote, result.exitCode, result.stdout, result.stderr)
	}
	return strings.TrimSpace(result.stdout), true
}

func assertNoRemote(t *testing.T, env processEnv, repo, remote string) {
	t.Helper()
	if got, ok := remoteURL(t, env, repo, remote); ok {
		t.Fatalf("remote %q exists with URL %q, want absent", remote, got)
	}
}

func assertClean(t *testing.T, env processEnv, repo string) {
	t.Helper()
	if status := strings.TrimSpace(gitOK(t, env, repo, "status", "--porcelain").stdout); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
}

func assertLatestCommit(t *testing.T, env processEnv, repo, identity, subject string) {
	t.Helper()
	got := strings.TrimSpace(gitOK(t, env, repo, "log", "-1", "--pretty=%an <%ae>|%cn <%ce>|%s").stdout)
	want := identity + "|" + identity + "|" + subject
	if got != want {
		t.Fatalf("latest commit metadata = %q, want %q", got, want)
	}
}

func shortRevision(revision string) string {
	if len(revision) < 7 {
		return revision
	}
	return revision[:7]
}
