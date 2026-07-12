package command

import (
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/testutil"
)

func TestStatusCommandStates(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		testutil.CommitAll(t, upstream, "upstream")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

		out := runCommandOK(t, repo, []string{"status", "vendor/basic"})
		assertContains(t, out, "vendor/basic (")
		assertContains(t, out, "[BRANCH=main]")
		assertNotContains(t, out, "Modified")
		assertNotContains(t, out, "Removed Locally")
	})

	t.Run("remote modified", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		testutil.CommitAll(t, upstream, "upstream")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

		testutil.WriteFile(t, upstream, "README.md", "remote changed\n")
		testutil.CommitAll(t, upstream, "remote changed")
		out := runCommandOK(t, repo, []string{"status", "vendor/basic"})
		assertContains(t, out, "Modified Remotely")
		assertNotContains(t, out, "Modified Locally")
	})

	t.Run("locally modified", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		testutil.CommitAll(t, upstream, "upstream")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

		testutil.WriteFile(t, repo, "vendor/basic/README.md", "local changed\n")
		testutil.CommitAll(t, repo, "local change")
		out := runCommandOK(t, repo, []string{"status", "vendor/basic"})
		assertContains(t, out, "Modified Locally")
		assertNotContains(t, out, "Modified Remotely")
	})

	t.Run("removed locally", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		testutil.CommitAll(t, upstream, "upstream")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

		testutil.Git(t, repo, "rm", "-r", "vendor/basic")
		testutil.Git(t, repo, "commit", "-m", "remove mirror content")
		out := runCommandOK(t, repo, []string{"status", "vendor/basic"})
		assertContains(t, out, "Removed Locally")
		assertNotContains(t, out, "Modified Locally")
	})
}

func TestStatusCommandNormalizesNativeLocalPathSelector(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "upstream")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	out := runCommandOK(t, repo, []string{"status", `vendor\basic`})
	assertContains(t, out, "vendor/basic (")
}

func TestStatusCommandFromSubdirectoryRequiresExactMirrorSelector(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "nested/file.txt", "base\n")
	testutil.CommitAll(t, upstream, "upstream")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	mirrorDir := filepath.Join(repo, "vendor", "basic")
	out := runCommandOKInDir(t, repo, mirrorDir, []string{"status", "."})
	assertContains(t, out, "vendor/basic (")

	nestedDir := filepath.Join(mirrorDir, "nested")
	stderr := runCommandErrorInDir(t, repo, nestedDir, []string{"status", "."})
	assertContains(t, stderr, "mirror does not exist: vendor/basic/nested")
}

func TestStatusCommandMirrorModes(t *testing.T) {
	t.Run("tag", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "tagged\n")
		testutil.CommitAll(t, upstream, "tagged")
		testutil.Git(t, upstream, "tag", "v1")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/tagged", "--tag", "v1"})

		out := runCommandOK(t, repo, []string{"status", "vendor/tagged"})
		assertContains(t, out, "[TAG=v1]")
	})

	t.Run("revision locked", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "locked\n")
		revision := testutil.CommitAll(t, upstream, "locked")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/locked", "--revision", revision})

		out := runCommandOK(t, repo, []string{"status", "vendor/locked"})
		assertContains(t, out, "[REVISION LOCKED]")
		assertNotContains(t, out, "Remote Modified")
	})

	t.Run("subdirectory path with spaces", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "lay outs/layout.txt", "layout\n")
		testutil.CommitAll(t, upstream, "spaces")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/path with spaces=lay outs"})

		out := runCommandOK(t, repo, []string{"status", "vendor/path with spaces"})
		if !strings.HasPrefix(out, "vendor/path with spaces (") {
			t.Fatalf("status output = %q, want path with spaces prefix", out)
		}
		assertContains(t, out, "[BRANCH=main]")
	})
}
