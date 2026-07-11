package integration

import (
	"path/filepath"
	"testing"
)

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
	cacheRemoteURL := repositoryCachePath(t, downstream, localPath, configMirror{URL: upstream, Branch: "main", Path: "lib dir"})

	add := runBraid(t, env, downstream, braid, "add", upstream, localPath, "--path", "lib dir")
	assertExit(t, add, 0)
	assertEmpty(t, "add stdout", add.stdout)
	assertProgress(t, add.stderr,
		"Braid: detecting default branch for mirror "+localPath,
		"Braid: updated cache for mirror "+localPath,
		"Braid: fetched mirror "+localPath,
	)
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
	assertExit(t, setup, 0)
	assertEmpty(t, "setup stdout", setup.stdout)
	assertProgress(t, setup.stderr,
		"Braid: updated cache for mirror "+localPath,
		"Braid: setting up mirror remote "+localPath,
		"Braid: set up mirror remote "+localPath,
	)
	assertRemoteURL(t, env, downstream, remote, cacheRemoteURL)

	gitOK(t, env, downstream, "remote", "set-url", remote, "manual-url")
	setupReuse := runBraid(t, env, downstream, braid, "setup", localPath)
	assertResult(t, setupReuse, 0, "", "")
	assertRemoteURL(t, env, downstream, remote, "manual-url")

	setupForce := runBraid(t, env, downstream, braid, "setup", localPath, "--force")
	assertExit(t, setupForce, 0)
	assertEmpty(t, "setup force stdout", setupForce.stdout)
	assertProgress(t, setupForce.stderr,
		"Braid: updated cache for mirror "+localPath,
		"Braid: setting up mirror remote "+localPath,
		"Braid: set up mirror remote "+localPath,
	)
	assertRemoteURL(t, env, downstream, remote, cacheRemoteURL)

	status := runBraid(t, env, downstream, braid, "status", localPath)
	assertExit(t, status, 0)
	assertProgress(t, status.stderr,
		"Braid: updated cache for mirror "+localPath,
		"Braid: fetched mirror "+localPath,
	)
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
	assertProgress(t, push.stderr,
		"Braid: updated cache for mirror "+localPath,
		"Braid: fetched mirror "+localPath,
		"Braid: pushing mirror "+localPath,
		"Braid: pushed mirror "+localPath,
	)
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
	assertExit(t, update, 0)
	assertEmpty(t, "update stdout", update.stdout)
	assertProgress(t, update.stderr,
		"Braid: updated cache for mirror "+localPath,
		"Braid: fetched mirror "+localPath,
		"Braid: checked mirror "+localPath,
		"Braid: updated mirror "+localPath+" to "+shortRevision(pushedRevision),
	)
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
