package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/testutil"
)

func TestProgressAddReportsRemoteOperationsAndQuietSuppresses(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	stdout, stderr := runCommandOKWithOutput(t, repo, []string{"add", upstream, "vendor/basic"})

	assertEmptyOutput(t, "add stdout", stdout)
	assertInOrder(t, stderr,
		"Braid: detecting default branch for source :001",
		"Braid: detected default branch for source :001",
		"Braid: updating cache for source :001",
		"Braid: updated cache for source :001",
		"Braid: fetching source :001",
		"Braid: fetched source :001",
	)
	assertNotContains(t, stderr, upstream)

	stdout, stderr = runCommandOKWithOutput(t, repo, []string{"--quiet", "add", ":001", "vendor/quiet"})
	assertEmptyOutput(t, "quiet add stdout", stdout)
	assertNoProgressOutput(t, stderr)
}

func TestProgressPullReportsNoopUpdateAndQuietSuppresses(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"--quiet", "add", upstream, "vendor/basic"})

	stdout, stderr := runCommandOKWithOutput(t, repo, []string{"pull", "vendor/basic"})
	assertContains(t, stdout, "Braid: source :001 is already up to date")
	assertInOrder(t, stderr,
		"Braid: updating cache for source :001",
		"Braid: updated cache for source :001",
		"Braid: fetching source :001",
		"Braid: fetched source :001",
		"Braid: checking source :001",
		"Braid: checked source :001",
	)
	assertNotContains(t, stderr, "Braid: updating source :001")

	testutil.WriteFile(t, upstream, "README.md", "remote\n")
	revision := testutil.CommitAll(t, upstream, "remote")
	stdout, stderr = runCommandOKWithOutput(t, repo, []string{"pull", "vendor/basic"})
	assertEmptyOutput(t, "pull stdout", stdout)
	assertInOrder(t, stderr,
		"Braid: updating cache for source :001",
		"Braid: fetching source :001",
		"Braid: checking source :001",
		"Braid: checked source :001",
		"Braid: updating source :001",
		"Braid: updated source :001 to "+revision[:7],
	)
	assertFile(t, repo, "vendor/basic/README.md", "remote\n")

	testutil.WriteFile(t, upstream, "README.md", "quiet remote\n")
	quietRevision := testutil.CommitAll(t, upstream, "quiet remote")
	stdout, stderr = runCommandOKWithOutput(t, repo, []string{"--quiet", "pull", "vendor/basic"})
	assertEmptyOutput(t, "quiet pull stdout", stdout)
	assertNoProgressOutput(t, stderr)
	assertFile(t, repo, "vendor/basic/README.md", "quiet remote\n")
	if got := loadMirror(t, repo, "vendor/basic").Revision; got != quietRevision {
		t.Fatalf("quiet pull revision = %q, want %q", got, quietRevision)
	}
}

func TestProgressStatusKeepsDataOnStdoutAndQuietSuppresses(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"--quiet", "add", upstream, "vendor/basic"})
	testutil.WriteFile(t, upstream, "README.md", "remote\n")
	testutil.CommitAll(t, upstream, "remote")

	stdout, stderr := runCommandOKWithOutput(t, repo, []string{"status", "vendor/basic"})
	assertContains(t, stdout, "vendor/basic (")
	assertContains(t, stdout, "Modified Remotely")
	assertInOrder(t, stderr,
		"Braid: updating cache for source :001",
		"Braid: updated cache for source :001",
		"Braid: fetching source :001",
		"Braid: fetched source :001",
	)

	stdout, stderr = runCommandOKWithOutput(t, repo, []string{"--quiet", "status", "vendor/basic"})
	assertContains(t, stdout, "vendor/basic (")
	assertContains(t, stdout, "Modified Remotely")
	assertNoProgressOutput(t, stderr)
}

