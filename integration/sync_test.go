package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutableSyncPushesThenUpdates(t *testing.T) {
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
	add := runBraid(t, env, downstream, braid, "--quiet", "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: baseRevision},
	})

	writeFile(t, downstream, "vendor/basic/README.md", "local\n")
	commitAll(t, env, downstream, "local mirror change")
	syncEnv := env.with("GIT_EDITOR", editorCommand(t, root, "Executable sync"))
	sync := runBraid(t, syncEnv, downstream, braid, "sync", "vendor/basic")
	assertExit(t, sync, 0)
	assertProgress(t, sync.stderr,
		"Braid: updated cache for mirror vendor/basic",
		"Braid: fetched mirror vendor/basic",
		"Braid: pushing mirror vendor/basic",
		"Braid: pushed mirror vendor/basic",
		"Braid: checked mirror vendor/basic",
		"Braid: updated mirror vendor/basic",
	)
	assertContains(t, sync.stdout, "Executable sync")

	assertFile(t, upstream, "README.md", "local\n")
	pushedRevision := gitOutput(t, env, upstream, "rev-parse", "HEAD")
	assertLatestCommit(t, env, upstream, defaultName+" <"+defaultEmail+">", "Executable sync")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: pushedRevision},
	})
	assertLatestCommit(t, env, downstream, defaultName+" <"+defaultEmail+">", "Braid: Update mirror 'vendor/basic' to '"+shortRevision(pushedRevision)+"'")
	assertNoRemote(t, env, downstream, remoteName("main", "vendor/basic"))
	assertClean(t, env, downstream)
}

func TestExecutablePushProvenanceTemplateTouchesGitDefaultTemplate(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "base\n")
	commitAll(t, env, upstream, "seed upstream")
	gitOK(t, env, upstream, "config", "--local", "receive.denyCurrentBranch", "updateInstead")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	add := runBraid(t, env, downstream, braid, "--quiet", "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")

	writeFile(t, downstream, "vendor/basic/README.md", "local\n")
	localRevision := commitAll(t, env, downstream, "local mirror change")
	capture, editor := capturingEditorCommand(t, root, "Executable push")
	pushEnv := env.with("GIT_EDITOR", editor)

	push := runBraid(t, pushEnv, downstream, braid, "push", "vendor/basic")
	assertExit(t, push, 0)
	assertProgress(t, push.stderr,
		"Braid: updated cache for mirror vendor/basic",
		"Braid: fetched mirror vendor/basic",
		"Braid: pushing mirror vendor/basic",
		"Braid: pushed mirror vendor/basic",
	)

	template := readFile(t, root, filepath.Base(capture))
	assertContains(t, template, "# Braid downstream mirror commit guidance for vendor/basic")
	assertContains(t, template, "# Commit "+localRevision)
	assertContains(t, template, "# local mirror change")
	assertContains(t, template, "# Please enter the commit message")
	assertNotContains(t, template, "BRAID_COMMIT_TEMPLATE")
}

func TestExecutableSyncPullOnlyUpdatesWithoutEditor(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "base\n")
	commitAll(t, env, upstream, "seed upstream")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	add := runBraid(t, env, downstream, braid, "--quiet", "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")

	writeFile(t, upstream, "README.md", "remote\n")
	remoteRevision := commitAll(t, env, upstream, "remote update")
	syncEnv := env.with("GIT_EDITOR", failingEditorCommand(t, root))
	sync := runBraid(t, syncEnv, downstream, braid, "--quiet", "sync", "--pull-only", "vendor/basic")
	assertResult(t, sync, 0, "", "")

	assertFile(t, downstream, "vendor/basic/README.md", "remote\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: remoteRevision},
	})
	assertLatestCommit(t, env, upstream, defaultName+" <"+defaultEmail+">", "remote update")
	assertNoRemote(t, env, downstream, remoteName("main", "vendor/basic"))
	assertClean(t, env, downstream)
}

func TestExecutableSyncAutostashRestoresSelectedState(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	upstream := filepath.Join(root, "upstream")
	initRepo(t, env, upstream)
	writeFile(t, upstream, "README.md", "base\n")
	commitAll(t, env, upstream, "seed upstream")

	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	add := runBraid(t, env, downstream, braid, "--quiet", "add", upstream, "vendor/basic")
	assertResult(t, add, 0, "", "")
	writeFile(t, downstream, "outside.txt", "outside base\n")
	gitOK(t, env, downstream, "add", "outside.txt")
	gitOK(t, env, downstream, "commit", "-m", "add outside")

	writeFile(t, downstream, "vendor/basic/README.md", "selected staged\n")
	gitOK(t, env, downstream, "add", "vendor/basic/README.md")
	writeFile(t, downstream, "vendor/basic/README.md", "selected unstaged\n")
	writeFile(t, downstream, "outside.txt", "outside staged\n")
	gitOK(t, env, downstream, "add", "outside.txt")
	writeFile(t, downstream, "outside.txt", "outside unstaged\n")
	outsideStatus := gitOK(t, env, downstream, "status", "--porcelain", "--", "outside.txt").stdout

	writeFile(t, upstream, "remote.txt", "remote\n")
	remoteRevision := commitAll(t, env, upstream, "remote update")
	sync := runBraid(t, env, downstream, braid, "--quiet", "sync", "--pull-only", "--autostash", "vendor/basic")
	assertResult(t, sync, 0, "", "")

	assertFile(t, downstream, "vendor/basic/remote.txt", "remote\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: remoteRevision},
	})
	if got := strings.TrimSpace(gitOK(t, env, downstream, "show", ":vendor/basic/README.md").stdout); got != "selected staged" {
		t.Fatalf("staged selected README = %q, want selected staged", got)
	}
	assertFile(t, downstream, "vendor/basic/README.md", "selected unstaged\n")
	if got := gitOK(t, env, downstream, "status", "--porcelain", "--", "outside.txt").stdout; got != outsideStatus {
		t.Fatalf("outside status changed from %q to %q", outsideStatus, got)
	}
	if stashList := strings.TrimSpace(gitOK(t, env, downstream, "stash", "list").stdout); stashList != "" {
		t.Fatalf("stash list = %q, want autostash dropped", stashList)
	}
}
