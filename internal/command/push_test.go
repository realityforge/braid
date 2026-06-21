package command

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/config"
	"braid/internal/mirror"
	"braid/internal/testutil"
)

func TestPushCommandPushesBranchAndPreservesIdentity(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	testutil.Git(t, repo, "config", "--local", "user.name", "Push User")
	testutil.Git(t, repo, "config", "--local", "user.email", "push@example.invalid")
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Push mirror changes"))

	runCommandOK(t, repo, []string{"push", "vendor/basic"})
	assertFile(t, upstream, "README.md", "local\n")
	assertCommitSubject(t, upstream, "Push mirror changes")
	got := strings.TrimSpace(testutil.Git(t, upstream, "log", "-1", "--pretty=%an <%ae>").Stdout)
	if got != "Push User <push@example.invalid>" {
		t.Fatalf("pushed identity = %q", got)
	}
}

func TestPushCommandEditorReceivesStdin(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "stdin\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeStdinEditor(t))

	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{
		WorkDir:    repo,
		ConfigRoot: repo,
		Stdin:      strings.NewReader("Push from stdin\n"),
	}).Run([]string{"push", "vendor/basic"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("braid push exit = %d, stderr = %q", code, stderr.String())
	}
	assertFile(t, upstream, "README.md", "stdin\n")
	assertCommitSubject(t, upstream, "Push from stdin")
}

func TestPushCommandPushesExplicitBranch(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "feature\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Push feature"))

	runCommandOK(t, repo, []string{"push", "vendor/basic", "--branch", "feature"})
	testutil.Git(t, upstream, "checkout", "feature")
	assertFile(t, upstream, "README.md", "feature\n")
}

func TestPushCommandPathVariants(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, upstream string)
		addArgs    func(upstream string) []string
		localPath  string
		localFile  string
		remoteFile string
	}{
		{
			name: "subdirectory",
			prepare: func(t *testing.T, upstream string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "lib/component.txt", "base\n")
			},
			addArgs:    func(upstream string) []string { return []string{"add", upstream, "vendor/lib", "--path", "lib"} },
			localPath:  "vendor/lib",
			localFile:  "vendor/lib/component.txt",
			remoteFile: "lib/component.txt",
		},
		{
			name: "path with spaces",
			prepare: func(t *testing.T, upstream string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "lay outs/layout.txt", "base\n")
			},
			addArgs: func(upstream string) []string {
				return []string{"add", upstream, "vendor/path with spaces", "--path", "lay outs"}
			},
			localPath:  "vendor/path with spaces",
			localFile:  "vendor/path with spaces/layout.txt",
			remoteFile: "lay outs/layout.txt",
		},
		{
			name: "single file",
			prepare: func(t *testing.T, upstream string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "LICENSE.txt", "base\n")
			},
			addArgs: func(upstream string) []string {
				return []string{"add", upstream, "licenses/THIRD_PARTY.txt", "--path", "LICENSE.txt"}
			},
			localPath:  "licenses/THIRD_PARTY.txt",
			localFile:  "licenses/THIRD_PARTY.txt",
			remoteFile: "LICENSE.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			test.prepare(t, upstream)
			testutil.CommitAll(t, upstream, "base")
			testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")
			repo := initDownstream(t)
			runCommandOK(t, repo, test.addArgs(upstream))
			testutil.WriteFile(t, repo, test.localFile, "changed\n")
			testutil.CommitAll(t, repo, "local mirror change")
			t.Setenv("GIT_EDITOR", writeEditor(t, "Push "+test.name))

			runCommandOK(t, repo, []string{"push", test.localPath})
			assertFile(t, upstream, test.remoteFile, "changed\n")
		})
	}
}

func TestPushCommandWorksWithDefaultWorkDir(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	if code := NewApp().Run([]string{"add", upstream, "vendor/basic"}, &stdout, &stderr); code != 0 {
		t.Fatalf("braid add exit = %d, stderr = %q", code, stderr.String())
	}
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "default workdir\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Push from default workdir"))

	stdout.Reset()
	stderr.Reset()
	if code := NewApp().Run([]string{"push", "vendor/basic"}, &stdout, &stderr); code != 0 {
		t.Fatalf("braid push exit = %d, stderr = %q", code, stderr.String())
	}
	assertFile(t, upstream, "README.md", "default workdir\n")
}

