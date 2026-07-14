package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"braid/internal/gitexec"
	"braid/internal/testutil"
)

func TestDiffCommandEndpointModesReverseAndPathLimited(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "a.txt", "base a\n")
	testutil.WriteFile(t, upstream, "b.txt", "base b\n")
	testutil.WriteFile(t, upstream, "c.txt", "base c\n")
	testutil.CommitAll(t, upstream, "upstream")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	testutil.WriteFile(t, repo, "vendor/basic/a.txt", "committed a\n")
	testutil.Git(t, repo, "add", "vendor/basic/a.txt")
	testutil.Git(t, repo, "commit", "-m", "committed mirror change")
	testutil.WriteFile(t, repo, "vendor/basic/b.txt", "staged b\n")
	testutil.Git(t, repo, "add", "vendor/basic/b.txt")
	testutil.WriteFile(t, repo, "vendor/basic/c.txt", "unstaged c\n")

	allDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic"})
	assertContains(t, allDiff, "diff --git a/a.txt b/a.txt")
	assertContains(t, allDiff, "diff --git a/b.txt b/b.txt")
	assertContains(t, allDiff, "diff --git a/c.txt b/c.txt")

	headDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--head"})
	assertContains(t, headDiff, "diff --git a/a.txt b/a.txt")
	assertNotContains(t, headDiff, "diff --git a/b.txt b/b.txt")
	assertNotContains(t, headDiff, "diff --git a/c.txt b/c.txt")

	rawHeadDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--", "HEAD"})
	if rawHeadDiff != headDiff {
		t.Fatalf("raw HEAD diff differs from --head diff:\n--head:\n%s\nraw HEAD:\n%s", headDiff, rawHeadDiff)
	}

	indexDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--index"})
	assertContains(t, indexDiff, "diff --git a/a.txt b/a.txt")
	assertContains(t, indexDiff, "diff --git a/b.txt b/b.txt")
	assertNotContains(t, indexDiff, "diff --git a/c.txt b/c.txt")

	cachedDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--", "--cached"})
	if cachedDiff != indexDiff {
		t.Fatalf("raw --cached diff differs from --index diff:\n--index:\n%s\nraw --cached:\n%s", indexDiff, cachedDiff)
	}

	reverseDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--", "-R", "--cached"})
	assertContains(t, reverseDiff, "diff --git b/a.txt a/a.txt")

	limitedDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--", "vendor/basic/c.txt"})
	assertContains(t, limitedDiff, "diff --git a/c.txt b/c.txt")
	assertNotContains(t, limitedDiff, "diff --git a/a.txt b/a.txt")
	assertNotContains(t, limitedDiff, "diff --git a/b.txt b/b.txt")

	limitedHeadDiff := runCommandOK(t, repo, []string{"diff", "vendor/basic", "--head", "--", "--stat"})
	assertContains(t, limitedHeadDiff, "a.txt")
	assertNotContains(t, limitedHeadDiff, "b.txt")
	assertNotContains(t, limitedHeadDiff, "c.txt")
}

func TestDiffCommandHeadRequiresDownstreamCommit(t *testing.T) {
	repo := testutil.InitRepo(t)
	testutil.WriteFile(t, repo, ".braids.json", "{\"config_version\":2,\"sources\":{}}\n")

	stderr := runCommandError(t, repo, []string{"diff", "--head"})
	assertContains(t, stderr, "diff --head requires a downstream HEAD commit")

	indexOut, indexErr := runCommandOKWithOutput(t, repo, []string{"diff", "--index"})
	if indexOut != "" || indexErr != "" {
		t.Fatalf("unborn index diff output = (%q, %q), want empty", indexOut, indexErr)
	}
}

func TestDiffCommandFromSubdirectoryPreservesRawPassthroughPathspecs(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "a.txt", "base a\n")
	testutil.WriteFile(t, upstream, "b.txt", "base b\n")
	testutil.CommitAll(t, upstream, "upstream")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/a.txt", "changed a\n")
	testutil.WriteFile(t, repo, "vendor/basic/b.txt", "changed b\n")
	workDir := filepath.Join(repo, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	rawMatch := runCommandOKInDir(t, repo, workDir, []string{"diff", "../../vendor/basic", "--", "../../vendor/basic/b.txt"})
	assertContains(t, rawMatch, "diff --git a/b.txt b/b.txt")
	assertNotContains(t, rawMatch, "diff --git a/a.txt b/a.txt")

	rawMiss := runCommandErrorInDir(t, repo, workDir, []string{"diff", "../../vendor/basic", "--", "vendor/basic/b.txt"})
	assertContains(t, rawMiss, "ambiguous argument 'vendor/basic/b.txt'")
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
	assertContains(t, out, "Braid: Diffing mirror vendor/one")
	assertContains(t, out, "Braid: Diffing mirror vendor/two")
	assertContains(t, out, "one changed")
	assertContains(t, out, "two changed")
}

