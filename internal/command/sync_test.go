package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
	"braid/internal/testutil"
)

func TestSyncCommandPushesChangedBranchThenUpdates(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	testutil.Git(t, repo, "config", "--local", "user.name", "Sync User")
	testutil.Git(t, repo, "config", "--local", "user.email", "sync@example.invalid")
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Sync push"))

	out := runCommandOK(t, repo, []string{"sync", "vendor/basic"})

	assertContains(t, out, "Sync push")
	assertNotContains(t, out, "No local changes")
	assertFile(t, upstream, "README.md", "local\n")
	assertCommitSubject(t, upstream, "Sync push")
	gotIdentity := strings.TrimSpace(testutil.Git(t, upstream, "log", "-1", "--pretty=%an <%ae>").Stdout)
	if gotIdentity != "Sync User <sync@example.invalid>" {
		t.Fatalf("pushed identity = %q", gotIdentity)
	}
	pushedRevision := testutil.CurrentRevision(t, upstream)
	if got := loadMirror(t, repo, "vendor/basic").Revision; got != pushedRevision {
		t.Fatalf("synced revision = %q, want %q", got, pushedRevision)
	}
	assertCommitSubject(t, repo, "Braid: Update mirror 'vendor/basic' to '"+pushedRevision[:7]+"'")
	assertNoGitRemote(t, repo, "main_braid_vendor_basic")
}

func TestSyncCommandPullOnlyUpdatesWithoutPushing(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, upstream, "README.md", "remote\n")
	remoteRevision := testutil.CommitAll(t, upstream, "remote")
	t.Setenv("GIT_EDITOR", writeFailingEditor(t))

	runCommandOK(t, repo, []string{"sync", "--pull-only", "vendor/basic"})

	assertFile(t, repo, "vendor/basic/README.md", "remote\n")
	if got := loadMirror(t, repo, "vendor/basic").Revision; got != remoteRevision {
		t.Fatalf("revision = %q, want %q", got, remoteRevision)
	}
	assertCommitSubject(t, upstream, "remote")
}

func TestSyncCommandPullOnlyAllowsExplicitRevisionLockedMirror(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/revision", "--revision", revision})
	head := testutil.CurrentRevision(t, repo)
	t.Setenv("GIT_EDITOR", writeFailingEditor(t))

	out := runCommandOK(t, repo, []string{"sync", "--pull-only", "vendor/revision"})
	if out != "" {
		t.Fatalf("sync stdout = %q, want quiet output", out)
	}

	if got := testutil.CurrentRevision(t, repo); got != head {
		t.Fatalf("repo HEAD = %q, want unchanged %q", got, head)
	}
	if got := loadMirror(t, repo, "vendor/revision").Revision; got != revision {
		t.Fatalf("revision = %q, want %q", got, revision)
	}
}

func TestSyncCommandNoPathSelectsBranchAndTagMirrorsAndSkipsLocked(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")
	testutil.Git(t, upstreamB, "tag", "v1")
	upstreamLocked := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamLocked, "README.md", "locked base\n")
	lockedRevision := testutil.CommitAll(t, upstreamLocked, "locked base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b", "--tag", "v1"})
	runCommandOK(t, repo, []string{"add", upstreamLocked, "vendor/locked", "--revision", lockedRevision})
	testutil.WriteFile(t, repo, "vendor/locked/README.md", "dirty locked\n")

	testutil.WriteFile(t, upstreamA, "README.md", "a remote\n")
	aRevision := testutil.CommitAll(t, upstreamA, "a remote")
	testutil.WriteFile(t, upstreamB, "README.md", "b remote\n")
	bRevision := testutil.CommitAll(t, upstreamB, "b remote")
	testutil.Git(t, upstreamB, "tag", "-f", "v1")

	out := runCommandOK(t, repo, []string{"sync", "--pull-only"})
	if want := skippedLockedOutput("vendor/locked"); out != want {
		t.Fatalf("sync stdout = %q, want %q", out, want)
	}

	if got := loadMirror(t, repo, "vendor/a").Revision; got != aRevision {
		t.Fatalf("vendor/a revision = %q, want %q", got, aRevision)
	}
	if got := loadMirror(t, repo, "vendor/b").Revision; got != bRevision {
		t.Fatalf("vendor/b revision = %q, want %q", got, bRevision)
	}
	if got := loadMirror(t, repo, "vendor/locked").Revision; got != lockedRevision {
		t.Fatalf("vendor/locked revision = %q, want %q", got, lockedRevision)
	}
	assertFile(t, repo, "vendor/locked/README.md", "dirty locked\n")

	subjects := strings.Split(strings.TrimSpace(testutil.Git(t, repo, "log", "-2", "--pretty=%s").Stdout), "\n")
	if len(subjects) != 2 || !strings.Contains(subjects[0], "vendor/b") || !strings.Contains(subjects[1], "vendor/a") {
		t.Fatalf("last sync update subjects = %#v, want newest vendor/b then vendor/a", subjects)
	}
}

