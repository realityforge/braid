package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
