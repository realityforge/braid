package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutableNoCommitAddUpdateRemoveWorkflow(t *testing.T) {
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
	writeFile(t, downstream, "tracked.txt", "tracked base\n")
	gitOK(t, env, downstream, "add", "tracked.txt")
	gitOK(t, env, downstream, "commit", "-m", "add tracked file")

	writeFile(t, downstream, "staged.txt", "staged content\n")
	gitOK(t, env, downstream, "add", "staged.txt")
	writeFile(t, downstream, "tracked.txt", "tracked dirty\n")
	writeFile(t, downstream, "untracked.txt", "untracked content\n")
	addHead := gitOutput(t, env, downstream, "rev-parse", "HEAD")

	add := runBraid(t, env, downstream, braid, "add", upstream, "vendor/basic", "--no-commit")
	assertExit(t, add, 0)
	assertContains(t, add.stdout, "Braid: warning: unrelated staged changes are present")
	assertContains(t, add.stdout, "Braid: staged add of mirror 'vendor/basic'")
	assertFile(t, downstream, "vendor/basic/README.md", "base\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: baseRevision},
	})
	assertCachedNames(t, env, downstream, ".braids.json", "staged.txt", "vendor/basic/README.md")
	assertEmpty(t, "add owned unstaged diff", gitOutput(t, env, downstream, "diff", "--name-only", "--", ".braids.json", "vendor/basic"))
	if got := gitOutput(t, env, downstream, "rev-parse", "HEAD"); got != addHead {
		t.Fatalf("HEAD = %s, want unchanged %s", got, addHead)
	}
	assertFile(t, downstream, "tracked.txt", "tracked dirty\n")
	assertFile(t, downstream, "untracked.txt", "untracked content\n")
	commitAll(t, env, downstream, "combined add")

	writeFile(t, upstream, "README.md", "updated\n")
	updateRevision := commitAll(t, env, upstream, "updated")
	workDir := filepath.Join(downstream, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workDir: %v", err)
	}
	updateHead := gitOutput(t, env, downstream, "rev-parse", "HEAD")
	update := runBraid(t, env, workDir, braid, "update", "../../vendor/basic", "--no-commit")
	assertExit(t, update, 0)
	assertContains(t, update.stdout, "Braid: staged update of mirror 'vendor/basic'")
	assertFile(t, downstream, "vendor/basic/README.md", "updated\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: updateRevision},
	})
	assertCachedNames(t, env, downstream, ".braids.json", "vendor/basic/README.md")
	if got := gitOutput(t, env, downstream, "rev-parse", "HEAD"); got != updateHead {
		t.Fatalf("HEAD = %s, want unchanged %s", got, updateHead)
	}
	commitAll(t, env, downstream, "combined update")

	remote := remoteName("main", "vendor/basic")
	gitOK(t, env, downstream, "remote", "add", remote, repositoryCachePath(t, downstream, "vendor/basic", configMirror{URL: upstream, Branch: "main"}))
	writeFile(t, downstream, "remove-staged.txt", "remove staged\n")
	gitOK(t, env, downstream, "add", "remove-staged.txt")
	removeHead := gitOutput(t, env, downstream, "rev-parse", "HEAD")
	remove := runBraid(t, env, downstream, braid, "remove", "vendor/basic", "--keep", "--no-commit")
	assertExit(t, remove, 0)
	assertContains(t, remove.stdout, "Braid: warning: unrelated staged changes are present")
	assertContains(t, remove.stdout, "Braid: staged removal of mirror 'vendor/basic'")
	assertPathMissing(t, downstream, "vendor/basic")
	assertConfigRaw(t, downstream, map[string]configMirror{})
	assertRemoteURL(t, env, downstream, remote, repositoryCachePath(t, downstream, "vendor/basic", configMirror{URL: upstream, Branch: "main"}))
	assertCachedNames(t, env, downstream, ".braids.json", "remove-staged.txt", "vendor/basic/README.md")
	if got := gitOutput(t, env, downstream, "rev-parse", "HEAD"); got != removeHead {
		t.Fatalf("HEAD = %s, want unchanged %s", got, removeHead)
	}
}

