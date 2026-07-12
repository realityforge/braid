package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutablePrimaryLifecycle(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream-repo")
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
	sourceName := "upstream-repo"
	remote := remoteName("main", sourceName)
	cacheRemoteURL := repositoryCachePath(t, downstream, localPath, configMirror{URL: upstream, Branch: "main", Path: "lib dir"})

	add := runBraid(t, env, downstream, braid, "add", upstream, localPath+"=lib dir")
	assertExit(t, add, 0)
	assertEmpty(t, "add stdout", add.stdout)
	assertProgress(t, add.stderr,
		"Braid: detecting default branch for source :"+sourceName,
		"Braid: updated cache for source :"+sourceName,
		"Braid: fetched source :"+sourceName,
	)
	assertFile(t, downstream, "vendor/lib with spaces/component.txt", "base\n")
	assertFile(t, downstream, "vendor/lib with spaces/kept.txt", "kept\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		localPath: {URL: upstream, Branch: "main", Path: "lib dir", Revision: baseRevision},
	})
	assertLatestCommit(t, env, downstream, defaultName+" <"+defaultEmail+">", "Braid: Add source '"+sourceName+"' at '"+shortRevision(baseRevision)+"'")
	assertNoRemote(t, env, downstream, remote)
	assertPathExists(t, filepath.Join(cacheRemoteURL, "HEAD"))
	assertClean(t, env, downstream)

	status := runBraid(t, env, downstream, braid, "status", localPath)
	assertExit(t, status, 0)
	assertProgress(t, status.stderr,
		"Braid: updated cache for source :"+sourceName,
		"Braid: fetched source :"+sourceName,
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
		"Braid: updated cache for source :"+sourceName,
		"Braid: fetched source :"+sourceName,
		"Braid: pushing source :"+sourceName,
		"Braid: pushed source :"+sourceName,
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
		"Braid: updated cache for source :"+sourceName,
		"Braid: fetched source :"+sourceName,
		"Braid: checked source :"+sourceName,
		"Braid: updated source :"+sourceName+" to "+shortRevision(pushedRevision),
	)
	assertFile(t, downstream, "vendor/lib with spaces/component.txt", "local\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		localPath: {URL: upstream, Branch: "main", Path: "lib dir", Revision: pushedRevision},
	})
	assertLatestCommit(t, env, downstream, defaultName+" <"+defaultEmail+">", "Braid: Update source '"+sourceName+"' to '"+shortRevision(pushedRevision)+"'")
	assertNoRemote(t, env, downstream, remote)
	assertClean(t, env, downstream)

	remove := runBraid(t, env, downstream, braid, "remove", localPath)
	assertResult(t, remove, 0, "", "")
	assertPathMissing(t, downstream, localPath)
	assertConfigRaw(t, downstream, map[string]configMirror{})
	assertLatestCommit(t, env, downstream, defaultName+" <"+defaultEmail+">", "Braid: Remove source '"+sourceName+"'")
	assertNoRemote(t, env, downstream, remote)
	assertClean(t, env, downstream)
}

func TestUpgradeConfigCommitAndNoCommit(t *testing.T) {
	for _, noCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "no-commit"}[noCommit], func(t *testing.T) {
			root := t.TempDir()
			env := newProcessEnv(t, root)
			braid := braidBinary(t)
			repo := filepath.Join(root, "repo")
			initRepo(t, env, repo)
			if err := os.WriteFile(filepath.Join(repo, ".braids.json"), []byte("{\"config_version\":1,\"mirrors\":{}}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitOK(t, env, repo, "add", ".braids.json")
			gitOK(t, env, repo, "commit", "-m", "version one")
			before := gitOutput(t, env, repo, "rev-parse", "HEAD")
			args := []string{"upgrade-config"}
			if noCommit {
				args = append(args, "--no-commit")
			}
			result := runBraid(t, env, repo, braid, args...)
			assertExit(t, result, 0)
			if noCommit {
				if got := gitOutput(t, env, repo, "rev-parse", "HEAD"); got != before {
					t.Fatalf("HEAD moved: %s", got)
				}
				assertContains(t, gitOK(t, env, repo, "diff", "--cached").stdout, `"config_version": 2`)
			} else {
				assertLatestCommit(t, env, repo, defaultName+" <"+defaultEmail+">", "Upgrade Braid config to version 2")
				assertClean(t, env, repo)
			}
		})
	}
}