func TestPushCommandTagRequiresExplicitBranchAndSupportsNoCache(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "tag", "v1")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"--no-cache", "add", upstream, "vendor/tagged", "--tag", "v1"})
	testutil.WriteFile(t, repo, "vendor/tagged/README.md", "tag pushed\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Push tag branch"))

	stderr := runCommandError(t, repo, []string{"push", "vendor/tagged"})
	assertContains(t, stderr, "specify --branch")

	runCommandOK(t, repo, []string{"--no-cache", "push", "vendor/tagged", "--branch", "tag-output"})
	testutil.Git(t, upstream, "checkout", "tag-output")
	assertFile(t, upstream, "README.md", "tag pushed\n")
}

func TestPushCommandRevisionRequiresExplicitBranch(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/revision", "--revision", revision})
	testutil.WriteFile(t, repo, "vendor/revision/README.md", "revision pushed\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeEditor(t, "Push revision branch"))

	stderr := runCommandError(t, repo, []string{"push", "vendor/revision"})
	assertContains(t, stderr, "specify --branch")

	runCommandOK(t, repo, []string{"push", "vendor/revision", "--branch", "revision-output"})
	testutil.Git(t, upstream, "checkout", "revision-output")
	assertFile(t, upstream, "README.md", "revision pushed\n")
}

func TestPushCommandStopsWhenNotUpToDateOrNoLocalChanges(t *testing.T) {
	t.Run("not up to date", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		testutil.CommitAll(t, upstream, "base")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

		testutil.WriteFile(t, upstream, "README.md", "remote\n")
		testutil.CommitAll(t, upstream, "remote")
		testutil.WriteFile(t, repo, "vendor/basic/README.md", "local\n")
		testutil.CommitAll(t, repo, "local mirror change")
		t.Setenv("GIT_EDITOR", writeEditor(t, "Should not push"))

		out := runCommandOK(t, repo, []string{"push", "vendor/basic"})
		assertContains(t, out, "not up to date")
		assertFile(t, upstream, "README.md", "remote\n")
	})

	t.Run("no local changes", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		head := testutil.CommitAll(t, upstream, "base")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
		t.Setenv("GIT_EDITOR", writeEditor(t, "Should not push"))

		out := runCommandOK(t, repo, []string{"push", "vendor/basic"})
		assertContains(t, out, "No local changes")
		gotHead := strings.TrimSpace(testutil.Git(t, upstream, "rev-parse", "HEAD").Stdout)
		if gotHead != head {
			t.Fatalf("upstream HEAD = %s, want unchanged %s", gotHead, head)
		}
	})

	t.Run("uncommitted local changes", func(t *testing.T) {
		upstream := testutil.InitRepo(t)
		testutil.WriteFile(t, upstream, "README.md", "base\n")
		head := testutil.CommitAll(t, upstream, "base")
		repo := initDownstream(t)
		runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
		testutil.WriteFile(t, repo, "vendor/basic/README.md", "uncommitted\n")
		t.Setenv("GIT_EDITOR", writeEditor(t, "Should not push"))

		out := runCommandOK(t, repo, []string{"push", "vendor/basic"})
		assertContains(t, out, "downstream HEAD")
		gotHead := strings.TrimSpace(testutil.Git(t, upstream, "rev-parse", "HEAD").Stdout)
		if gotHead != head {
			t.Fatalf("upstream HEAD = %s, want unchanged %s", gotHead, head)
		}
		assertFile(t, upstream, "README.md", "base\n")
	})
}

func TestPushCommandDoesNotPushWhenEditorFails(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	head := testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local\n")
	testutil.CommitAll(t, repo, "local mirror change")
	t.Setenv("GIT_EDITOR", writeFailingEditor(t))

	runCommandError(t, repo, []string{"push", "vendor/basic"})
	gotHead := strings.TrimSpace(testutil.Git(t, upstream, "rev-parse", "HEAD").Stdout)
	if gotHead != head {
		t.Fatalf("upstream HEAD = %s, want unchanged %s", gotHead, head)
	}
	assertFile(t, upstream, "README.md", "base\n")
}