func TestExecutableNoCommitPullAllAndDirtyBlocker(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstreamA := filepath.Join(root, "upstream-a")
	initRepo(t, env, upstreamA)
	writeFile(t, upstreamA, "README.md", "a base\n")
	commitAll(t, env, upstreamA, "a base")
	upstreamB := filepath.Join(root, "upstream-b")
	initRepo(t, env, upstreamB)
	writeFile(t, upstreamB, "README.md", "b base\n")
	commitAll(t, env, upstreamB, "b base")
	upstreamLocked := filepath.Join(root, "upstream-locked")
	initRepo(t, env, upstreamLocked)
	writeFile(t, upstreamLocked, "README.md", "locked base\n")
	lockedRevision := commitAll(t, env, upstreamLocked, "locked base")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	assertExit(t, runBraid(t, env, downstream, braid, "--quiet", "add", upstreamA, "vendor/a"), 0)
	assertExit(t, runBraid(t, env, downstream, braid, "--quiet", "add", upstreamB, "vendor/b"), 0)
	assertExit(t, runBraid(t, env, downstream, braid, "--quiet", "add", upstreamLocked, "vendor/locked", "--revision", lockedRevision), 0)
	head := gitOutput(t, env, downstream, "rev-parse", "HEAD")

	writeFile(t, upstreamA, "README.md", "a updated\n")
	aRevision := commitAll(t, env, upstreamA, "a updated")
	writeFile(t, upstreamB, "README.md", "b updated\n")
	bRevision := commitAll(t, env, upstreamB, "b updated")

	pull := runBraid(t, env, downstream, braid, "pull", "--no-commit")
	assertExit(t, pull, 0)
	assertNotContains(t, pull.stdout, "Braid: warning: unrelated staged changes are present")
	assertContains(t, pull.stdout, "Braid: staged update of mirror 'vendor/a'")
	assertContains(t, pull.stdout, "Braid: staged update of mirror 'vendor/b'")
	assertContains(t, pull.stdout, "Braid: skipped revision-locked mirrors:\n  vendor/locked\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/a":      {URL: upstreamA, Branch: "main", Revision: aRevision},
		"vendor/b":      {URL: upstreamB, Branch: "main", Revision: bRevision},
		"vendor/locked": {URL: upstreamLocked, Revision: lockedRevision},
	})
	assertCachedNames(t, env, downstream, ".braids.json", "vendor/a/README.md", "vendor/b/README.md")
	if got := gitOutput(t, env, downstream, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD = %s, want unchanged %s", got, head)
	}

	gitOK(t, env, downstream, "restore", "--source=HEAD", "--staged", "--worktree", "--", ".braids.json", "vendor/a", "vendor/b")
	writeFile(t, downstream, "vendor/b/README.md", "dirty b\n")
	blocked := runBraid(t, env, downstream, braid, "pull", "--no-commit")
	assertExit(t, blocked, 1)
	assertContains(t, blocked.stderr, "local changes are present in vendor/b")
	assertCachedNames(t, env, downstream)
}

func TestExecutableNoCommitPullConflictMatchesExistingRecovery(t *testing.T) {
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
	add := runBraid(t, env, downstream, braid, "--quiet", "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: baseRevision},
	})

	writeFile(t, downstream, "vendor/basic/README.md", "local\n")
	commitAll(t, env, downstream, "local mirror change")
	head := gitOutput(t, env, downstream, "rev-parse", "HEAD")
	writeFile(t, downstream, "staged.txt", "staged content\n")
	gitOK(t, env, downstream, "add", "staged.txt")
	writeFile(t, upstream, "README.md", "remote\n")
	remoteRevision := commitAll(t, env, upstream, "remote change")

	update := runBraid(t, env, downstream, braid, "--quiet", "pull", "vendor/basic", "--no-commit")
	assertExit(t, update, 0)
	assertEmpty(t, "conflict update stderr", update.stderr)
	assertContains(t, update.stdout, "CONFLICT: vendor/basic/README.md")
	assertContains(t, update.stdout, "Braid: warning: unrelated staged changes are present")
	assertContains(t, update.stdout, "git add -- ':(top)vendor/basic' ':(top).braids.json'")
	assertContains(t, update.stdout, "git commit -F '.git/MERGE_MSG'")
	assertNotContains(t, update.stdout, "Braid: staged update of mirror")
	assertContains(t, readFile(t, downstream, "vendor/basic/README.md"), "<<<<<<<")
	assertContains(t, readFile(t, downstream, ".git/MERGE_MSG"), "Braid: Update mirror 'vendor/basic' to '"+shortRevision(remoteRevision)+"'")
	assertCachedNames(t, env, downstream, ".braids.json", "staged.txt")
	if got := gitOutput(t, env, downstream, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD = %s, want unchanged %s", got, head)
	}
}

func assertCachedNames(t *testing.T, env processEnv, repo string, want ...string) {
	t.Helper()
	got := strings.Fields(gitOK(t, env, repo, "diff", "--cached", "--name-only").stdout)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cached names = %#v, want %#v", got, want)
	}
}