func TestPartialCloneSubdirectoryLifecycle(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)
	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	gitOK(t, env, upstream, "config", "uploadpack.allowFilter", "true")
	gitOK(t, env, upstream, "config", "receive.denyCurrentBranch", "updateInstead")
	writeFile(t, upstream, "wanted/file.txt", "one\n")
	writeFile(t, upstream, "other/large.txt", string(make([]byte, 1024*1024)))
	first := commitAll(t, env, upstream, "initial")
	unrelatedBlob := gitOutput(t, env, upstream, "rev-parse", "HEAD:other/large.txt")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "initial")

	result := runBraid(t, env, downstream, braid, "add", upstream, "vendor/wanted=wanted", "--partial-clone")
	assertExit(t, result, 0)
	assertFile(t, downstream, "vendor/wanted/file.txt", "one\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/wanted": {URL: upstream, Branch: "main", Path: "wanted", Revision: first, PartialClone: true},
	})
	cachePath := repositoryCachePath(t, downstream, "vendor/wanted", configMirror{URL: upstream, Branch: "main", Path: "wanted"})
	missing := runProcess(t, env.with("GIT_NO_LAZY_FETCH", "1"), cachePath, "git", "cat-file", "-e", unrelatedBlob)
	if missing.exitCode == 0 {
		t.Fatalf("unrelated blob %s unexpectedly present in partial cache", unrelatedBlob)
	}
	missing = runProcess(t, env.with("GIT_NO_LAZY_FETCH", "1"), downstream, "git", "cat-file", "-e", unrelatedBlob)
	if missing.exitCode == 0 {
		t.Fatalf("unrelated blob %s unexpectedly present downstream", unrelatedBlob)
	}

	writeFile(t, downstream, "vendor/wanted/file.txt", "pushed\n")
	commitAll(t, env, downstream, "local partial mirror change")
	result = runBraid(t, env, downstream, braid, "push", "vendor/wanted", "--message", "Push partial mirror")
	assertExit(t, result, 0)
	assertFile(t, upstream, "wanted/file.txt", "pushed\n")
	assertFile(t, upstream, "other/large.txt", string(make([]byte, 1024*1024)))
	assertLatestCommit(t, env, upstream, defaultName+" <"+defaultEmail+">", "Push partial mirror")
	missing = runProcess(t, env.with("GIT_NO_LAZY_FETCH", "1"), cachePath, "git", "cat-file", "-e", unrelatedBlob)
	if missing.exitCode == 0 {
		t.Fatalf("unrelated blob %s unexpectedly present in partial cache after push", unrelatedBlob)
	}
	result = runBraid(t, env, downstream, braid, "pull", "vendor/wanted")
	assertExit(t, result, 0)
	assertFile(t, downstream, "vendor/wanted/file.txt", "pushed\n")

	writeFile(t, upstream, "wanted/file.txt", "two\n")
	second := commitAll(t, env, upstream, "update")
	result = runBraid(t, env, downstream, braid, "pull", "vendor/wanted")
	assertExit(t, result, 0)
	assertFile(t, downstream, "vendor/wanted/file.txt", "two\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/wanted": {URL: upstream, Branch: "main", Path: "wanted", Revision: second, PartialClone: true},
	})
	assertClean(t, env, downstream)
}

func TestPartialCloneRejectsUpstreamWithoutFilterSupport(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)
	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "wanted/file.txt", "content\n")
	commitAll(t, env, upstream, "initial")
	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "initial")

	result := runBraid(t, env, downstream, braid, "add", upstream, "vendor/wanted=wanted", "--partial-clone")
	assertExit(t, result, 1)
	assertContains(t, result.stderr, "does not support partial clone filtering")
	assertClean(t, env, downstream)
}
