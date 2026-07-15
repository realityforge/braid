package command

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/config"
	"braid/internal/gitexec"
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
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic", "--sync-push"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Sync push"))
	git := forbidMergeTreeGit(t, repo)

	out := runCommandOKInDirWithOptions(t, repo, repo, Options{Git: git}, []string{"sync", "vendor/basic"})

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
	assertCommitSubject(t, repo, "Braid: Update source '001' to '"+pushedRevision[:7]+"'")
	assertNoGitRemote(t, repo, "main_braid_001")
}

func TestSyncCommandPreservesIgnoredMirrorFilesWithoutAutostash(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, ".gitignore", "user.bazelrc\n")
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/lib/replicant"})
	testutil.WriteFile(t, repo, "vendor/lib/replicant/user.bazelrc", "local config\n")
	testutil.WriteFile(t, upstream, "README.md", "updated\n")
	revision := testutil.CommitAll(t, upstream, "updated")

	runCommandOK(t, repo, []string{"sync", "--pull-only", "vendor/lib/replicant"})

	assertFile(t, repo, "vendor/lib/replicant/README.md", "updated\n")
	assertFile(t, repo, "vendor/lib/replicant/user.bazelrc", "local config\n")
	if got := loadMirror(t, repo, "vendor/lib/replicant").Revision; got != revision {
		t.Fatalf("mirror revision = %q, want %q", got, revision)
	}
}

func TestSyncCommandPushesOnlyOptedInSources(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	testutil.Git(t, upstreamA, "config", "receive.denyCurrentBranch", "updateInstead")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")
	testutil.Git(t, upstreamB, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a", "--sync-push"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})
	testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
	testutil.WriteFile(t, repo, "vendor/b/README.md", "b local\n")
	testutil.CommitAll(t, repo, "local mirror changes")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Sync opted in source"))

	runCommandOK(t, repo, []string{"sync", "vendor/a", "vendor/b"})

	assertFile(t, upstreamA, "README.md", "a local\n")
	assertCommitSubject(t, upstreamA, "Sync opted in source")
	assertFile(t, upstreamB, "README.md", "b base\n")
}

func TestSyncCommandProvenanceGuidanceIsPerPushedMirror(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	testutil.Git(t, upstreamA, "config", "receive.denyCurrentBranch", "updateInstead")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")
	testutil.Git(t, upstreamB, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a", "--sync-push"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b", "--sync-push"})
	testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
	aCommit := commitAllWithMessage(t, repo, "local a sync")
	testutil.WriteFile(t, repo, "vendor/b/README.md", "b local\n")
	bCommit := commitAllWithMessage(t, repo, "local b sync")
	captureDir, editor := writeSequenceCapturingEditor(t, "Sync push")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"sync", "vendor/a", "vendor/b"})

	first := readTestFile(t, filepath.Join(captureDir, "template-1.txt"))
	second := readTestFile(t, filepath.Join(captureDir, "template-2.txt"))
	assertContains(t, first, "# Braid downstream mirror commit guidance for vendor/a")
	assertContains(t, first, "# Commit "+aCommit)
	assertContains(t, first, "# local a sync")
	assertNotContains(t, first, bCommit)
	assertNotContains(t, first, "local b sync")
	assertContains(t, second, "# Braid downstream mirror commit guidance for vendor/b")
	assertContains(t, second, "# Commit "+bCommit)
	assertContains(t, second, "# local b sync")
	assertNotContains(t, second, aCommit)
	assertNotContains(t, second, "local a sync")
	assertFile(t, upstreamA, "README.md", "a local\n")
	assertFile(t, upstreamB, "README.md", "b local\n")
}

