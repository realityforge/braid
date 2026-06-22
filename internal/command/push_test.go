package command

import (
	"bytes"
	"context"
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

func TestPushCommandGeneratedMessagePromptAndReview(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.Git(t, repo, "config", "--local", "core.commentChar", "auto")
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local generated\n")
	localCommit := commitAllWithMessage(t, repo, "local generated change", "body from downstream")
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(globalConfig, []byte("[commit]\n\tcleanup = whitespace\n"), 0o644); err != nil {
		t.Fatalf("write global gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)

	captureDir := t.TempDir()
	promptCapture := filepath.Join(captureDir, "prompt.txt")
	repoCapture := filepath.Join(captureDir, "repo.txt")
	generator := writeGenerator(t, "#!/bin/sh\nprompt=$1\nmessage=$2\ncontext=$3\nrepo=$4\ncp \"$prompt\" \"$BRAID_GENERATOR_PROMPT\" || exit 1\nprintf '%s\\n' \"$repo\" > \"$BRAID_GENERATOR_REPO\" || exit 1\ntest -d \"$context\" || exit 1\nprintf 'Generated upstream subject\\n\\nGenerated body\\n' > \"$message\" || exit 1\nprintf 'hidden stdout\\n'\nprintf 'hidden stderr\\n' >&2\n")
	t.Setenv("BRAID_GENERATOR_PROMPT", promptCapture)
	t.Setenv("BRAID_GENERATOR_REPO", repoCapture)
	t.Setenv(pushMessageCommandEnv, shellQuote(generator)+" {PROMPT_FILE} {MESSAGE_FILE} {CONTEXT_DIR} {REPO_DIR} {UNKNOWN_PLACEHOLDER}")
	seedCapture, editor := writeCapturingEditor(t, "Reviewed upstream message")
	t.Setenv("GIT_EDITOR", editor)

	stdout, stderr := runCommandOKWithOutput(t, repo, []string{"push", "vendor/basic"})

	assertNotContains(t, stdout, "hidden stdout")
	assertContains(t, stderr, "Braid: generating push commit message for vendor/basic using external tool")
	assertNotContains(t, stderr, "hidden stderr")
	assertNotContains(t, stderr, "core.commentChar=auto")
	expectedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("resolve repo path: %v", err)
	}
	if got := strings.TrimSpace(readTestFile(t, repoCapture)); got != expectedRepo {
		t.Fatalf("generator repo arg = %q, want %q", got, expectedRepo)
	}
	prompt := readTestFile(t, promptCapture)
	for _, want := range []string{
		"The commit will be written to the mirrored/upstream repository, not to the downstream/hosting repository",
		"Describe the staged mirror changes from the mirrored repository's perspective",
		"Use downstream commit provenance only as background for intent",
		"Respond only with the proposed commit message.",
		"Local mirror path: vendor/basic",
		"Upstream URL: " + upstream,
		"Upstream path: (repository root)",
		"Target branch: main",
		"Commit " + localCommit,
		"local generated change",
		"body from downstream",
		"Inline diff byte length:",
		"+local generated",
	} {
		assertContains(t, prompt, want)
	}
	assertNotContains(t, prompt, "Message output file:")
	seed := readTestFile(t, seedCapture)
	assertContains(t, seed, "Generated upstream subject")
	assertContains(t, seed, "Generated body")
	assertContains(t, seed, "# Braid downstream mirror commit guidance for vendor/basic")
	assertContains(t, seed, "# Commit "+localCommit)
	assertCommitSubject(t, upstream, "Reviewed upstream message")
	message := testutil.Git(t, upstream, "log", "-1", "--pretty=%B").Stdout
	assertNotContains(t, message, "Generated upstream subject")
	assertNotContains(t, message, "Braid downstream mirror commit guidance")
}

func TestPushCommandGeneratorFailuresOpenEditorWithDiagnostics(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantReason string
		wantOutput bool
	}{
		{
			name:       "nonzero",
			body:       "#!/bin/sh\ni=0\nwhile [ \"$i\" -lt 5000 ]; do printf o; i=$((i + 1)); done\ni=0\nwhile [ \"$i\" -lt 5000 ]; do printf e >&2; i=$((i + 1)); done\nexit 17\n",
			wantReason: "generator exited with status 17",
			wantOutput: true,
		},
		{
			name:       "missing output",
			body:       "#!/bin/sh\nexit 0\n",
			wantReason: "generator did not create the message output file",
		},
		{
			name:       "whitespace output",
			body:       "#!/bin/sh\nprintf '  \\n\\t\\n' > \"$2\"\n",
			wantReason: "generator wrote only whitespace to the message output file",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			testutil.WriteFile(t, upstream, "README.md", "base\n")
			testutil.CommitAll(t, upstream, "base")
			testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

			repo := initDownstream(t)
			runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
			testutil.Git(t, repo, "config", "--local", "core.commentChar", "auto")
			testutil.WriteFile(t, repo, "vendor/basic/README.md", "local failure\n")
			commitAllWithMessage(t, repo, "local failure change")
			globalConfig := filepath.Join(t.TempDir(), "gitconfig")
			if err := os.WriteFile(globalConfig, []byte("[commit]\n\tcleanup = whitespace\n"), 0o644); err != nil {
				t.Fatalf("write global gitconfig: %v", err)
			}
			t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)

			generator := writeGenerator(t, test.body)
			t.Setenv(pushMessageCommandEnv, shellQuote(generator)+" {PROMPT_FILE} {MESSAGE_FILE}")
			seedCapture, editor := writePrependCapturingEditor(t, "Manual failure message")
			t.Setenv("GIT_EDITOR", editor)

			runCommandOK(t, repo, []string{"push", "vendor/basic"})

			seed := readTestFile(t, seedCapture)
			assertContains(t, seed, "# Braid AI push commit-message generation failed.")
			assertContains(t, seed, "# Reason: "+test.wantReason)
			assertContains(t, seed, "# Braid downstream mirror commit guidance for vendor/basic")
			if test.wantOutput {
				assertContains(t, seed, "# Generator stdout:")
				assertContains(t, seed, pushMessageTruncationMarker)
				assertContains(t, seed, "# Generator stderr:")
			}
			message := testutil.Git(t, upstream, "log", "-1", "--pretty=%B").Stdout
			assertContains(t, message, "Manual failure message")
			assertNotContains(t, message, "Braid AI push commit-message generation failed")
			assertNotContains(t, message, "local failure change")
		})
	}
}