func TestSyncCommandNoPathQuietWhenNoLockedMirrors(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	for _, args := range [][]string{
		{"sync"},
		{"sync", "--pull-only"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			repo := initDownstream(t)
			runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

			out := runCommandOK(t, repo, args)
			if out != "" {
				t.Fatalf("sync stdout = %q, want quiet output", out)
			}
		})
	}
}

func TestSyncCommandAllLockedNoPathReportsSkippedMirrors(t *testing.T) {
	for _, args := range [][]string{
		{"sync"},
		{"sync", "--pull-only"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			repo := initDownstream(t)
			writeLockedMirrorConfig(t, repo, "vendor/a", "vendor/z")

			out := runCommandOK(t, repo, args)
			if want := skippedLockedOutput("vendor/a", "vendor/z"); out != want {
				t.Fatalf("sync stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestSyncCommandExplicitTargetsPreserveOrderAndRejectDuplicates(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})

	duplicateErr := runCommandError(t, repo, []string{"sync", "vendor/a", "./vendor/a"})
	assertContains(t, duplicateErr, "duplicate sync path: vendor/a")
	missingErr := runCommandError(t, repo, []string{"sync", "vendor/missing"})
	assertContains(t, missingErr, "mirror does not exist: vendor/missing")

	testutil.WriteFile(t, upstreamA, "README.md", "a remote\n")
	testutil.CommitAll(t, upstreamA, "a remote")
	testutil.WriteFile(t, upstreamB, "README.md", "b remote\n")
	testutil.CommitAll(t, upstreamB, "b remote")
	runCommandOK(t, repo, []string{"sync", "--pull-only", "vendor/b", "vendor/a"})

	subjects := strings.Split(strings.TrimSpace(testutil.Git(t, repo, "log", "-2", "--pretty=%s").Stdout), "\n")
	if len(subjects) != 2 || !strings.Contains(subjects[0], "vendor/a") || !strings.Contains(subjects[1], "vendor/b") {
		t.Fatalf("last sync update subjects = %#v, want explicit order vendor/b then vendor/a", subjects)
	}
}

func TestSyncCommandTargetValidationAndScopedPrecheckErrors(t *testing.T) {
	t.Run("mirror path overlaps config", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		revision := testutil.CommitAll(t, upstream, "base")
		repo := testutil.InitRepo(t)
		cfg := config.Empty()
		if err := cfg.Add(mirror.Mirror{Path: ".braids.json", URL: upstream, Branch: "main", Revision: revision}); err != nil {
			t.Fatalf("add mirror config: %v", err)
		}
		if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
			t.Fatalf("write config: %v", err)
		}
		testutil.Git(t, repo, "add", config.FileName)
		testutil.Git(t, repo, "commit", "-m", "config")

		stderr := runCommandError(t, repo, []string{"sync", ".braids.json"})

		assertContains(t, stderr, `mirror path ".braids.json" overlaps .braids.json`)
	})

	t.Run("dirty config", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		testutil.CommitAll(t, upstream, "base")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
		path := filepath.Join(repo, config.FileName)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if err := os.WriteFile(path, append(data, []byte(" \n")...), 0o644); err != nil {
			t.Fatalf("dirty config: %v", err)
		}

		stderr := runCommandError(t, repo, []string{"sync", "vendor/basic"})

		assertContains(t, stderr, "local changes are present in .braids.json")
	})

	t.Run("pull only dirty target", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		testutil.CommitAll(t, upstream, "base")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
		testutil.WriteFile(t, repo, "vendor/basic/README.md", "dirty\n")

		stderr := runCommandError(t, repo, []string{"sync", "--pull-only", "vendor/basic"})

		assertContains(t, stderr, "local changes are present in vendor/basic")
	})
}

