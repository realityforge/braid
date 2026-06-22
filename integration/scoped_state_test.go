package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

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