func TestPushCommandGeneratorReadsLargeDiffFromContextDir(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "config", "receive.denyCurrentBranch", "updateInstead")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	largeContent := strings.Repeat("large-diff-line\n", 450)
	testutil.WriteFile(t, repo, "vendor/basic/README.md", largeContent)
	commitAllWithMessage(t, repo, "large generated change")

	captureDir := t.TempDir()
	promptCapture := filepath.Join(captureDir, "prompt.txt")
	diffCapture := filepath.Join(captureDir, "upstream.diff")
	generator := writeGenerator(t, "#!/bin/sh\nprompt=$1\nmessage=$2\ncontext=$3\ndiff_file=$(sed -n 's/^Full diff file: //p' \"$prompt\")\n[ -n \"$diff_file\" ] || exit 12\n[ -f \"$diff_file\" ] || exit 13\ncase \"$diff_file\" in \"$context\"/*) ;; *) exit 14 ;; esac\ncp \"$prompt\" \"$BRAID_GENERATOR_PROMPT\" || exit 1\ncp \"$diff_file\" \"$BRAID_GENERATOR_DIFF\" || exit 1\nprintf 'Generated large diff\\n' > \"$message\"\n")
	t.Setenv("BRAID_GENERATOR_PROMPT", promptCapture)
	t.Setenv("BRAID_GENERATOR_DIFF", diffCapture)
	t.Setenv(pushMessageCommandEnv, shellQuote(generator)+" {PROMPT_FILE} {MESSAGE_FILE} {CONTEXT_DIR}")
	_, editor := writeCapturingEditor(t, "Reviewed large diff")
	t.Setenv("GIT_EDITOR", editor)

	runCommandOK(t, repo, []string{"push", "vendor/basic"})

	prompt := readTestFile(t, promptCapture)
	assertContains(t, prompt, "Full diff file:")
	assertNotContains(t, prompt, "```diff")
	diff := readTestFile(t, diffCapture)
	assertContains(t, diff, "+large-diff-line")
	if len(diff) <= pushMessageInlineDiffLimit {
		t.Fatalf("captured diff length = %d, want greater than %d", len(diff), pushMessageInlineDiffLimit)
	}
	assertCommitSubject(t, upstream, "Reviewed large diff")
}