func TestSyncCommandScopedPrecheckRunsBeforeSideEffects(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	aBase := testutil.CommitAll(t, upstreamA, "a base")
	testutil.Git(t, upstreamA, "config", "receive.denyCurrentBranch", "updateInstead")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})
	testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
	testutil.CommitAll(t, repo, "local a")
	testutil.WriteFile(t, repo, "vendor/b/README.md", "dirty b\n")
	t.Setenv("GIT_EDITOR", writeFailingEditor(t))

	stderr := runCommandError(t, repo, []string{"sync", "vendor/a", "vendor/b"})

	assertContains(t, stderr, "local changes are present in vendor/b")
	if got := testutil.CurrentRevision(t, upstreamA); got != aBase {
		t.Fatalf("upstream a revision = %q, want unchanged %q", got, aBase)
	}
	assertNoGitRemote(t, repo, "main_braid_vendor_a")
}

func TestSyncCommandNoPathScopedPrecheckRunsBeforeSideEffects(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	aBase := testutil.CommitAll(t, upstreamA, "a base")
	testutil.Git(t, upstreamA, "config", "receive.denyCurrentBranch", "updateInstead")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	bBase := testutil.CommitAll(t, upstreamB, "b base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})
	testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
	testutil.CommitAll(t, repo, "local a")
	testutil.WriteFile(t, repo, "vendor/b/README.md", "dirty b\n")
	t.Setenv("GIT_EDITOR", writeFailingEditor(t))

	stderr := runCommandError(t, repo, []string{"sync"})

	assertContains(t, stderr, "local changes are present in vendor/b")
	if got := testutil.CurrentRevision(t, upstreamA); got != aBase {
		t.Fatalf("upstream a revision = %q, want unchanged %q", got, aBase)
	}
	if got := loadMirror(t, repo, "vendor/b").Revision; got != bBase {
		t.Fatalf("vendor/b revision = %q, want unchanged %q", got, bBase)
	}
	assertNoGitRemote(t, repo, "main_braid_vendor_a")
}

func TestSyncCommandNoPathSuppressesSkippedMirrorOutputOnError(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamLocked := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamLocked, "README.md", "locked base\n")
	lockedRevision := testutil.CommitAll(t, upstreamLocked, "locked base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamLocked, "vendor/locked", "--revision", lockedRevision})
	testutil.WriteFile(t, repo, "vendor/a/README.md", "dirty a\n")

	stdout, stderr := runCommandErrorWithOutput(t, repo, []string{"sync"})
	assertContains(t, stderr, "local changes are present in vendor/a")
	if stdout != "" {
		t.Fatalf("sync stdout = %q, want quiet output on error", stdout)
	}
}

func TestSyncCommandExplicitPrecheckIgnoresDirtyNonSelectedMirror(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})
	testutil.WriteFile(t, repo, "vendor/b/README.md", "dirty b\n")

	runCommandOK(t, repo, []string{"sync", "--pull-only", "vendor/a"})

	assertFile(t, repo, "vendor/b/README.md", "dirty b\n")
}

