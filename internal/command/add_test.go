package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/config"
	"braid/internal/mirror"
	"braid/internal/testutil"
)

func TestAddCommandDefaultBranchCommitsAndRemovesRemote(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "hello from upstream\n")
	revision := testutil.CommitAll(t, upstream, "upstream")

	repo := initDownstream(t)
	writeFailingPreCommitHook(t, repo)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	assertFile(t, repo, "vendor/basic/README.md", "hello from upstream\n")
	m := loadMirror(t, repo, "vendor/basic")
	if m.URL != upstream || m.Branch != "main" || m.Tag != "" || m.Revision != revision {
		t.Fatalf("mirror = %#v, want upstream main at %s", m, revision)
	}
	assertCommitSubject(t, repo, "Braid: Add mirror 'vendor/basic' at '"+revision[:7]+"'")
	assertCommitIdentity(t, repo)
	assertNoRemote(t, repo, m.Remote())
	assertClean(t, repo)
}

func TestAddCommandNormalizesNativeLocalPathArgument(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "native path\n")
	revision := testutil.CommitAll(t, upstream, "upstream")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, `vendor\native`})

	assertFile(t, repo, "vendor/native/README.md", "native path\n")
	m := loadMirror(t, repo, "vendor/native")
	if m.Path != "vendor/native" || m.Revision != revision {
		t.Fatalf("mirror = %#v, want normalized path at %s", m, revision)
	}
}

func TestAddCommandGlobalVerboseTracesWorktreeAndCacheGit(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "trace\n")
	testutil.CommitAll(t, upstream, "upstream")

	repo := initDownstream(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_LOCAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"--verbose", "add", upstream, "vendor/basic"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("braid add exit = %d, stderr = %q", code, stderr.String())
	}
	trace := stderr.String()
	assertContains(t, trace, `Braid: Executing ["git", "--version"]`)
	assertContains(t, trace, `Braid: Executing ["git", "fetch", "-n", "main_braid_vendor_basic"]`)
	assertContains(t, trace, `Braid: Executing ["git", "clone", "--mirror"`)
}

func TestAddCommandMirrorVariants(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, upstream string) string
		args       func(upstream, revision string) []string
		wantPath   string
		wantFile   string
		wantBranch string
		wantTag    string
		wantLocked bool
	}{
		{
			name: "explicit branch",
			prepare: func(t *testing.T, upstream string) string {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "main\n")
				testutil.CommitAll(t, upstream, "main")
				testutil.Git(t, upstream, "checkout", "-b", "feature")
				testutil.WriteFile(t, upstream, "README.md", "feature\n")
				return testutil.CommitAll(t, upstream, "feature")
			},
			args: func(upstream, _ string) []string {
				return []string{"add", upstream, "vendor/branch", "--branch", "feature"}
			},
			wantPath:   "vendor/branch",
			wantFile:   "vendor/branch/README.md",
			wantBranch: "feature",
		},
		{
			name: "revision locked",
			prepare: func(t *testing.T, upstream string) string {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "locked\n")
				return testutil.CommitAll(t, upstream, "locked")
			},
			args: func(upstream, revision string) []string {
				return []string{"add", upstream, "vendor/revision", "--revision", revision}
			},
			wantPath:   "vendor/revision",
			wantFile:   "vendor/revision/README.md",
			wantLocked: true,
		},
		{
			name: "upstream subdirectory",
			prepare: func(t *testing.T, upstream string) string {
				t.Helper()
				testutil.WriteFile(t, upstream, "lib/component.txt", "component\n")
				testutil.WriteFile(t, upstream, "ignored.txt", "ignored\n")
				return testutil.CommitAll(t, upstream, "subdir")
			},
			args:       func(upstream, _ string) []string { return []string{"add", upstream, "vendor/lib", "--path", "lib"} },
			wantPath:   "vendor/lib",
			wantFile:   "vendor/lib/component.txt",
			wantBranch: "main",
		},
		{
			name: "path with spaces",
			prepare: func(t *testing.T, upstream string) string {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "spaces\n")
				return testutil.CommitAll(t, upstream, "spaces")
			},
			args:       func(upstream, _ string) []string { return []string{"add", upstream, "vendor/path with spaces"} },
			wantPath:   "vendor/path with spaces",
			wantFile:   "vendor/path with spaces/README.md",
			wantBranch: "main",
		},
		{
			name: "single file",
			prepare: func(t *testing.T, upstream string) string {
				t.Helper()
				testutil.WriteFile(t, upstream, "LICENSE.txt", "license\n")
				testutil.WriteFile(t, upstream, "ignored.txt", "ignored\n")
				return testutil.CommitAll(t, upstream, "single file")
			},
			args: func(upstream, _ string) []string {
				return []string{"add", upstream, "licenses/THIRD_PARTY.txt", "--path", "LICENSE.txt"}
			},
			wantPath:   "licenses/THIRD_PARTY.txt",
			wantFile:   "licenses/THIRD_PARTY.txt",
			wantBranch: "main",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			revision := test.prepare(t, upstream)
			repo := initDownstream(t)
			runCommandOK(t, repo, test.args(upstream, revision))

			if test.wantFile == "licenses/THIRD_PARTY.txt" {
				assertFile(t, repo, test.wantFile, "license\n")
			} else {
				if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(test.wantFile))); err != nil {
					t.Fatalf("expected %s to exist: %v", test.wantFile, err)
				}
			}
			m := loadMirror(t, repo, test.wantPath)
			if m.Revision != revision {
				t.Fatalf("revision = %q, want %q", m.Revision, revision)
			}
			if test.wantLocked {
				if m.Branch != "" || m.Tag != "" {
					t.Fatalf("locked mirror has branch/tag: %#v", m)
				}
			} else if m.Branch != test.wantBranch || m.Tag != test.wantTag {
				t.Fatalf("tracking = branch %q tag %q, want branch %q tag %q", m.Branch, m.Tag, test.wantBranch, test.wantTag)
			}
			assertClean(t, repo)
		})
	}
}