func TestPushCommandSparseCheckoutAvoidsRequiredFilter(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, ".gitattributes", "*.dat filter=required-test\n")
	testutil.WriteFile(t, upstream, "data.dat", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/data.dat", "changed\n")
	testutil.CommitAll(t, repo, "local mirror change")

	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(globalConfig, []byte("[filter \"required-test\"]\n\trequired = true\n\tsmudge = false\n\tclean = cat\n"), 0o644); err != nil {
		t.Fatalf("write global gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_EDITOR", writeEditor(t, "Push with required filter"))

	runCommandOK(t, repo, []string{"push", "vendor/basic", "--branch", "filtered-output"})
	got := testutil.Git(t, upstream, "show", "filtered-output:data.dat").Stdout
	if got != "changed\n" {
		t.Fatalf("filtered-output:data.dat = %q", got)
	}
}

func TestPushCommandProvenanceTemplateGuidesMessageAndStripsComments(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local one\n")
	localOne := commitAllWithMessage(t, repo, "local one", "body line\n\nfooter")
	testutil.WriteFile(t, repo, "vendor/basic/extra.txt", "extra\n")
	localTwo := commitAllWithMessage(t, repo, "local two")
	capture, editor := writePrependCapturingEditor(t, "Push guided")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	for _, want := range []string{
		"# Braid downstream mirror commit guidance for vendor/basic",
		"# Commit " + localOne,
		"# local one",
		"# body line",
		"# ",
		"# footer",
		"# Commit " + localTwo,
		"# local two",
	} {
		assertContains(t, template, want)
	}
	assertInOrder(t, template, "# Commit "+localOne, "# Commit "+localTwo)
	message := strings.TrimSpace(testutil.Git(t, upstream, "log", "-1", "--pretty=%B").Stdout)
	if message != "Push guided" {
		t.Fatalf("upstream message = %q, want Push guided", message)
	}
}

func TestPushCommandProvenanceExcludesOnlyBraidAutomaticCommitsWithoutWarning(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.Git(t, repo, "config", "--local", "core.commentChar", "auto")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "automatic-looking local change\n")
	commitAllWithMessage(t, repo, "Braid: Update mirror 'vendor/basic' to 'fffffff'")
	capture, editor := writeCapturingEditor(t, "Push automatic-looking change")
	t.Setenv("GIT_EDITOR", editor)

	_, stderr := runCommandOKWithOutput(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	assertNotContains(t, template, "Braid downstream mirror commit guidance")
	assertNotContains(t, stderr, "core.commentChar=auto")
	assertNotContains(t, stderr, "push provenance guidance skipped")
	assertFile(t, upstream, "README.md", "automatic-looking local change\n")
}

func TestPushCommandProvenanceCustomCommentCharAndCleanupWhitespace(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.Git(t, repo, "config", "--local", "core.commentChar", ";")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "custom comment\n")
	localCommit := commitAllWithMessage(t, repo, "local custom comment")
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(globalConfig, []byte("[commit]\n\tcleanup = whitespace\n"), 0o644); err != nil {
		t.Fatalf("write global gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	capture, editor := writePrependCapturingEditor(t, "Push custom comment")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	assertContains(t, template, "; Braid downstream mirror commit guidance for vendor/basic")
	assertContains(t, template, "; Commit "+localCommit)
	message := testutil.Git(t, upstream, "log", "-1", "--pretty=%B").Stdout
	assertContains(t, message, "Push custom comment")
	assertNotContains(t, message, "Braid downstream mirror commit guidance")
	assertNotContains(t, message, "local custom comment")
}

func TestPushCommandProvenanceSpaceCommentCharAndCleanupWhitespace(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.Git(t, repo, "config", "--local", "core.commentChar", " ")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "space comment\n")
	localCommit := commitAllWithMessage(t, repo, "local space comment")
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(globalConfig, []byte("[commit]\n\tcleanup = whitespace\n"), 0o644); err != nil {
		t.Fatalf("write global gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	capture, editor := writePrependCapturingEditor(t, "Push space comment")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	assertContains(t, template, "  Braid downstream mirror commit guidance for vendor/basic")
	assertContains(t, template, "  Commit "+localCommit)
	assertNotContains(t, template, "# Braid downstream mirror commit guidance")
	message := testutil.Git(t, upstream, "log", "-1", "--pretty=%B").Stdout
	assertContains(t, message, "Push space comment")
	assertNotContains(t, message, "Braid downstream mirror commit guidance")
	assertNotContains(t, message, "local space comment")
}

func TestPushCommandProvenanceCommentCharAutoWarnsAndSkips(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.Git(t, repo, "config", "--local", "core.commentChar", "auto")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "auto comment\n")
	commitAllWithMessage(t, repo, "local auto comment")
	capture, editor := writeCapturingEditor(t, "Push auto comment")
	t.Setenv("GIT_EDITOR", editor)

	_, stderr := runCommandOKWithOutput(t, repo, []string{"push", "vendor/basic"})

	assertContains(t, stderr, "push provenance guidance skipped")
	assertContains(t, stderr, "core.commentChar=auto")
	assertNotContains(t, readTestFile(t, capture), "Braid downstream mirror commit guidance")
	assertFile(t, upstream, "README.md", "auto comment\n")
}

func TestPushCommandProvenanceFailureWarnsAndContinues(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	validConfig, err := os.ReadFile(filepath.Join(repo, config.FileName))
	if err != nil {
		t.Fatalf("read valid config: %v", err)
	}
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "malformed history\n")
	testutil.WriteFile(t, repo, config.FileName, "{not json\n")
	commitAllWithMessage(t, repo, "malformed historical config")
	if err := os.WriteFile(filepath.Join(repo, config.FileName), validConfig, 0o644); err != nil {
		t.Fatalf("restore valid config: %v", err)
	}
	commitAllWithMessage(t, repo, "restore valid config")
	capture, editor := writeCapturingEditor(t, "Push after malformed history")
	t.Setenv("GIT_EDITOR", editor)

	_, stderr := runCommandOKWithOutput(t, repo, []string{"push", "vendor/basic"})

	assertContains(t, stderr, "push provenance guidance skipped")
	assertContains(t, stderr, "parse .braids.json")
	assertNotContains(t, readTestFile(t, capture), "Braid downstream mirror commit guidance")
	assertFile(t, upstream, "README.md", "malformed history\n")
}

func TestPushCommandProvenanceSurvivesUpdateAndExcludesBraidCommit(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "local.txt", "base local\n")
	testutil.WriteFile(t, upstream, "remote.txt", "base remote\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/local.txt", "local change\n")
	localCommit := commitAllWithMessage(t, repo, "local before update")
	testutil.WriteFile(t, upstream, "remote.txt", "remote change\n")
	remoteRevision := testutil.CommitAll(t, upstream, "remote update")
	runCommandOK(t, repo, []string{"update", "vendor/basic"})
	updateSubject := "Braid: Update mirror 'vendor/basic' to '" + remoteRevision[:7] + "'"
	capture, editor := writeCapturingEditor(t, "Push after update")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	assertContains(t, template, "# Commit "+localCommit)
	assertContains(t, template, "# local before update")
	assertNotContains(t, template, updateSubject)
	assertFile(t, upstream, "local.txt", "local change\n")
	assertFile(t, upstream, "remote.txt", "remote change\n")
}

