package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableSubdirectoryLifecycle(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "base\n")
	baseRevision := commitAll(t, env, upstream, "seed upstream")
	gitOK(t, env, upstream, "config", "--local", "receive.denyCurrentBranch", "updateInstead")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	workDir := filepath.Join(downstream, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	localPath := "apps/web/vendor/basic"
	remote := remoteName("main", "upstream")
	add := runBraid(t, env, workDir, braid, "--quiet", "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")
	assertFile(t, downstream, "apps/web/vendor/basic/README.md", "base\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		localPath: {URL: upstream, Branch: "main", Revision: baseRevision},
	})

	status := runBraid(t, env, workDir, braid, "--quiet", "status", "vendor/basic")
	assertExit(t, status, 0)
	assertEmpty(t, "subdir status stderr", status.stderr)
	assertContains(t, status.stdout, localPath+" ("+baseRevision+") [BRANCH=main]")
	assertNoRemote(t, env, downstream, remote)

	writeFile(t, downstream, "apps/web/vendor/basic/README.md", "local\n")
	diff := runBraid(t, env, workDir, braid, "--quiet", "diff", "vendor/basic")
	assertExit(t, diff, 0)
	assertEmpty(t, "subdir diff stderr", diff.stderr)
	assertContains(t, diff.stdout, "diff --git a/README.md b/README.md")
	assertContains(t, diff.stdout, "local")

	commitAll(t, env, downstream, "local mirror change")
	pushEnv := env.with("GIT_EDITOR", editorCommand(t, root, "Subdir push"))
	push := runBraid(t, pushEnv, workDir, braid, "--quiet", "push", "vendor/basic")
	assertExit(t, push, 0)
	assertEmpty(t, "subdir push stderr", push.stderr)
	assertFile(t, upstream, "README.md", "local\n")
	pushedRevision := gitOutput(t, env, upstream, "rev-parse", "HEAD")
	assertNoRemote(t, env, downstream, remote)

	update := runBraid(t, env, workDir, braid, "--quiet", "update", "vendor/basic")
	assertResult(t, update, 0, "", "")
	assertConfigRaw(t, downstream, map[string]configMirror{
		localPath: {URL: upstream, Branch: "main", Revision: pushedRevision},
	})

	remove := runBraid(t, env, workDir, braid, "remove", "vendor/basic")
	assertResult(t, remove, 0, "", "")
	assertPathMissing(t, downstream, localPath)
	assertConfigRaw(t, downstream, map[string]configMirror{})
	assertNoRemote(t, env, downstream, remote)
	assertClean(t, env, downstream)
}

func TestExecutableNoPathCommandsFromSubdirectoryRemainRepositoryWide(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstreamA := filepath.Join(root, "upstream-a")
	initRepo(t, env, upstreamA)
	writeFile(t, upstreamA, "README.md", "a\n")
	commitAll(t, env, upstreamA, "seed a")
	upstreamB := filepath.Join(root, "upstream-b")
	initRepo(t, env, upstreamB)
	writeFile(t, upstreamB, "README.md", "b\n")
	commitAll(t, env, upstreamB, "seed b")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	assertResult(t, runBraid(t, env, downstream, braid, "--quiet", "add", upstreamA, "vendor/a"), 0, "", "")
	assertResult(t, runBraid(t, env, downstream, braid, "--quiet", "add", upstreamB, "third_party/b"), 0, "", "")
	writeFile(t, downstream, "vendor/a/README.md", "a local\n")
	writeFile(t, downstream, "third_party/b/README.md", "b local\n")
	workDir := filepath.Join(downstream, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	status := runBraid(t, env, workDir, braid, "--quiet", "status")
	assertExit(t, status, 0)
	assertEmpty(t, "no-path status stderr", status.stderr)
	assertContains(t, status.stdout, "vendor/a (")
	assertContains(t, status.stdout, "third_party/b (")

	diff := runBraid(t, env, workDir, braid, "--quiet", "diff")
	assertExit(t, diff, 0)
	assertEmpty(t, "no-path diff stderr", diff.stderr)
	assertContains(t, diff.stdout, "Braid: Diffing mirror vendor/a")
	assertContains(t, diff.stdout, "Braid: Diffing mirror third_party/b")
	assertContains(t, diff.stdout, "a local")
	assertContains(t, diff.stdout, "b local")
}

func TestExecutableSubdirectoryAbsolutePathInputs(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstreamAbs := filepath.Join(root, "upstream-absolute")
	initRepo(t, env, upstreamAbs)
	writeFile(t, upstreamAbs, "README.md", "absolute\n")
	absRevision := commitAll(t, env, upstreamAbs, "seed absolute")
	upstreamSymlink := filepath.Join(root, "upstream-symlink")
	initRepo(t, env, upstreamSymlink)
	writeFile(t, upstreamSymlink, "README.md", "symlink\n")
	symlinkRevision := commitAll(t, env, upstreamSymlink, "seed symlink")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	workDir := filepath.Join(downstream, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	absoluteTarget := filepath.Join(downstream, "vendor", "absolute")
	addAbs := runBraid(t, env, workDir, braid, "--quiet", "add", upstreamAbs, absoluteTarget)
	assertResult(t, addAbs, 0, "", "")
	assertFile(t, downstream, "vendor/absolute/README.md", "absolute\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/absolute": {URL: upstreamAbs, Branch: "main", Revision: absRevision},
	})

	outside := runBraid(t, env, workDir, braid, "status", filepath.Join(root, "outside"))
	assertExit(t, outside, 1)
	assertEmpty(t, "outside absolute stdout", outside.stdout)
	assertContains(t, outside.stderr, "outside the git worktree")

	symlinkRoot := filepath.Join(root, "downstream-link")
	if err := os.Symlink(downstream, symlinkRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	symlinkWorkDir := filepath.Join(symlinkRoot, "apps", "web")
	symlinkTarget := filepath.Join(symlinkWorkDir, "vendor", "symlinked")
	addSymlink := runBraid(t, env.with("PWD", symlinkWorkDir), symlinkWorkDir, braid, "--quiet", "add", upstreamSymlink, symlinkTarget)
	assertResult(t, addSymlink, 0, "", "")
	assertFile(t, downstream, "apps/web/vendor/symlinked/README.md", "symlink\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"apps/web/vendor/symlinked": {URL: upstreamSymlink, Branch: "main", Revision: symlinkRevision},
		"vendor/absolute":           {URL: upstreamAbs, Branch: "main", Revision: absRevision},
	})
}