func TestDiffCommandSyncPushOnlyFiltersSources(t *testing.T) {
	upstreamEnabled := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamEnabled, "README.md", "enabled base\n")
	testutil.CommitAll(t, upstreamEnabled, "enabled")

	upstreamDisabled := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamDisabled, "README.md", "disabled base\n")
	testutil.CommitAll(t, upstreamDisabled, "disabled")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamEnabled, "--name", "enabled", "vendor/enabled-a", "vendor/enabled-b", "--sync-push"})
	runCommandOK(t, repo, []string{"add", upstreamDisabled, "--name", "disabled", "vendor/disabled"})
	testutil.WriteFile(t, repo, "vendor/enabled-a/README.md", "enabled a changed\n")
	testutil.WriteFile(t, repo, "vendor/enabled-b/README.md", "enabled b changed\n")
	testutil.WriteFile(t, repo, "vendor/disabled/README.md", "disabled changed\n")

	out := runCommandOK(t, repo, []string{"diff", "--sync-push-only"})
	assertContains(t, out, "Braid: Diffing mirror vendor/enabled-a")
	assertContains(t, out, "Braid: Diffing mirror vendor/enabled-b")
	assertContains(t, out, "enabled a changed")
	assertContains(t, out, "enabled b changed")
	assertNotContains(t, out, "vendor/disabled")
	assertNotContains(t, out, "disabled changed")

	sourceOut := runCommandOK(t, repo, []string{"diff", ":enabled", "--sync-push-only"})
	assertContains(t, sourceOut, "Braid: Diffing mirror vendor/enabled-a")
	assertContains(t, sourceOut, "Braid: Diffing mirror vendor/enabled-b")
	testutil.CommitAll(t, repo, "commit filtered changes")
	headOut := runCommandOK(t, repo, []string{"diff", "--sync-push-only", "--head"})
	assertContains(t, headOut, "enabled a changed")
	assertContains(t, headOut, "enabled b changed")
	assertNotContains(t, headOut, "vendor/disabled")
	assertNotContains(t, headOut, "disabled changed")

	disabledOut, disabledErr := runCommandOKWithOutput(t, repo, []string{"diff", "vendor/disabled", "--sync-push-only"})
	if disabledOut != "" || disabledErr != "" {
		t.Fatalf("disabled source output = (%q, %q), want empty", disabledOut, disabledErr)
	}

	disabledOnlyRepo := initDownstream(t)
	runCommandOK(t, disabledOnlyRepo, []string{"add", upstreamDisabled, "vendor/disabled"})
	testutil.WriteFile(t, disabledOnlyRepo, "vendor/disabled/README.md", "disabled changed\n")
	disabledOut, disabledErr = runCommandOKWithOutput(t, disabledOnlyRepo, []string{"diff", "--sync-push-only"})
	if disabledOut != "" || disabledErr != "" {
		t.Fatalf("no eligible sources output = (%q, %q), want empty", disabledOut, disabledErr)
	}
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
				t.Helper()
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
				t.Helper()
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
				t.Helper()
				testutil.WriteFile(t, upstream, "lib/component.txt", "subdir base\n")
				return testutil.CommitAll(t, upstream, "subdir base")
			},
			addArgs:   func(upstream, _ string) []string { return []string{"add", upstream, "vendor/lib=lib"} },
			localPath: "vendor/lib",
			localFile: "vendor/lib/component.txt",
			wantPath:  "component.txt",
		},
		{
			name: "path with spaces",
			prepare: func(t *testing.T, upstream string) string {
				t.Helper()
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
	runCommandOK(t, repo, []string{"add", upstream, "licenses/THIRD_PARTY.txt=LICENSE.txt"})
	testutil.WriteFile(t, repo, "licenses/THIRD_PARTY.txt", "committed license\n")
	testutil.Git(t, repo, "add", "licenses/THIRD_PARTY.txt")
	testutil.Git(t, repo, "commit", "-m", "commit license change")

	headOut := runCommandOK(t, repo, []string{"diff", "licenses/THIRD_PARTY.txt", "--head"})
	assertContains(t, headOut, "diff --git a/LICENSE.txt b/THIRD_PARTY.txt")
	assertContains(t, headOut, "committed license")

	testutil.WriteFile(t, repo, "licenses/THIRD_PARTY.txt", "staged license\n")
	testutil.Git(t, repo, "add", "licenses/THIRD_PARTY.txt")
	indexOut := runCommandOK(t, repo, []string{"diff", "licenses/THIRD_PARTY.txt", "--index"})
	assertContains(t, indexOut, "diff --git a/LICENSE.txt b/THIRD_PARTY.txt")
	assertContains(t, indexOut, "staged license")
}

func TestDiffCommandSingleFileFromSubdirectoryUsesTopAnchoredLimiter(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "LICENSE.txt", "license\n")
	testutil.CommitAll(t, upstream, "single file")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "licenses/THIRD_PARTY.txt=LICENSE.txt"})
	testutil.WriteFile(t, repo, "licenses/THIRD_PARTY.txt", "changed license\n")
	workDir := filepath.Join(repo, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	out := runCommandOKInDir(t, repo, workDir, []string{"diff", "../../licenses/THIRD_PARTY.txt"})
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