func TestSyncCommandGeneratedMessagesArePerPushedMirror(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	testutil.Git(t, upstreamA, "config", "receive.denyCurrentBranch", "updateInstead")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")
	testutil.Git(t, upstreamB, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a", "--sync-push"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b", "--sync-push"})
	testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
	testutil.CommitAll(t, repo, "local a generated")
	testutil.WriteFile(t, repo, "vendor/b/README.md", "b local\n")
	testutil.CommitAll(t, repo, "local b generated")
	generator := writeGenerator(t, "#!/bin/sh\nprompt=$1\nmessage=$2\nif grep -q 'Source name: 001' \"$prompt\"; then\n  printf 'Generated for vendor/a\\n' > \"$message\"\nelif grep -q 'Source name: 002' \"$prompt\"; then\n  printf 'Generated for vendor/b\\n' > \"$message\"\nelse\n  exit 18\nfi\n")
	t.Setenv(pushMessageCommandEnv, shellQuote(generator)+" {PROMPT_FILE} {MESSAGE_FILE}")
	captureDir, editor := writeSequenceCapturingEditor(t, "Sync reviewed")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"sync", "vendor/a", "vendor/b"})

	first := readTestFile(t, filepath.Join(captureDir, "template-1.txt"))
	second := readTestFile(t, filepath.Join(captureDir, "template-2.txt"))
	assertContains(t, first, "Generated for vendor/a")
	assertNotContains(t, first, "Generated for vendor/b")
	assertContains(t, second, "Generated for vendor/b")
	assertNotContains(t, second, "Generated for vendor/a")
	assertFile(t, upstreamA, "README.md", "a local\n")
	assertFile(t, upstreamB, "README.md", "b local\n")
	assertCommitSubject(t, upstreamA, "Sync reviewed 1")
	assertCommitSubject(t, upstreamB, "Sync reviewed 2")
}

