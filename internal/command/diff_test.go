package command

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/gitexec"
	"braid/internal/testutil"
)

func TestDiffCommandShowsStagedUnstagedReverseAndPathLimited(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "a.txt", "base a\n")
	testutil.WriteFile(t, upstream, "b.txt", "base b\n")
	testutil.CommitAll(t, upstream, "upstream")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	testutil.WriteFile(t, repo, "vendor/basic/a.txt", "staged a\n")
	testutil.Git(t, repo, "add", "vendor/basic/a.txt")
	testutil.WriteFile(t, repo, "vendor/basic/b.txt", "unstaged b\n")

	allDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic"})
	assertContains(t, allDiff, "diff --git a/a.txt b/a.txt")
	assertContains(t, allDiff, "diff --git a/b.txt b/b.txt")

	cachedDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--", "--cached"})
	assertContains(t, cachedDiff, "diff --git a/a.txt b/a.txt")
	assertNotContains(t, cachedDiff, "diff --git a/b.txt b/b.txt")

	reverseDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--", "-R", "--cached"})
	assertContains(t, reverseDiff, "diff --git b/a.txt a/a.txt")

	limitedDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--", "vendor/basic/b.txt"})
	assertContains(t, limitedDiff, "diff --git a/b.txt b/b.txt")
	assertNotContains(t, limitedDiff, "diff --git a/a.txt b/a.txt")
}

func TestDiffCommandAllMirrors(t *testing.T) {
	upstreamOne := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamOne, "README.md", "one\n")
	testutil.CommitAll(t, upstreamOne, "one")

	upstreamTwo := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamTwo, "README.md", "two\n")
	testutil.CommitAll(t, upstreamTwo, "two")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamOne, "vendor/one"})
	runCommandOK(t, repo, []string{"add", upstreamTwo, "vendor/two"})
	testutil.WriteFile(t, repo, "vendor/one/README.md", "one changed\n")
	testutil.WriteFile(t, repo, "vendor/two/README.md", "two changed\n")

	out := runCommandOK(t, repo, []string{"diff"})
	assertContains(t, out, "Braid: Diffing vendor/one")
	assertContains(t, out, "Braid: Diffing vendor/two")
	assertContains(t, out, "one changed")
	assertContains(t, out, "two changed")
}

func TestDiffCommandMirrorVariants(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(t *testing.T, upstream string) string
		addArgs   func(upstream, revision string) []string
		localPath string
		localFile string
		wantPath  string
	}{
		{
			name: "tag",
			prepare: func(t *testing.T, upstream string) string {
				testutil.WriteFile(t, upstream, "README.md", "tag base\n")
				revision := testutil.CommitAll(t, upstream, "tag base")
				testutil.Git(t, upstream, "tag", "v1")
				return revision
			},
			addArgs:   func(upstream, _ string) []string { return []string{"add", upstream, "vendor/tagged", "--tag", "v1"} },
			localPath: "vendor/tagged",
			localFile: "vendor/tagged/README.md",
			wantPath:  "README.md",
		},
		{
			name: "revision",
			prepare: func(t *testing.T, upstream string) string {
				testutil.WriteFile(t, upstream, "README.md", "revision base\n")
				return testutil.CommitAll(t, upstream, "revision base")
			},
			addArgs: func(upstream, revision string) []string {
				return []string{"add", upstream, "vendor/revision", "--revision", revision}
			},
			localPath: "vendor/revision",
			localFile: "vendor/revision/README.md",
			wantPath:  "README.md",
		},
		{
			name: "subdirectory",
			prepare: func(t *testing.T, upstream string) string {
				testutil.WriteFile(t, upstream, "lib/component.txt", "subdir base\n")
				return testutil.CommitAll(t, upstream, "subdir base")
			},
			addArgs:   func(upstream, _ string) []string { return []string{"add", upstream, "vendor/lib", "--path", "lib"} },
			localPath: "vendor/lib",
			localFile: "vendor/lib/component.txt",
			wantPath:  "component.txt",
		},
		{
			name: "path with spaces",
			prepare: func(t *testing.T, upstream string) string {
				testutil.WriteFile(t, upstream, "README.md", "spaces base\n")
				return testutil.CommitAll(t, upstream, "spaces base")
			},
			addArgs:   func(upstream, _ string) []string { return []string{"add", upstream, "vendor/path with spaces"} },
			localPath: "vendor/path with spaces",
			localFile: "vendor/path with spaces/README.md",
			wantPath:  "README.md",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			revision := test.prepare(t, upstream)
			repo := initDownstream(t)
			runCommandOK(t, repo, test.addArgs(upstream, revision))
			testutil.WriteFile(t, repo, test.localFile, test.name+" changed\n")

			out := runCommandOK(t, repo, []string{"diff", test.localPath})
			assertContains(t, out, "diff --git a/"+test.wantPath+" b/"+test.wantPath)
			assertContains(t, out, test.name+" changed")
		})
	}
}

func TestDiffCommandSingleFilePrefixes(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "LICENSE.txt", "license\n")
	testutil.CommitAll(t, upstream, "single file")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "licenses/THIRD_PARTY.txt", "--path", "LICENSE.txt"})
	testutil.WriteFile(t, repo, "licenses/THIRD_PARTY.txt", "changed license\n")

	out := runCommandOK(t, repo, []string{"diff", "licenses/THIRD_PARTY.txt"})
	assertContains(t, out, "diff --git a/LICENSE.txt b/THIRD_PARTY.txt")
	assertContains(t, out, "changed license")
}

func TestDiffCommandFetchesMissingBaseRevision(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "upstream")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	parent := t.TempDir()
	clone := filepath.Join(parent, "clone")
	testutil.Git(t, parent, "clone", "--no-local", repo, clone)
	if result, err := gitexec.New(clone, false, nil).RunOK(context.Background(), "rev-parse", "--verify", "--quiet", revision+"^{commit}"); err == nil {
		t.Fatalf("base revision unexpectedly present in clone: %s", result.Stdout)
	}

	testutil.WriteFile(t, clone, "vendor/basic/README.md", "changed\n")
	out := runCommandOK(t, clone, []string{"diff", "vendor/basic"})
	assertContains(t, out, "diff --git a/README.md b/README.md")
	assertContains(t, out, "changed")
}

func assertContains(t *testing.T, value, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("output does not contain %q:\n%s", want, value)
	}
}

func assertNotContains(t *testing.T, value, unwanted string) {
	t.Helper()
	if strings.Contains(value, unwanted) {
		t.Fatalf("output contains %q unexpectedly:\n%s", unwanted, value)
	}
}