func TestSyncCommandPushPlanValidationPreventsEarlierPush(t *testing.T) {
	t.Run("later non branch local change", func(t *testing.T) {
		upstreamA := testutil.InitRepo(t)
		testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
		aBase := testutil.CommitAll(t, upstreamA, "a base")
		testutil.Git(t, upstreamA, "config", "receive.denyCurrentBranch", "updateInstead")
		upstreamB := testutil.InitRepo(t)
		testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
		testutil.CommitAll(t, upstreamB, "b base")
		testutil.Git(t, upstreamB, "tag", "v1")

		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
		runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b", "--tag", "v1"})
		testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
		testutil.WriteFile(t, repo, "vendor/b/README.md", "b local\n")
		testutil.CommitAll(t, repo, "local mirror changes")
		t.Setenv("GIT_EDITOR", writeFailingEditor(t))

		stderr := runCommandError(t, repo, []string{"sync", "vendor/a", "vendor/b"})

		assertContains(t, stderr, "sync cannot push committed local changes for non-branch mirror vendor/b")
		assertContains(t, stderr, "braid push vendor/b --branch <branch>")
		if got := testutil.CurrentRevision(t, upstreamA); got != aBase {
			t.Fatalf("upstream a revision = %q, want unchanged %q", got, aBase)
		}
	})

	t.Run("later stale branch", func(t *testing.T) {
		upstreamA := testutil.InitRepo(t)
		testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
		aBase := testutil.CommitAll(t, upstreamA, "a base")
		testutil.Git(t, upstreamA, "config", "receive.denyCurrentBranch", "updateInstead")
		upstreamB := testutil.InitRepo(t)
		testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
		testutil.CommitAll(t, upstreamB, "b base")
		testutil.Git(t, upstreamB, "config", "receive.denyCurrentBranch", "updateInstead")

		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
		runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})
		testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
		testutil.WriteFile(t, repo, "vendor/b/README.md", "b local\n")
		testutil.CommitAll(t, repo, "local mirror changes")
		testutil.WriteFile(t, upstreamB, "README.md", "b remote\n")
		testutil.CommitAll(t, upstreamB, "b remote")
		t.Setenv("GIT_EDITOR", writeFailingEditor(t))

		stderr := runCommandError(t, repo, []string{"sync", "vendor/a", "vendor/b"})

		assertContains(t, stderr, "sync cannot push vendor/b because the upstream branch is not up to date")
		assertContains(t, stderr, "run braid update vendor/b")
		if got := testutil.CurrentRevision(t, upstreamA); got != aBase {
			t.Fatalf("upstream a revision = %q, want unchanged %q", got, aBase)
		}
	})
}

func TestSyncCommandStopsBeforePullPhaseWhenPushFails(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	aBase := testutil.CommitAll(t, upstreamA, "a base")
	testutil.Git(t, upstreamA, "config", "receive.denyCurrentBranch", "updateInstead")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	bBase := testutil.CommitAll(t, upstreamB, "b base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})
	testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
	testutil.CommitAll(t, repo, "local a")
	testutil.WriteFile(t, upstreamB, "README.md", "b remote\n")
	testutil.CommitAll(t, upstreamB, "b remote")
	t.Setenv("GIT_EDITOR", writeFailingEditor(t))

	runCommandError(t, repo, []string{"sync", "vendor/a", "vendor/b"})

	if got := testutil.CurrentRevision(t, upstreamA); got != aBase {
		t.Fatalf("upstream a revision = %q, want unchanged %q", got, aBase)
	}
	if got := loadMirror(t, repo, "vendor/b").Revision; got != bBase {
		t.Fatalf("vendor/b revision = %q, want unchanged %q", got, bBase)
	}
	assertFile(t, repo, "vendor/b/README.md", "b base\n")
}

func TestSyncCommandUnchangedMovedBranchUpdatesNormally(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, upstream, "README.md", "remote\n")
	remoteRevision := testutil.CommitAll(t, upstream, "remote")
	t.Setenv("GIT_EDITOR", writeFailingEditor(t))

	runCommandOK(t, repo, []string{"sync", "vendor/basic"})

	assertFile(t, repo, "vendor/basic/README.md", "remote\n")
	if got := loadMirror(t, repo, "vendor/basic").Revision; got != remoteRevision {
		t.Fatalf("revision = %q, want %q", got, remoteRevision)
	}
}

