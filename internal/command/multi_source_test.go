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

func TestMultiMirrorSourceLifecycle(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "LICENSE", "one\n")
	testutil.WriteFile(t, upstream, "src/code.txt", "one\n")
	first := testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "--name", "replicant", "vendor/replicant", "licenses/replicant-LICENSE=LICENSE"})
	assertFile(t, repo, "vendor/replicant/LICENSE", "one\n")
	assertFile(t, repo, "licenses/replicant-LICENSE", "one\n")
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := cfg.SourceByName("replicant")
	if !ok || s.Revision != first || len(s.Mirrors) != 2 {
		t.Fatalf("source=%#v", s)
	}
	statusOut, statusProgress := runCommandOKWithOutput(t, repo, []string{"status", ":replicant"})
	assertContains(t, statusOut, "licenses/replicant-LICENSE")
	assertContains(t, statusOut, "vendor/replicant")
	if got := strings.Count(statusProgress, "Braid: fetched source :replicant"); got != 1 {
		t.Fatalf("status fetched source %d times, want once:\n%s", got, statusProgress)
	}
	testutil.WriteFile(t, repo, "licenses/replicant-LICENSE", "local license\n")
	testutil.WriteFile(t, repo, "vendor/replicant/src/code.txt", "local code\n")
	pathDiff := runCommandOK(t, repo, []string{"diff", "licenses/replicant-LICENSE"})
	assertContains(t, pathDiff, "local license")
	assertNotContains(t, pathDiff, "local code")
	sourceDiff := runCommandOK(t, repo, []string{"diff", ":replicant"})
	assertContains(t, sourceDiff, "local license")
	assertContains(t, sourceDiff, "local code")
	testutil.Git(t, repo, "restore", "licenses/replicant-LICENSE", "vendor/replicant/src/code.txt")
	testutil.WriteFile(t, upstream, "LICENSE", "two\n")
	testutil.WriteFile(t, upstream, "src/code.txt", "two\n")
	testutil.CommitAll(t, upstream, "update")
	runCommandOK(t, repo, []string{"pull", ":replicant"})
	assertFile(t, repo, "vendor/replicant/LICENSE", "two\n")
	assertFile(t, repo, "licenses/replicant-LICENSE", "two\n")
	runCommandOK(t, repo, []string{"remove", "licenses/replicant-LICENSE"})
	cfg, err = config.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	s, _ = cfg.SourceByName("replicant")
	if len(s.Mirrors) != 1 {
		t.Fatalf("mirrors=%#v", s.Mirrors)
	}
	if _, err := os.Stat(filepath.Join(repo, "licenses/replicant-LICENSE")); !os.IsNotExist(err) {
		t.Fatalf("removed mirror still exists: %v", err)
	}
	runCommandOK(t, repo, []string{"remove", "vendor/replicant"})
	cfg, err = config.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.SourceByName("replicant"); ok {
		t.Fatal("removing the final mirror retained the source")
	}
}

func TestAggregatePullRollsBackConflictApplyFailure(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "--name", "replicant", "vendor/replicant"})
	testutil.WriteFile(t, repo, "vendor/replicant/README.md", "local\n")
	testutil.CommitAll(t, repo, "local")
	configBefore := readTestFile(t, filepath.Join(repo, config.FileName))
	testutil.WriteFile(t, upstream, "README.md", "remote\n")
	testutil.CommitAll(t, upstream, "remote")
	git := failConflictAddGit{Git: gitexec.New(repo, false, nil)}

	stderr := runCommandErrorInDirWithOptions(t, repo, repo, Options{Git: git}, []string{"pull", ":replicant"})
	assertContains(t, stderr, "injected conflict add failure")
	assertFile(t, repo, "vendor/replicant/README.md", "local\n")
	if got := readTestFile(t, filepath.Join(repo, config.FileName)); got != configBefore {
		t.Fatalf("config changed after rollback:\n%s", got)
	}
	if unmerged := strings.TrimSpace(testutil.Git(t, repo, "ls-files", "-u").Stdout); unmerged != "" {
		t.Fatalf("unmerged entries after rollback: %q", unmerged)
	}
	assertNoFile(t, repo, ".git/MERGE_MSG")
}

func TestAggregatePullMaterializesRenameConflictStages(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "old.txt", "content\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "--name", "replicant", "vendor/replicant"})
	testutil.Git(t, repo, "mv", "vendor/replicant/old.txt", "vendor/replicant/local.txt")
	testutil.Git(t, repo, "commit", "-m", "local rename")
	testutil.Git(t, upstream, "mv", "old.txt", "remote.txt")
	testutil.Git(t, upstream, "commit", "-m", "remote rename")

	runCommandOK(t, repo, []string{"pull", ":replicant"})
	unmerged := testutil.Git(t, repo, "ls-files", "-u").Stdout
	assertContains(t, unmerged, " 1\tvendor/replicant/old.txt")
	assertContains(t, unmerged, " 2\tvendor/replicant/local.txt")
	assertContains(t, unmerged, " 3\tvendor/replicant/remote.txt")
}