func TestPushCommandProvenanceCapShowsNewestTwentyFiveChronologically(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	var commits []string
	for i := 1; i <= 26; i++ {
		testutil.WriteFile(t, repo, "vendor/basic/README.md", fmt.Sprintf("change %02d\n", i))
		commits = append(commits, commitAllWithMessage(t, repo, fmt.Sprintf("local change %02d", i)))
	}
	capture, editor := writeCapturingEditor(t, "Push capped changes")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	if got := strings.Count(template, "# Commit "); got != 25 {
		t.Fatalf("commit entries = %d, want 25:\n%s", got, template)
	}
	assertContains(t, template, "# 1 older eligible downstream commit omitted.")
	assertNotContains(t, template, commits[0])
	assertContains(t, template, "# Commit "+commits[1])
	assertContains(t, template, "# Commit "+commits[25])
	assertInOrder(t, template, "# Commit "+commits[1], "# Commit "+commits[25])
}

func TestPushCommandProvenanceIncludesFullHistoryAndMergeCommit(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.Git(t, repo, "checkout", "-b", "feature")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "feature\n")
	featureCommit := commitAllWithMessage(t, repo, "feature mirror change")
	testutil.Git(t, repo, "checkout", "main")
	testutil.WriteFile(t, repo, "README.md", "main outside\n")
	commitAllWithMessage(t, repo, "main outside change")
	testutil.Git(t, repo, "merge", "--no-ff", "--no-commit", "feature")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "merge adjusted\n")
	mergeCommit := commitAllWithMessage(t, repo, "merge feature adjusted")
	capture, editor := writeCapturingEditor(t, "Push merged history")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	assertContains(t, template, "# Commit "+featureCommit)
	assertContains(t, template, "# feature mirror change")
	assertContains(t, template, "# Commit "+mergeCommit)
	assertContains(t, template, "# merge feature adjusted")
}