func TestSyncCommandDoesNotRunGeneratorWhenPullOnlyOrNoPushAction(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "pull only", args: []string{"sync", "--pull-only", "vendor/basic"}},
		{name: "unchanged push phase", args: []string{"sync", "vendor/basic"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			testutil.WriteFile(t, upstream, "README.md", "base\n")
			testutil.CommitAll(t, upstream, "base")
			repo := initDownstream(t)
			runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
			testutil.WriteFile(t, upstream, "README.md", test.name+" remote\n")
			remoteRevision := testutil.CommitAll(t, upstream, test.name+" remote")
			marker := filepath.Join(t.TempDir(), "generator-ran")
			generator := writeGenerator(t, "#!/bin/sh\nprintf ran > \"$BRAID_GENERATOR_MARKER\"\nexit 99\n")
			t.Setenv("BRAID_GENERATOR_MARKER", marker)
			t.Setenv(pushMessageCommandEnv, shellQuote(generator)+" {PROMPT_FILE} {MESSAGE_FILE}")
			t.Setenv("GIT_EDITOR", writeFailingEditor(t))

			runCommandOK(t, repo, test.args)

			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("generator marker %s exists, want generator not run", marker)
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat generator marker: %v", err)
			}
			assertFile(t, repo, "vendor/basic/README.md", test.name+" remote\n")
			if got := loadMirror(t, repo, "vendor/basic").Revision; got != remoteRevision {
				t.Fatalf("revision = %q, want %q", got, remoteRevision)
			}
		})
	}
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
	if out != "Braid: source :001 is already up to date\n" {
		t.Fatalf("sync stdout = %q, want source no-op result", out)
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
	if want := skippedLockedOutput("003"); out != want {
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
	if len(subjects) != 2 || !strings.Contains(subjects[0], "'002'") || !strings.Contains(subjects[1], "'001'") {
		t.Fatalf("last sync update subjects = %#v, want newest source 002 then source 001", subjects)
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
			if out != "Braid: source :001 is already up to date\n" {
				t.Fatalf("sync stdout = %q, want source no-op result", out)
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
			if want := skippedLockedOutput("vendor-a", "vendor-z"); out != want {
				t.Fatalf("sync stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestSyncCommandExplicitTargetsDeduplicateAndSortSources(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})

	runCommandOK(t, repo, []string{"sync", "vendor/a", "./vendor/a"})
	missingErr := runCommandError(t, repo, []string{"sync", "vendor/missing"})
	assertContains(t, missingErr, "mirror does not exist: vendor/missing")

	testutil.WriteFile(t, upstreamA, "README.md", "a remote\n")
	testutil.CommitAll(t, upstreamA, "a remote")
	testutil.WriteFile(t, upstreamB, "README.md", "b remote\n")
	testutil.CommitAll(t, upstreamB, "b remote")
	runCommandOK(t, repo, []string{"sync", "--pull-only", "vendor/b", "vendor/a"})

	subjects := strings.Split(strings.TrimSpace(testutil.Git(t, repo, "log", "-2", "--pretty=%s").Stdout), "\n")
	if len(subjects) != 2 || !strings.Contains(subjects[0], "'002'") || !strings.Contains(subjects[1], "'001'") {
		t.Fatalf("last sync update subjects = %#v, want source-name order 001 then 002", subjects)
	}
}

func TestSyncCommandTargetValidationAndScopedPrecheckErrors(t *testing.T) {
	t.Run("mirror path overlaps config", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		revision := testutil.CommitAll(t, upstream, "base")
		repo := testutil.InitRepo(t)
		cfg := config.Empty()
		if err := cfg.AddSource(testSourceMirror(".braids.json", "", upstream, "main", "", revision, false).Source); err != nil {
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

	t.Run("dirty config with autostash", func(t *testing.T) {
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
		testutil.WriteFile(t, repo, "vendor/basic/README.md", "dirty\n")

		stderr := runCommandError(t, repo, []string{"sync", "--autostash", "vendor/basic"})

		assertContains(t, stderr, "local changes are present in .braids.json")
		assertFile(t, repo, "vendor/basic/README.md", "dirty\n")
		if stashList := strings.TrimSpace(testutil.Git(t, repo, "stash", "list").Stdout); stashList != "" {
			t.Fatalf("stash list = %q, want no autostash before config blocker", stashList)
		}
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

	t.Run("unresolved operation with autostash", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		testutil.CommitAll(t, upstream, "base")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
		testutil.WriteFile(t, repo, "vendor/basic/README.md", "dirty\n")
		if err := os.WriteFile(filepath.Join(repo, ".git", "MERGE_HEAD"), []byte("abc123\n"), 0o644); err != nil {
			t.Fatalf("write MERGE_HEAD: %v", err)
		}

		stderr := runCommandError(t, repo, []string{"sync", "--autostash", "vendor/basic"})

		assertContains(t, stderr, "unresolved git operation state is present: MERGE_HEAD")
		assertFile(t, repo, "vendor/basic/README.md", "dirty\n")
		if stashList := strings.TrimSpace(testutil.Git(t, repo, "stash", "list").Stdout); stashList != "" {
			t.Fatalf("stash list = %q, want no autostash before operation blocker", stashList)
		}
	})
}

func TestSyncCommandAutostashRestoresSelectedStateAndPreservesUnrelatedState(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, ".gitignore", "vendor/basic/ignored.log\noutside-ignored.log\n")
	testutil.WriteFile(t, repo, "outside.txt", "outside base\n")
	testutil.Git(t, repo, "add", ".gitignore", "outside.txt")
	testutil.Git(t, repo, "commit", "-m", "add ignore and outside file")

	testutil.WriteFile(t, repo, "vendor/basic/README.md", "selected staged\n")
	testutil.Git(t, repo, "add", "vendor/basic/README.md")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "selected unstaged\n")
	testutil.WriteFile(t, repo, "vendor/basic/new.txt", "new\n")
	testutil.WriteFile(t, repo, "vendor/basic/ignored.log", "ignored\n")
	testutil.WriteFile(t, repo, "outside.txt", "outside staged\n")
	testutil.Git(t, repo, "add", "outside.txt")
	testutil.WriteFile(t, repo, "outside.txt", "outside unstaged\n")
	testutil.WriteFile(t, repo, "outside-ignored.log", "outside ignored\n")
	beforeOutsideStatus := testutil.Git(t, repo, "status", "--porcelain", "--ignored", "--", "outside.txt", "outside-ignored.log").Stdout

	testutil.WriteFile(t, upstream, "remote.txt", "remote\n")
	remoteRevision := testutil.CommitAll(t, upstream, "remote")

	runCommandOK(t, repo, []string{"sync", "--pull-only", "--autostash", "vendor/basic"})

	assertFile(t, repo, "vendor/basic/remote.txt", "remote\n")
	if got := loadMirror(t, repo, "vendor/basic").Revision; got != remoteRevision {
		t.Fatalf("revision = %q, want %q", got, remoteRevision)
	}
	if got := strings.TrimSpace(testutil.Git(t, repo, "show", ":vendor/basic/README.md").Stdout); got != "selected staged" {
		t.Fatalf("staged selected README = %q, want selected staged", got)
	}
	assertFile(t, repo, "vendor/basic/README.md", "selected unstaged\n")
	assertFile(t, repo, "vendor/basic/new.txt", "new\n")
	assertFile(t, repo, "vendor/basic/ignored.log", "ignored\n")
	afterSelectedStatus := testutil.Git(t, repo, "status", "--porcelain", "--ignored", "--", "vendor/basic").Stdout
	for _, want := range []string{"MM vendor/basic/README.md", "?? vendor/basic/new.txt", "!! vendor/basic/ignored.log"} {
		assertContains(t, afterSelectedStatus, want)
	}
	afterOutsideStatus := testutil.Git(t, repo, "status", "--porcelain", "--ignored", "--", "outside.txt", "outside-ignored.log").Stdout
	if afterOutsideStatus != beforeOutsideStatus {
		t.Fatalf("outside status changed from %q to %q", beforeOutsideStatus, afterOutsideStatus)
	}
	if stashList := strings.TrimSpace(testutil.Git(t, repo, "stash", "list").Stdout); stashList != "" {
		t.Fatalf("stash list = %q, want autostash dropped", stashList)
	}
}

func TestSyncCommandPlainAndAutostashPreserveIgnoredOnlyState(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, ".gitignore", "vendor/basic/ignored.log\n")
	testutil.Git(t, repo, "add", ".gitignore")
	testutil.Git(t, repo, "commit", "-m", "ignore mirror log")
	testutil.WriteFile(t, repo, "vendor/basic/ignored.log", "ignored\n")
	testutil.WriteFile(t, upstream, "remote.txt", "remote\n")
	testutil.CommitAll(t, upstream, "remote")

	runCommandOK(t, repo, []string{"sync", "--pull-only", "vendor/basic"})

	assertFile(t, repo, "vendor/basic/remote.txt", "remote\n")
	assertFile(t, repo, "vendor/basic/ignored.log", "ignored\n")
	if stashList := strings.TrimSpace(testutil.Git(t, repo, "stash", "list").Stdout); stashList != "" {
		t.Fatalf("stash list = %q, want no autostash for plain sync", stashList)
	}

	testutil.WriteFile(t, upstream, "another.txt", "another\n")
	testutil.CommitAll(t, upstream, "another")
	runCommandOK(t, repo, []string{"sync", "--pull-only", "--autostash", "vendor/basic"})

	assertFile(t, repo, "vendor/basic/ignored.log", "ignored\n")
	if stashList := strings.TrimSpace(testutil.Git(t, repo, "stash", "list").Stdout); stashList != "" {
		t.Fatalf("stash list = %q, want autostash dropped", stashList)
	}
}

func TestSyncCommandAutostashRestoresAfterOperationalFailure(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	cfg := config.Empty()
	bogusRevision := strings.Repeat("0", 40)
	if err := cfg.AddSource(testSourceMirror("vendor/basic", "", upstream, "main", "", bogusRevision, false).Source); err != nil {
		t.Fatalf("add mirror config: %v", err)
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("write config: %v", err)
	}
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "base\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-m", "configure broken mirror")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "dirty\n")

	stderr := runCommandError(t, repo, []string{"sync", "--autostash", "vendor/basic"})

	assertContains(t, stderr, "recorded revision "+bogusRevision+" for source :vendor-basic is unavailable from upstream "+upstream+"; the repository-local cache may have been deleted or the upstream history may have been rewritten")
	assertFile(t, repo, "vendor/basic/README.md", "dirty\n")
	if stashList := strings.TrimSpace(testutil.Git(t, repo, "stash", "list").Stdout); stashList != "" {
		t.Fatalf("stash list = %q, want autostash restored and dropped", stashList)
	}
}

func TestSyncCommandAutostashUpdateConflictLeavesStash(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local committed\n")
	testutil.Git(t, repo, "add", "vendor/basic/README.md")
	testutil.Git(t, repo, "commit", "-m", "local mirror change")
	testutil.WriteFile(t, repo, "vendor/basic/note.txt", "saved work\n")
	testutil.WriteFile(t, upstream, "README.md", "remote committed\n")
	testutil.CommitAll(t, upstream, "remote mirror change")

	stdout, stderr := runCommandErrorWithOutput(t, repo, []string{"sync", "--pull-only", "--autostash", "vendor/basic"})

	assertContains(t, stdout, "  vendor/basic/README.md")
	assertContains(t, stderr, "Braid preserved autostash")
	assertContains(t, stderr, "Resolve the Braid pull conflict first")
	assertContains(t, stderr, "git stash apply")
	assertContains(t, stderr, "git restore --source=")
	assertNoFile(t, repo, "vendor/basic/note.txt")
	stashList := testutil.Git(t, repo, "stash", "list").Stdout
	assertContains(t, stashList, "braid sync autostash")
	data, err := os.ReadFile(filepath.Join(repo, "vendor", "basic", "README.md"))
	if err != nil {
		t.Fatalf("read conflicted README: %v", err)
	}
	assertContains(t, string(data), "<<<<<<<")
	assertContains(t, string(data), "local committed")
	assertContains(t, string(data), "remote committed")
}

func TestSyncCommandAutostashUpdateConflictWriteFailureRollsBackAndRestores(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local committed\n")
	testutil.Git(t, repo, "add", "vendor/basic/README.md")
	testutil.Git(t, repo, "commit", "-m", "local mirror change")
	testutil.WriteFile(t, repo, "vendor/basic/note.txt", "saved work\n")
	testutil.WriteFile(t, upstream, "README.md", "remote committed\n")
	testutil.CommitAll(t, upstream, "remote mirror change")
	if err := os.Mkdir(filepath.Join(repo, ".git", "MERGE_MSG"), 0o755); err != nil {
		t.Fatalf("create MERGE_MSG directory: %v", err)
	}

	stdout, stderr := runCommandErrorWithOutput(t, repo, []string{"sync", "--pull-only", "--autostash", "vendor/basic"})

	if stdout != "" {
		t.Fatalf("stdout = %q, want rollback before conflict output", stdout)
	}
	assertContains(t, stderr, "pull vendor/basic:")
	assertContains(t, stderr, "MERGE_MSG")
	assertNotContains(t, stderr, "Braid preserved autostash")
	assertFile(t, repo, "vendor/basic/note.txt", "saved work\n")
	stashList := testutil.Git(t, repo, "stash", "list").Stdout
	assertNotContains(t, stashList, "braid sync autostash")
	data, err := os.ReadFile(filepath.Join(repo, "vendor", "basic", "README.md"))
	if err != nil {
		t.Fatalf("read conflicted README: %v", err)
	}
	if string(data) != "local committed\n" {
		t.Fatalf("README after rollback = %q", data)
	}
}

func TestSyncCommandAutostashRestoreReportsCleanupFailureAfterApply(t *testing.T) {
	git := &fakeSyncAutostashRestoreGit{dropErr: errors.New("drop failed")}
	saved := syncAutostash{
		Entry: gitexec.StashEntry{OID: "abc123", Message: syncAutostashMessage},
		Paths: []string{"vendor/basic"},
	}

	err := SyncHandler{}.restoreSyncAutostash(context.Background(), git, saved)

	if err == nil {
		t.Fatal("restoreSyncAutostash returned nil error for drop failure")
	}
	assertContains(t, err.Error(), "restored saved work from braid sync autostash abc123")
	assertContains(t, err.Error(), "could not remove the stash entry")
	assertContains(t, err.Error(), "The saved stash abc123 remains recoverable")
	if !git.applied || !git.indexRestored || !git.dropAttempted {
		t.Fatalf("restore calls applied=%v indexRestored=%v dropAttempted=%v, want all true", git.applied, git.indexRestored, git.dropAttempted)
	}
	if git.applyOID != saved.Entry.OID || git.restoreOID != saved.Entry.OID || git.dropEntry != saved.Entry {
		t.Fatalf("restore used apply=%q restore=%q drop=%#v, want saved entry %#v", git.applyOID, git.restoreOID, git.dropEntry, saved.Entry)
	}
	if got := strings.Join(git.restorePaths, "\n"); got != "vendor/basic" {
		t.Fatalf("restore paths = %q, want vendor/basic", got)
	}
}

type fakeSyncAutostashRestoreGit struct {
	applyErr      error
	restoreErr    error
	dropErr       error
	applied       bool
	indexRestored bool
	dropAttempted bool
	applyOID      string
	restoreOID    string
	restorePaths  []string
	dropEntry     gitexec.StashEntry
}

func (f *fakeSyncAutostashRestoreGit) StashApply(_ context.Context, oid string) error {
	f.applied = true
	f.applyOID = oid
	return f.applyErr
}

func (f *fakeSyncAutostashRestoreGit) RestoreStashIndexPathspecs(_ context.Context, oid string, paths ...string) error {
	f.indexRestored = true
	f.restoreOID = oid
	f.restorePaths = append([]string(nil), paths...)
	return f.restoreErr
}

func (f *fakeSyncAutostashRestoreGit) DropStashEntry(_ context.Context, entry gitexec.StashEntry) (string, error) {
	f.dropAttempted = true
	f.dropEntry = entry
	return "", f.dropErr
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
	assertNoGitRemote(t, repo, "main_braid_001")
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
	assertNoGitRemote(t, repo, "main_braid_001")
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
		runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a", "--sync-push"})
		runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b", "--sync-push"})
		testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
		testutil.WriteFile(t, repo, "vendor/b/README.md", "b local\n")
		testutil.CommitAll(t, repo, "local mirror changes")
		testutil.WriteFile(t, upstreamB, "README.md", "b remote\n")
		testutil.CommitAll(t, upstreamB, "b remote")
		t.Setenv("GIT_EDITOR", writeFailingEditor(t))

		stderr := runCommandError(t, repo, []string{"sync", "vendor/a", "vendor/b"})

		assertContains(t, stderr, "sync cannot push vendor/b because the upstream branch is not up to date")
		assertContains(t, stderr, "run braid pull vendor/b")
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
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a", "--sync-push"})
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

func TestSyncCommandLaterEditorFailureLeavesEarlierGeneratedPushComplete(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	aBase := testutil.CommitAll(t, upstreamA, "a base")
	testutil.Git(t, upstreamA, "config", "receive.denyCurrentBranch", "updateInstead")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	bBase := testutil.CommitAll(t, upstreamB, "b base")
	testutil.Git(t, upstreamB, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a", "--sync-push"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b", "--sync-push"})
	testutil.WriteFile(t, repo, "vendor/a/README.md", "a local\n")
	testutil.CommitAll(t, repo, "local a partial")
	testutil.WriteFile(t, repo, "vendor/b/README.md", "b local\n")
	testutil.CommitAll(t, repo, "local b partial")
	generator := writeGenerator(t, "#!/bin/sh\nprintf 'Generated partial\\n' > \"$2\"\n")
	t.Setenv(pushMessageCommandEnv, shellQuote(generator)+" {PROMPT_FILE} {MESSAGE_FILE}")
	t.Setenv("GIT_EDITOR", writeSequenceCapturingEditorFailAt(t, "Sync partial", 2))

	runCommandError(t, repo, []string{"sync", "vendor/a", "vendor/b"})

	assertFile(t, upstreamA, "README.md", "a local\n")
	assertCommitSubject(t, upstreamA, "Sync partial 1")
	assertFile(t, upstreamB, "README.md", "b base\n")
	if got := testutil.CurrentRevision(t, upstreamB); got != bBase {
		t.Fatalf("upstream b revision = %q, want unchanged %q", got, bBase)
	}
	if got := loadMirror(t, repo, "vendor/a").Revision; got != aBase {
		t.Fatalf("vendor/a revision = %q, want original %q because update phase was skipped", got, aBase)
	}
	if got := loadMirror(t, repo, "vendor/b").Revision; got != bBase {
		t.Fatalf("vendor/b revision = %q, want original %q", got, bBase)
	}
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

func TestSyncCommandRejectsDeletedSelectedMirrorPath(t *testing.T) {
	tests := []struct {
		name      string
		addArgs   func(string) []string
		localPath string
		remove    string
	}{
		{
			name:      "directory",
			addArgs:   func(upstream string) []string { return []string{"add", upstream, "vendor/basic", "--sync-push"} },
			localPath: "vendor/basic",
			remove:    "vendor/basic",
		},
		{
			name: "single file",
			addArgs: func(upstream string) []string {
				return []string{"add", upstream, "licenses/THIRD_PARTY.txt=LICENSE.txt", "--sync-push"}
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
		runCommandOK(t, repo, []string{"add", upstream, "vendor/lib=lib", "--sync-push"})
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
		runCommandOK(t, repo, []string{"add", upstream, "vendor/lib=lib"})
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
		runCommandOK(t, repo, []string{"add", upstream, "licenses/THIRD_PARTY.txt=LICENSE.txt", "--sync-push"})
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
		runCommandOK(t, repo, []string{"add", upstream, "licenses/THIRD_PARTY.txt=LICENSE.txt"})
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
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic", "--sync-push"})

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
	s := testSourceMirror("vendor/basic", "", upstream, "main", "", bogusRevision, false).Source
	s.SyncPush = true
	if err := cfg.AddSource(s); err != nil {
		t.Fatalf("add mirror config: %v", err)
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("write config: %v", err)
	}
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "base\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-m", "configure broken mirror")

	stderr := runCommandError(t, repo, []string{"sync", "vendor/basic"})

	assertContains(t, stderr, "recorded revision "+bogusRevision+" for source :vendor-basic is unavailable from upstream "+upstream+"; the repository-local cache may have been deleted or the upstream history may have been rewritten")
}

func TestSyncCommandKeepRetainsTemporaryRemote(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	runCommandOK(t, repo, []string{"sync", "--pull-only", "--keep", "vendor/basic"})

	assertGitRemote(t, repo, "main_braid_001")
}

func TestSyncCommandKeepRetainsDistinctTagTrackingRefs(t *testing.T) {
	ctx := context.Background()
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a\n")
	revisionA := testutil.CommitAll(t, upstreamA, "a")
	testutil.Git(t, upstreamA, "tag", "v1")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b\n")
	revisionB := testutil.CommitAll(t, upstreamB, "b")
	testutil.Git(t, upstreamB, "tag", "v1")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a", "--tag", "v1"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b", "--tag", "v1"})

	runCommandOK(t, repo, []string{"sync", "--pull-only", "--keep", "vendor/a", "vendor/b"})

	git := gitexec.New(repo, false, nil)
	assertRefCommit(t, ctx, git, "refs/remotes/v1_braid_001/tags/v1", revisionA)
	assertRefCommit(t, ctx, git, "refs/remotes/v1_braid_002/tags/v1", revisionB)
	assertRefMissing(t, ctx, git, "refs/tags/v1")
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