func TestSyncCommandRejectsSelectedNonBranchLocalChanges(t *testing.T) {
	t.Run("no path tag mirror", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		testutil.CommitAll(t, upstream, "base")
		testutil.Git(t, upstream, "tag", "v1")

		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/tagged", "--tag", "v1"})
		testutil.WriteFile(t, repo, "vendor/tagged/README.md", "local\n")
		testutil.CommitAll(t, repo, "local tag change")

		stderr := runCommandError(t, repo, []string{"sync"})

		assertContains(t, stderr, "sync cannot push committed local changes for non-branch mirror vendor/tagged")
	})

	t.Run("explicit revision mirror", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		revision := testutil.CommitAll(t, upstream, "base")

		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/revision", "--revision", revision})
		testutil.WriteFile(t, repo, "vendor/revision/README.md", "local\n")
		testutil.CommitAll(t, repo, "local revision change")

		stderr := runCommandError(t, repo, []string{"sync", "vendor/revision"})

		assertContains(t, stderr, "sync cannot push committed local changes for non-branch mirror vendor/revision")
		assertContains(t, stderr, "braid sync --pull-only vendor/revision")
	})
}

func TestSyncCommandRejectsDeletedSelectedMirrorPath(t *testing.T) {
	tests := []struct {
		name      string
		addArgs   func(string) []string
		localPath string
		remove    string
	}{
		{
			name:      "directory",
			addArgs:   func(upstream string) []string { return []string{"add", upstream, "vendor/basic"} },
			localPath: "vendor/basic",
			remove:    "vendor/basic",
		},
		{
			name: "single file",
			addArgs: func(upstream string) []string {
				return []string{"add", upstream, "licenses/THIRD_PARTY.txt", "--path", "LICENSE.txt"}
			},
			localPath: "licenses/THIRD_PARTY.txt",
			remove:    "licenses/THIRD_PARTY.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			testutil.WriteFile(t, upstream, "README.md", "base\n")
			testutil.WriteFile(t, upstream, "LICENSE.txt", "license\n")
			testutil.CommitAll(t, upstream, "base")
			repo := initDownstream(t)
			runCommandOK(t, repo, test.addArgs(upstream))
			if err := os.RemoveAll(filepath.Join(repo, filepath.FromSlash(test.remove))); err != nil {
				t.Fatalf("remove mirror path: %v", err)
			}
			testutil.Git(t, repo, "add", "-A")
			testutil.Git(t, repo, "commit", "-m", "delete mirror path")

			stderr := runCommandError(t, repo, []string{"sync", test.localPath})

			assertContains(t, stderr, "sync cannot push deletion of mirror path "+test.localPath)
		})
	}
}