func TestPushCommandProvenanceMirrorIdentityChangeResetsWindow(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	bRevision := testutil.CommitAll(t, upstreamB, "b base")
	testutil.Git(t, upstreamB, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "old local\n")
	oldCommit := commitAllWithMessage(t, repo, "old identity local")
	writeSingleMirrorConfig(t, repo, mirror.Mirror{Path: "vendor/basic", URL: upstreamB, Branch: "main", Revision: bRevision})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "b base\n")
	commitAllWithMessage(t, repo, "switch mirror identity")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "new local\n")
	newCommit := commitAllWithMessage(t, repo, "new identity local")
	capture, editor := writeCapturingEditor(t, "Push new identity")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	assertNotContains(t, template, oldCommit)
	assertNotContains(t, template, "old identity local")
	assertContains(t, template, "# Commit "+newCommit)
	assertContains(t, template, "# new identity local")
	assertFile(t, upstreamB, "README.md", "new local\n")
}

func TestPushCommandProvenanceExcludesMergedSideBranchBeforeCurrentIdentity(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	bRevision := testutil.CommitAll(t, upstreamB, "b base")
	testutil.Git(t, upstreamB, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/basic"})
	testutil.Git(t, repo, "checkout", "-b", "old-side")
	testutil.WriteFile(t, repo, "vendor/basic/old-side.txt", "old side\n")
	oldSideCommit := commitAllWithMessage(t, repo, "old side identity")
	testutil.Git(t, repo, "checkout", "main")
	writeSingleMirrorConfig(t, repo, mirror.Mirror{Path: "vendor/basic", URL: upstreamB, Branch: "main", Revision: bRevision})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "b base\n")
	commitAllWithMessage(t, repo, "switch to new identity")
	testutil.Git(t, repo, "merge", "--no-ff", "--no-commit", "old-side")
	mergeCommit := commitAllWithMessage(t, repo, "merge old side after identity switch")
	capture, editor := writeCapturingEditor(t, "Push side branch filtered")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	assertNotContains(t, template, oldSideCommit)
	assertNotContains(t, template, "old side identity")
	assertContains(t, template, "# Commit "+mergeCommit)
	assertContains(t, template, "# merge old side after identity switch")
	assertFile(t, upstreamB, "old-side.txt", "old side\n")
}

func TestPushCommandProvenanceRootWithoutCleanAnchorShowsNote(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := testutil.InitRepo(t)
	writeSingleMirrorConfig(t, repo, mirror.Mirror{Path: "vendor/basic", URL: upstream, Branch: "main", Revision: revision})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "dirty from root\n")
	rootCommit := commitAllWithMessage(t, repo, "root dirty mirror")
	capture, editor := writeCapturingEditor(t, "Push root dirty")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	template := readTestFile(t, capture)
	assertContains(t, template, "# No clean mirror anchor was found;")
	assertContains(t, template, "# Commit "+rootCommit)
	assertContains(t, template, "# root dirty mirror")
	assertFile(t, upstream, "README.md", "dirty from root\n")
}

func TestAlternateObjectPathUsesAbsoluteSlashPath(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)

	got, err := alternateObjectPath(filepath.Join(".git", "objects"), ".")
	if err != nil {
		t.Fatalf("alternateObjectPath returned error: %v", err)
	}
	want := filepath.ToSlash(filepath.Join(repo, ".git", "objects"))
	if got != want {
		t.Fatalf("alternate path = %q, want %q", got, want)
	}
}