func TestPullFailureRestoresPreexistingSourceRemote(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "--name", "replicant", "vendor/replicant"})
	m := loadMirror(t, repo, "vendor/replicant")
	oldURL := filepath.Join(t.TempDir(), "retained-remote")
	testutil.Git(t, repo, "remote", "add", m.Remote(), oldURL)
	oldFetch := "+refs/heads/custom:refs/remotes/" + m.Remote() + "/custom"
	testutil.Git(t, repo, "config", "--replace-all", "remote."+m.Remote()+".fetch", oldFetch)
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := cfg.SourceByName("replicant")
	s.URL = filepath.Join(t.TempDir(), "missing-upstream")
	if err := cfg.UpdateSource(s); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "add", config.FileName)
	testutil.Git(t, repo, "commit", "-m", "break source URL")

	runCommandError(t, repo, []string{"--no-cache", "pull", ":replicant"})
	if got := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", m.Remote()).Stdout); got != oldURL {
		t.Fatalf("remote URL = %q, want restored %q", got, oldURL)
	}
	if got := strings.TrimSpace(testutil.Git(t, repo, "config", "--get-all", "remote."+m.Remote()+".fetch").Stdout); got != oldFetch {
		t.Fatalf("remote fetch = %q, want restored %q", got, oldFetch)
	}
}

type failConflictAddGit struct{ gitexec.Git }

func (failConflictAddGit) Add(context.Context, string) error {
	return errors.New("injected conflict add failure")
}

func TestPushRejectsDivergentOverlappingMirrors(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "LICENSE", "one\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "--name", "replicant", "vendor/replicant", "licenses/replicant-LICENSE=LICENSE"})
	testutil.WriteFile(t, repo, "vendor/replicant/LICENSE", "different\n")
	testutil.CommitAll(t, repo, "diverge copies")
	err := runCommandError(t, repo, []string{"push", ":replicant", "--message", "push"})
	if !strings.Contains(err, "inconsistent upstream content") {
		t.Fatalf("error=%q", err)
	}
	if got := readTestFile(t, filepath.Join(upstream, "LICENSE")); got != "one\n" {
		t.Fatalf("upstream changed: %q", got)
	}
}

func TestAddMirrorToExistingSourceUsesRecordedRevision(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "LICENSE", "recorded\n")
	testutil.WriteFile(t, upstream, "src/code", "recorded\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "--name", "replicant", "licenses/LICENSE=LICENSE"})
	testutil.WriteFile(t, upstream, "src/code", "latest\n")
	testutil.CommitAll(t, upstream, "latest")
	runCommandOK(t, repo, []string{"add", ":replicant", "vendor/src=src"})
	assertFile(t, repo, "vendor/src/code", "recorded\n")
}

func TestPushConsistentOverlappingMirrors(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")
	testutil.WriteFile(t, upstream, "LICENSE", "one\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "--name", "replicant", "vendor/replicant", "licenses/LICENSE=LICENSE"})
	testutil.WriteFile(t, repo, "vendor/replicant/LICENSE", "two\n")
	testutil.WriteFile(t, repo, "licenses/LICENSE", "two\n")
	testutil.CommitAll(t, repo, "update both")
	runCommandOK(t, repo, []string{"push", ":replicant", "--message", "update license"})
	if got := readTestFile(t, filepath.Join(upstream, "LICENSE")); got != "two\n" {
		t.Fatalf("upstream=%q", got)
	}
}

func TestAggregatePullMaterializesConflictStages(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "a.txt", "base\n")
	testutil.WriteFile(t, upstream, "b.txt", "base\n")
	testutil.WriteFile(t, upstream, "c.txt", "one\nmiddle\nthree\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "--name", "pair", "vendor/a=a.txt", "vendor/b=b.txt", "vendor/c=c.txt"})
	testutil.WriteFile(t, repo, "vendor/a", "local\n")
	testutil.WriteFile(t, repo, "vendor/c", "local\nmiddle\nthree\n")
	testutil.CommitAll(t, repo, "local")
	testutil.WriteFile(t, upstream, "a.txt", "remote\n")
	testutil.WriteFile(t, upstream, "b.txt", "remote\n")
	testutil.WriteFile(t, upstream, "c.txt", "one\nmiddle\nremote\n")
	testutil.CommitAll(t, upstream, "remote")
	runCommandOK(t, repo, []string{"pull", ":pair"})
	unmerged := testutil.Git(t, repo, "ls-files", "-u").Stdout
	if !strings.Contains(unmerged, "vendor/a") {
		t.Fatalf("unmerged=%q", unmerged)
	}
	if strings.Contains(unmerged, "vendor/c") {
		t.Fatalf("cleanly merged file was left unmerged: %q", unmerged)
	}
	assertFile(t, repo, "vendor/b", "remote\n")
	assertFile(t, repo, "vendor/c", "local\nmiddle\nremote\n")
}