func TestSyncCommandRemotePathAwareClassification(t *testing.T) {
	t.Run("changed subdirectory pushes remote subdirectory", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "lib/component.txt", "base\n")
		testutil.WriteFile(t, upstream, "outside.txt", "outside\n")
		testutil.CommitAll(t, upstream, "base")
		testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/lib", "--path", "lib"})
		testutil.WriteFile(t, repo, "vendor/lib/component.txt", "local\n")
		testutil.CommitAll(t, repo, "local subdir")
		t.Setenv("GIT_EDITOR", writeEditor(t, "Sync subdir"))

		runCommandOK(t, repo, []string{"sync", "vendor/lib"})

		assertFile(t, upstream, "lib/component.txt", "local\n")
		assertFile(t, upstream, "outside.txt", "outside\n")
	})

	t.Run("unchanged subdirectory ignores unrelated upstream movement", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "lib/component.txt", "base\n")
		testutil.WriteFile(t, upstream, "outside.txt", "outside\n")
		testutil.CommitAll(t, upstream, "base")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/lib", "--path", "lib"})
		testutil.WriteFile(t, upstream, "outside.txt", "remote outside\n")
		next := testutil.CommitAll(t, upstream, "remote outside")
		t.Setenv("GIT_EDITOR", writeFailingEditor(t))

		runCommandOK(t, repo, []string{"sync", "vendor/lib"})

		assertFile(t, repo, "vendor/lib/component.txt", "base\n")
		if got := loadMirror(t, repo, "vendor/lib").Revision; got != next {
			t.Fatalf("revision = %q, want %q", got, next)
		}
	})

	t.Run("changed single file pushes remote file", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "LICENSE.txt", "base\n")
		testutil.WriteFile(t, upstream, "README.md", "readme\n")
		testutil.CommitAll(t, upstream, "base")
		testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "licenses/THIRD_PARTY.txt", "--path", "LICENSE.txt"})
		testutil.WriteFile(t, repo, "licenses/THIRD_PARTY.txt", "local license\n")
		testutil.CommitAll(t, repo, "local license")
		t.Setenv("GIT_EDITOR", writeEditor(t, "Sync license"))

		runCommandOK(t, repo, []string{"sync", "licenses/THIRD_PARTY.txt"})

		assertFile(t, upstream, "LICENSE.txt", "local license\n")
		assertFile(t, upstream, "README.md", "readme\n")
	})

	t.Run("unchanged single file ignores unrelated upstream movement", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "LICENSE.txt", "base\n")
		testutil.WriteFile(t, upstream, "README.md", "readme\n")
		testutil.CommitAll(t, upstream, "base")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "licenses/THIRD_PARTY.txt", "--path", "LICENSE.txt"})
		testutil.WriteFile(t, upstream, "README.md", "remote readme\n")
		next := testutil.CommitAll(t, upstream, "remote readme")
		t.Setenv("GIT_EDITOR", writeFailingEditor(t))

		runCommandOK(t, repo, []string{"sync", "licenses/THIRD_PARTY.txt"})

		assertFile(t, repo, "licenses/THIRD_PARTY.txt", "base\n")
		if got := loadMirror(t, repo, "licenses/THIRD_PARTY.txt").Revision; got != next {
			t.Fatalf("revision = %q, want %q", got, next)
		}
	})
}

func TestSyncCommandHydratesMissingRecordedRevision(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	parent := t.TempDir()
	clone := filepath.Join(parent, "clone")
	testutil.Git(t, parent, "clone", "--no-local", repo, clone)
	if result, err := gitexec.New(clone, false, nil).RunOK(context.Background(), "rev-parse", "--verify", "--quiet", revision+"^{commit}"); err == nil {
		t.Fatalf("base revision unexpectedly present in clone: %s", result.Stdout)
	}
	t.Setenv("GIT_EDITOR", writeFailingEditor(t))

	runCommandOK(t, clone, []string{"sync", "vendor/basic"})

	if _, err := gitexec.New(clone, false, nil).RunOK(context.Background(), "rev-parse", "--verify", "--quiet", revision+"^{commit}"); err != nil {
		t.Fatalf("base revision was not hydrated: %v", err)
	}
}

func TestSyncCommandReportsUnavailableRecordedRevisionAfterHydration(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	cfg := config.Empty()
	bogusRevision := strings.Repeat("0", 40)
	if err := cfg.Add(mirror.Mirror{Path: "vendor/basic", URL: upstream, Branch: "main", Revision: bogusRevision}); err != nil {
		t.Fatalf("add mirror config: %v", err)
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("write config: %v", err)
	}
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "base\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-m", "configure broken mirror")

	stderr := runCommandError(t, repo, []string{"sync", "vendor/basic"})

	assertContains(t, stderr, "recorded revision "+bogusRevision+" for vendor/basic is unavailable after hydration")
}

func TestSyncCommandKeepRetainsTemporaryRemote(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	runCommandOK(t, repo, []string{"sync", "--pull-only", "--keep", "vendor/basic"})

	assertGitRemote(t, repo, "main_braid_vendor_basic")
}

func assertNoGitRemote(t *testing.T, repo, remote string) {
	t.Helper()
	remotes := strings.Fields(testutil.Git(t, repo, "remote").Stdout)
	for _, got := range remotes {
		if got == remote {
			t.Fatalf("remote %q exists unexpectedly in %#v", remote, remotes)
		}
	}
}

func assertGitRemote(t *testing.T, repo, remote string) {
	t.Helper()
	remotes := strings.Fields(testutil.Git(t, repo, "remote").Stdout)
	for _, got := range remotes {
		if got == remote {
			return
		}
	}
	t.Fatalf("remote %q missing from %#v", remote, remotes)
}