func TestAlternateObjectPathRejectsEmptyPath(t *testing.T) {
	if _, err := alternateObjectPath("", "."); err == nil {
		t.Fatal("alternateObjectPath succeeded with empty objects path")
	}
}

func writeEditor(t *testing.T, message string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\nprintf '" + message + "\\n' > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write editor: %v", err)
	}
	return path
}

func writeStdinEditor(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\nIFS= read -r message || exit 1\nprintf '%s\\n' \"$message\" > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stdin editor: %v", err)
	}
	return path
}

func writeFailingEditor(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write failing editor: %v", err)
	}
	return path
}

func writeCapturingEditor(t *testing.T, message string) (string, string) {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "commit-message.txt")
	t.Setenv("BRAID_EDITOR_CAPTURE", capture)
	t.Setenv("BRAID_EDITOR_MESSAGE", message)
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\ncp \"$1\" \"$BRAID_EDITOR_CAPTURE\" || exit 1\nprintf '%s\\n' \"$BRAID_EDITOR_MESSAGE\" > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write capturing editor: %v", err)
	}
	return capture, path
}

func writePrependCapturingEditor(t *testing.T, message string) (string, string) {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "commit-message.txt")
	t.Setenv("BRAID_EDITOR_CAPTURE", capture)
	t.Setenv("BRAID_EDITOR_MESSAGE", message)
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\ncp \"$1\" \"$BRAID_EDITOR_CAPTURE\" || exit 1\ntmp=\"$1.tmp\"\nprintf '%s\\n\\n' \"$BRAID_EDITOR_MESSAGE\" > \"$tmp\" || exit 1\ncat \"$1\" >> \"$tmp\" || exit 1\nmv \"$tmp\" \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write prepend editor: %v", err)
	}
	return capture, path
}

func writeSequenceCapturingEditor(t *testing.T, messagePrefix string) (string, string) {
	t.Helper()
	captureDir := t.TempDir()
	t.Setenv("BRAID_EDITOR_CAPTURE_DIR", captureDir)
	t.Setenv("BRAID_EDITOR_MESSAGE_PREFIX", messagePrefix)
	path := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\ndir=\"$BRAID_EDITOR_CAPTURE_DIR\"\ncount_file=\"$dir/count\"\nif [ -f \"$count_file\" ]; then count=$(cat \"$count_file\"); else count=0; fi\ncount=$((count + 1))\nprintf '%s\\n' \"$count\" > \"$count_file\" || exit 1\ncp \"$1\" \"$dir/template-$count.txt\" || exit 1\nprintf '%s %s\\n' \"$BRAID_EDITOR_MESSAGE_PREFIX\" \"$count\" > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write sequence editor: %v", err)
	}
	return captureDir, path
}

func commitAllWithMessage(t *testing.T, repo, subject string, bodies ...string) string {
	t.Helper()
	testutil.Git(t, repo, "add", ".")
	args := []string{"commit", "-m", subject}
	for _, body := range bodies {
		args = append(args, "-m", body)
	}
	testutil.Git(t, repo, args...)
	return testutil.CurrentRevision(t, repo)
}

func writeSingleMirrorConfig(t *testing.T, repo string, m mirror.Mirror) {
	t.Helper()
	cfg := config.Empty()
	if err := cfg.Add(m); err != nil {
		t.Fatalf("add mirror config: %v", err)
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("write mirror config: %v", err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func runCommandOKWithOutput(t *testing.T, repo string, args []string) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_LOCAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
	t.Chdir(repo)
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: repo}).Run(args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("braid %v exit = %d, stderr = %q", args, code, stderr.String())
	}
	return stdout.String(), stderr.String()
}

func assertInOrder(t *testing.T, value string, needles ...string) {
	t.Helper()
	offset := 0
	for _, needle := range needles {
		index := strings.Index(value[offset:], needle)
		if index < 0 {
			t.Fatalf("%q does not appear after offset %d in:\n%s", needle, offset, value)
		}
		offset += index + len(needle)
	}
}