func TestPushCommandGenerationContinuesWhenProvenanceFails(t *testing.T) {
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
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "malformed generated history\n")
	testutil.WriteFile(t, repo, config.FileName, "{not json\n")
	commitAllWithMessage(t, repo, "malformed historical config")
	if err := os.WriteFile(filepath.Join(repo, config.FileName), validConfig, 0o644); err != nil {
		t.Fatalf("restore valid config: %v", err)
	}
	commitAllWithMessage(t, repo, "restore valid config")

	promptCapture := filepath.Join(t.TempDir(), "prompt.txt")
	generator := writeGenerator(t, "#!/bin/sh\ncp \"$1\" \"$BRAID_GENERATOR_PROMPT\" || exit 1\nprintf 'Generated despite provenance failure\\n' > \"$2\"\n")
	t.Setenv("BRAID_GENERATOR_PROMPT", promptCapture)
	t.Setenv(pushMessageCommandEnv, shellQuote(generator)+" {PROMPT_FILE} {MESSAGE_FILE}")
	_, editor := writeCapturingEditor(t, "Reviewed after provenance failure")
	t.Setenv("GIT_EDITOR", editor)

	_, stderr := runCommandOKWithOutput(t, repo, []string{"push", "vendor/basic"})

	assertContains(t, stderr, "push provenance guidance skipped")
	assertContains(t, stderr, "parse .braids.json")
	prompt := readTestFile(t, promptCapture)
	assertContains(t, prompt, "Unavailable: parse .braids.json")
	assertFile(t, upstream, "README.md", "malformed generated history\n")
	assertCommitSubject(t, upstream, "Reviewed after provenance failure")
}

func TestPushCommandDoesNotRunGeneratorWithoutPushCommitAttempt(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, upstream, repo string)
		wantOut string
	}{
		{
			name: "not up to date",
			prepare: func(t *testing.T, upstream, repo string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "remote\n")
				testutil.CommitAll(t, upstream, "remote")
				testutil.WriteFile(t, repo, "vendor/basic/README.md", "local\n")
				testutil.CommitAll(t, repo, "local mirror change")
			},
			wantOut: "not up to date",
		},
		{
			name: "no local changes",
			prepare: func(t *testing.T, upstream, repo string) {
				t.Helper()
			},
			wantOut: "No local changes",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			testutil.WriteFile(t, upstream, "README.md", "base\n")
			testutil.CommitAll(t, upstream, "base")
			repo := initDownstream(t)
			runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
			test.prepare(t, upstream, repo)

			marker := filepath.Join(t.TempDir(), "generator-ran")
			generator := writeGenerator(t, "#!/bin/sh\nprintf ran > \"$BRAID_GENERATOR_MARKER\"\nexit 99\n")
			t.Setenv("BRAID_GENERATOR_MARKER", marker)
			t.Setenv(pushMessageCommandEnv, shellQuote(generator)+" {PROMPT_FILE} {MESSAGE_FILE}")
			t.Setenv("GIT_EDITOR", writeFailingEditor(t))

			out := runCommandOK(t, repo, []string{"push", "vendor/basic"})

			assertContains(t, out, test.wantOut)
			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("generator marker %s exists, want generator not run", marker)
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat generator marker: %v", err)
			}
		})
	}
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
	assertContains(t, template, "# Please enter the commit message")
	assertNotContains(t, template, "BRAID_COMMIT_TEMPLATE")
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

func TestPushMessageCommandSubstitutionQuotesDocumentedPlaceholders(t *testing.T) {
	values := pushMessageCommandValues{
		RepoDir:     "/tmp/repo with ' quote",
		ContextDir:  "/tmp/context dir",
		PromptFile:  "/tmp/context dir/prompt's.txt",
		MessageFile: "/tmp/context dir/message.txt",
	}

	got := expandPushMessageCommand("run {REPO_DIR} {CONTEXT_DIR} {PROMPT_FILE} {MESSAGE_FILE} {UNKNOWN}", values)

	want := "run '/tmp/repo with '\\'' quote' '/tmp/context dir' '/tmp/context dir/prompt'\\''s.txt' '/tmp/context dir/message.txt' {UNKNOWN}"
	if got != want {
		t.Fatalf("expanded command = %q, want %q", got, want)
	}
}