func TestProgressDiffHydrationKeepsDataOnStdoutAndQuietSuppresses(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"--quiet", "add", upstream, "vendor/basic"})

	clone := cloneWithoutBaseRevision(t, repo, revision)
	testutil.WriteFile(t, clone, "vendor/basic/README.md", "changed\n")
	stdout, stderr := runCommandOKWithOutput(t, clone, []string{"diff", "vendor/basic"})
	assertContains(t, stdout, "diff --git a/README.md b/README.md")
	assertContains(t, stdout, "changed")
	assertInOrder(t, stderr,
		"Braid: updating cache for source :001",
		"Braid: updated cache for source :001",
		"Braid: fetching source :001",
		"Braid: fetched source :001",
	)

	quietClone := cloneWithoutBaseRevision(t, repo, revision)
	testutil.WriteFile(t, quietClone, "vendor/basic/README.md", "quiet changed\n")
	stdout, stderr = runCommandOKWithOutput(t, quietClone, []string{"--quiet", "diff", "vendor/basic"})
	assertContains(t, stdout, "diff --git a/README.md b/README.md")
	assertContains(t, stdout, "quiet changed")
	assertNoProgressOutput(t, stderr)
}

func TestProgressPushReportsRemoteOperationsAndQuietPreservesResults(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"--quiet", "add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Push progress"))

	stdout, stderr := runCommandOKWithOutput(t, repo, []string{"push", "vendor/basic"})
	assertContains(t, stdout, "Push progress")
	assertInOrder(t, stderr,
		"Braid: updating cache for source :001",
		"Braid: updated cache for source :001",
		"Braid: fetching source :001",
		"Braid: fetched source :001",
		"Braid: pushing source :001",
		"Braid: pushed source :001",
	)
	assertFile(t, upstream, "README.md", "local\n")

	stdout, stderr = runCommandOKWithOutput(t, repo, []string{"--quiet", "push", "vendor/basic"})
	assertContains(t, stdout, "Braid: Source is not up to date. Stopping.")
	assertNoProgressOutput(t, stderr)
}

func TestProgressQuietPreservesWarningsAndErrors(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"--quiet", "add", upstream, "vendor/basic"})
	validConfig, err := os.ReadFile(filepath.Join(repo, config.FileName))
	if err != nil {
		t.Fatalf("read valid config: %v", err)
	}
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "quiet warning\n")
	testutil.WriteFile(t, repo, config.FileName, "{not json\n")
	commitAllWithMessage(t, repo, "malformed historical config")
	if err := os.WriteFile(filepath.Join(repo, config.FileName), validConfig, 0o644); err != nil {
		t.Fatalf("restore valid config: %v", err)
	}
	commitAllWithMessage(t, repo, "restore valid config")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Push with warning"))

	stdout, stderr := runCommandOKWithOutput(t, repo, []string{"--quiet", "push", "vendor/basic"})
	assertEmptyOutput(t, "quiet warning push stdout", stdout)
	assertContains(t, stderr, "Braid: warning: push provenance guidance skipped")
	assertNoProgressOutput(t, stderr)
	assertFile(t, upstream, "README.md", "quiet warning\n")

	stderr = runCommandError(t, repo, []string{"--quiet", "status", "vendor/missing"})
	assertContains(t, stderr, "braid: mirror does not exist: vendor/missing")
	assertNoProgressOutput(t, stderr)
}

func cloneWithoutBaseRevision(t *testing.T, repo, revision string) string {
	t.Helper()
	parent := t.TempDir()
	clone := filepath.Join(parent, "clone")
	testutil.Git(t, parent, "clone", "--no-local", repo, clone)
	if result, err := gitexec.New(clone, false, nil).RunOK(context.Background(), "rev-parse", "--verify", "--quiet", revision+"^{commit}"); err == nil {
		t.Fatalf("base revision unexpectedly present in clone: %s", result.Stdout)
	}
	return clone
}

func assertEmptyOutput(t *testing.T, name, value string) {
	t.Helper()
	if value != "" {
		t.Fatalf("%s = %q, want empty", name, value)
	}
}

func assertNoProgressOutput(t *testing.T, stderr string) {
	t.Helper()
	for _, unwanted := range []string{
		"Braid: detecting default branch",
		"Braid: detected default branch",
		"Braid: updating cache",
		"Braid: updated cache",
		"Braid: fetching mirror",
		"Braid: fetched mirror",
		"Braid: checking mirror",
		"Braid: checked mirror",
		"Braid: updating mirror",
		"Braid: updated mirror",
		"Braid: pushing mirror",
		"Braid: pushed mirror",
		"Braid: setting up mirror remote",
		"Braid: set up mirror remote",
	} {
		assertNotContains(t, stderr, unwanted)
	}
}
