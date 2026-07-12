package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	add := runBraid(t, env, downstream, braid, "--quiet", "add", upstream, "vendor/basic")
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

	update := runBraid(t, env, downstream, braid, "--quiet", "update", "vendor/basic")
	assertExit(t, update, 0)
	assertEmpty(t, "conflict update stderr", update.stderr)
	assertContains(t, update.stdout, "  vendor/basic/README.md")
	assertContains(t, update.stdout, "Braid: warning: unrelated staged changes are present")
	assertContains(t, update.stdout, "git add -- ':(top)vendor/basic' ':(top).braids.json'")
	assertContains(t, update.stdout, "git commit -F '.git/MERGE_MSG'")
	conflicted := readFile(t, downstream, "vendor/basic/README.md")
	assertContains(t, conflicted, "<<<<<<<")
	assertContains(t, conflicted, "local")
	assertContains(t, conflicted, "remote")
	assertContains(t, readFile(t, downstream, ".git/MERGE_MSG"), "Braid: Update source 'upstream' to '"+shortRevision(remoteRevision)+"'")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: remoteRevision},
	})
	if unmerged := strings.TrimSpace(gitOK(t, env, downstream, "ls-files", "-u").stdout); len(strings.Split(unmerged, "\n")) != 3 {
		t.Fatalf("unmerged entries = %q, want stages 1, 2, and 3", unmerged)
	}
	if cached := strings.Fields(gitOK(t, env, downstream, "diff", "--cached", "--name-only").stdout); strings.Join(cached, "\n") != ".braids.json\nstaged.txt\nvendor/basic/README.md" {
		t.Fatalf("cached names = %#v, want config, staged unrelated file, and conflicted mirror", cached)
	}
	if got := strings.TrimSpace(gitOK(t, env, downstream, "show", ":staged.txt").stdout); got != "staged content" {
		t.Fatalf("staged blob = %q, want staged content", got)
	}
	assertContains(t, gitOK(t, env, downstream, "status", "--porcelain").stdout, "README.md")
}

func TestExecutableSubdirectoryConflictRecoveryCommands(t *testing.T) {
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
	writeFile(t, upstream, "README.md", "remote\n")
	remoteRevision := commitAll(t, env, upstream, "remote change")
	workDir := filepath.Join(downstream, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	update := runBraid(t, env, workDir, braid, "--quiet", "update", "../../vendor/basic")
	assertExit(t, update, 0)
	assertEmpty(t, "subdir conflict stderr", update.stderr)
	assertContains(t, update.stdout, "  vendor/basic/README.md")
	assertContains(t, update.stdout, "git add -- ':(top)vendor/basic' ':(top).braids.json'")
	assertContains(t, update.stdout, "git commit -F '../../.git/MERGE_MSG'")
	assertContains(t, readFile(t, downstream, ".git/MERGE_MSG"), "Braid: Update source 'upstream' to '"+shortRevision(remoteRevision)+"'")

	writeFile(t, downstream, "vendor/basic/README.md", "resolved\n")
	gitOK(t, env, workDir, "add", "--", ":(top)vendor/basic", ":(top).braids.json")
	gitOK(t, env, workDir, "commit", "-F", "../../.git/MERGE_MSG")
	assertLatestCommit(t, env, downstream, defaultName+" <"+defaultEmail+">", "Braid: Update source 'upstream' to '"+shortRevision(remoteRevision)+"'")
	assertFile(t, downstream, "vendor/basic/README.md", "resolved\n")
	assertConfigRaw(t, downstream, map[string]configMirror{
		"vendor/basic": {URL: upstream, Branch: "main", Revision: remoteRevision},
	})
	assertClean(t, env, downstream)
}
