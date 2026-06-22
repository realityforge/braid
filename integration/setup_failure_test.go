package integration

import (
	"os"
	"path/filepath"
	"testing"
)

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

	subdirRepo := filepath.Join(root, "subdir")
	initRepo(t, env, subdirRepo)
	writeFile(t, subdirRepo, "README.md", "downstream\n")
	commitAll(t, env, subdirRepo, "seed downstream")
	subdir := filepath.Join(subdirRepo, "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}
	subdirStatus := runBraid(t, env, subdir, braid, "status")
	assertExit(t, subdirStatus, 1)
	assertEmpty(t, "subdir missing config stdout", subdirStatus.stdout)
	assertContains(t, subdirStatus.stderr, "missing .braids.json")

	outsideDir := filepath.Join(root, "outside-worktree")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	outsideWorktree := runBraid(t, env, outsideDir, braid, "status")
	assertExit(t, outsideWorktree, 1)
	assertEmpty(t, "outside worktree stdout", outsideWorktree.stdout)
	assertContains(t, outsideWorktree.stderr, "inside a git working tree")

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