func TestAddCommandNoCacheTags(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		make func(t *testing.T, upstream, tag string) string
	}{
		{
			name: "lightweight",
			tag:  "v1-light",
			make: func(t *testing.T, upstream, tag string) string {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "lightweight\n")
				revision := testutil.CommitAll(t, upstream, "lightweight")
				testutil.Git(t, upstream, "tag", tag)
				return revision
			},
		},
		{
			name: "annotated",
			tag:  "v1-annotated",
			make: func(t *testing.T, upstream, tag string) string {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "annotated\n")
				revision := testutil.CommitAll(t, upstream, "annotated")
				testutil.Git(t, upstream, "tag", "-a", tag, "-m", "annotated tag")
				return revision
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			revision := test.make(t, upstream, test.tag)
			repo := initDownstream(t)
			localPath := "vendor/" + test.name
			runCommandOK(t, repo, []string{"--no-cache", "add", upstream, localPath, "--tag", test.tag})

			assertFile(t, repo, localPath+"/README.md", test.name+"\n")
			m := loadMirror(t, repo, localPath)
			if m.Tag != test.tag || m.Branch != "" || m.Revision != revision {
				t.Fatalf("mirror = %#v, want tag %q at %s", m, test.tag, revision)
			}
			assertClean(t, repo)
		})
	}
}

func TestAddCommandMissingUpstreamPathResets(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "hello\n")
	testutil.CommitAll(t, upstream, "upstream")

	repo := initDownstream(t)
	head := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD").Stdout)
	stderr := runCommandError(t, repo, []string{"add", upstream, "vendor/missing", "--path", "does-not-exist"})
	if !strings.Contains(stderr, "no tree item exists") {
		t.Fatalf("stderr = %q, want missing tree item diagnostic", stderr)
	}
	gotHead := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD").Stdout)
	if gotHead != head {
		t.Fatalf("HEAD = %s, want original %s", gotHead, head)
	}
	if _, err := os.Stat(filepath.Join(repo, config.FileName)); !os.IsNotExist(err) {
		t.Fatalf("%s exists after failed add, stat err = %v", config.FileName, err)
	}
	if remotes := strings.TrimSpace(testutil.Git(t, repo, "remote").Stdout); remotes != "" {
		t.Fatalf("remotes after failed add = %q, want none", remotes)
	}
	assertClean(t, repo)
}

func initDownstream(t *testing.T) string {
	t.Helper()
	repo := testutil.InitRepo(t)
	testutil.WriteFile(t, repo, "README.md", "downstream\n")
	testutil.CommitAll(t, repo, "downstream")
	return repo
}

func runCommandOK(t *testing.T, repo string, args []string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_LOCAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
	t.Chdir(repo)
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run(args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("braid %v exit = %d, stderr = %q", args, code, stderr.String())
	}
	return stdout.String()
}

func runCommandError(t *testing.T, repo string, args []string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_LOCAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
	t.Chdir(repo)
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run(args, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("braid %v succeeded unexpectedly, stdout = %q", args, stdout.String())
	}
	return stderr.String()
}

func loadMirror(t *testing.T, repo, localPath string) mirror.Mirror {
	t.Helper()
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	m, err := cfg.GetRequired(localPath)
	if err != nil {
		t.Fatalf("GetRequired(%q): %v", localPath, err)
	}
	return m
}

func assertFile(t *testing.T, repo, relativePath, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", relativePath, string(data), want)
	}
}

func assertCommitSubject(t *testing.T, repo, want string) {
	t.Helper()
	got := strings.TrimSpace(testutil.Git(t, repo, "log", "-1", "--pretty=%s").Stdout)
	if got != want {
		t.Fatalf("commit subject = %q, want %q", got, want)
	}
}

func assertCommitIdentity(t *testing.T, repo string) {
	t.Helper()
	got := strings.TrimSpace(testutil.Git(t, repo, "log", "-1", "--pretty=%an <%ae>").Stdout)
	want := testutil.DefaultName + " <" + testutil.DefaultEmail + ">"
	if got != want {
		t.Fatalf("commit identity = %q, want %q", got, want)
	}
}

func assertNoRemote(t *testing.T, repo, remote string) {
	t.Helper()
	remotes := strings.Fields(testutil.Git(t, repo, "remote").Stdout)
	for _, got := range remotes {
		if got == remote {
			t.Fatalf("remote %q still exists", remote)
		}
	}
}

func assertClean(t *testing.T, repo string) {
	t.Helper()
	if status := strings.TrimSpace(testutil.Git(t, repo, "status", "--porcelain").Stdout); status != "" {
		t.Fatalf("status --porcelain = %q, want clean", status)
	}
}

func writeFailingPreCommitHook(t *testing.T, repo string) {
	t.Helper()
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write pre-commit hook: %v", err)
	}
}

func writePostCommitHook(t *testing.T, repo string) {
	t.Helper()
	hook := filepath.Join(repo, ".git", "hooks", "post-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf 'ran\\n' > post-commit-ran\n"), 0o755); err != nil {
		t.Fatalf("write post-commit hook: %v", err)
	}
}