func TestPushMessageGeneratorVerboseTrace(t *testing.T) {
	repoDir := t.TempDir()
	contextDir := t.TempDir()
	promptPath := filepath.Join(contextDir, pushMessagePromptFileName)
	messagePath := filepath.Join(contextDir, pushMessageOutputFileName)
	if err := os.WriteFile(promptPath, []byte("prompt\n"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	generator := writeGenerator(t, "#!/bin/sh\nprintf 'generated\\n' > \"$2\"\n")
	template := shellQuote(generator) + " {PROMPT_FILE} {MESSAGE_FILE}"
	values := pushMessageCommandValues{
		RepoDir:     repoDir,
		ContextDir:  contextDir,
		PromptFile:  promptPath,
		MessageFile: messagePath,
	}

	var quietTrace bytes.Buffer
	if _, _, err := runPushMessageGenerator(context.Background(), template, values, false, &quietTrace); err != nil {
		t.Fatalf("runPushMessageGenerator quiet returned error: %v", err)
	}
	if quietTrace.String() != "" {
		t.Fatalf("quiet trace = %q, want empty", quietTrace.String())
	}

	var trace bytes.Buffer
	message, failure, err := runPushMessageGenerator(context.Background(), template, values, true, &trace)
	if err != nil {
		t.Fatalf("runPushMessageGenerator verbose returned error: %v", err)
	}
	if failure != nil {
		t.Fatalf("runPushMessageGenerator verbose failure = %#v, want nil", failure)
	}
	if message != "generated" {
		t.Fatalf("generated message = %q, want generated", message)
	}
	assertContains(t, trace.String(), "Braid: Executing [\"/bin/sh\", \"-c\", ")
	assertContains(t, trace.String(), shellQuote(generator))
	assertContains(t, trace.String(), shellQuote(promptPath))
	assertContains(t, trace.String(), shellQuote(messagePath))
	assertContains(t, trace.String(), " in "+repoDir+"\n")
}

func TestPushMessageDiffArgs(t *testing.T) {
	if got, want := pushMessageDiffArgs(""), []string{"--cached", "--no-color", "--no-ext-diff", "--no-textconv", "--binary", "HEAD", "--"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("root diff args = %#v, want %#v", got, want)
	}
	if got, want := pushMessageDiffArgs("lib/path"), []string{"--cached", "--no-color", "--no-ext-diff", "--no-textconv", "--binary", "HEAD", "--", "lib/path"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("path diff args = %#v, want %#v", got, want)
	}
}

func TestPushMessageDiffContextCutoff(t *testing.T) {
	contextDir := t.TempDir()
	inlineDiff := strings.Repeat("a", pushMessageInlineDiffLimit)

	inlineContext, err := writePushMessageDiffContext(contextDir, inlineDiff)
	if err != nil {
		t.Fatalf("writePushMessageDiffContext inline returned error: %v", err)
	}
	if inlineContext.Inline != inlineDiff || inlineContext.FilePath != "" || inlineContext.ByteLen != pushMessageInlineDiffLimit {
		t.Fatalf("inline diff context = %#v, want inline at cutoff", inlineContext)
	}

	largeDiff := inlineDiff + "b"
	largeContext, err := writePushMessageDiffContext(contextDir, largeDiff)
	if err != nil {
		t.Fatalf("writePushMessageDiffContext large returned error: %v", err)
	}
	if largeContext.Inline != "" || largeContext.FilePath == "" || largeContext.ByteLen != pushMessageInlineDiffLimit+1 {
		t.Fatalf("large diff context = %#v, want file over cutoff", largeContext)
	}
	if got := readTestFile(t, largeContext.FilePath); got != largeDiff {
		t.Fatalf("large diff file = %q, want original diff", got)
	}
}

func TestPushMessageGeneratorPlatformSupport(t *testing.T) {
	if err := validatePushMessageGeneratorPlatform("linux"); err != nil {
		t.Fatalf("linux platform validation returned error: %v", err)
	}
	err := validatePushMessageGeneratorPlatform("windows")
	if err == nil || !strings.Contains(err.Error(), "/bin/sh") || !strings.Contains(err.Error(), pushMessageCommandEnv) {
		t.Fatalf("windows platform validation error = %v, want clear unsupported message", err)
	}
}

func TestConfiguredPushMessageGenerationTreatsUnsetAndEmptyAsDisabled(t *testing.T) {
	t.Setenv(pushMessageCommandEnv, "temporary")

	if err := os.Unsetenv(pushMessageCommandEnv); err != nil {
		t.Fatalf("unset %s: %v", pushMessageCommandEnv, err)
	}
	if got := configuredPushMessageGeneration(); got.Enabled {
		t.Fatalf("unset env configured generation = %#v, want disabled", got)
	}

	t.Setenv(pushMessageCommandEnv, "")
	if got := configuredPushMessageGeneration(); got.Enabled {
		t.Fatalf("empty env configured generation = %#v, want disabled", got)
	}

	t.Setenv(pushMessageCommandEnv, "printf message")
	if got := configuredPushMessageGeneration(); !got.Enabled || got.CommandTemplate != "printf message" {
		t.Fatalf("configured generation = %#v, want enabled command", got)
	}
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
