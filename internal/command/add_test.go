package command

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
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

func TestAddCommandPreservesUnrelatedIndexAndWorktreeState(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "hello from upstream\n")
	testutil.CommitAll(t, upstream, "upstream")
	repo := initDownstream(t)
	testutil.WriteFile(t, repo, "tracked.txt", "tracked base\n")
	testutil.Git(t, repo, "add", "tracked.txt")
	testutil.Git(t, repo, "commit", "-m", "add unrelated tracked file")

	testutil.WriteFile(t, repo, "staged.txt", "staged content\n")
	testutil.Git(t, repo, "add", "staged.txt")
	testutil.WriteFile(t, repo, "tracked.txt", "tracked dirty\n")
	testutil.WriteFile(t, repo, "untracked.txt", "untracked content\n")

	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	changed := strings.Fields(testutil.Git(t, repo, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Stdout)
	wantChanged := []string{".braids.json", "vendor/basic/README.md"}
	if strings.Join(changed, "\n") != strings.Join(wantChanged, "\n") {
		t.Fatalf("Braid commit changed %#v, want %#v", changed, wantChanged)
	}
	if got := strings.TrimSpace(testutil.Git(t, repo, "show", ":staged.txt").Stdout); got != "staged content" {
		t.Fatalf("staged blob = %q, want staged content", got)
	}
	assertFile(t, repo, "tracked.txt", "tracked dirty\n")
	assertFile(t, repo, "untracked.txt", "untracked content\n")
	status := testutil.Git(t, repo, "status", "--porcelain").Stdout
	assertContains(t, status, "A  staged.txt")
	assertContains(t, status, " M tracked.txt")
	assertContains(t, status, "?? untracked.txt")
}

func TestAddCommandNoCommitStagesContentConfigAndPreservesUnrelatedState(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "hello from upstream\n")
	revision := testutil.CommitAll(t, upstream, "upstream")
	repo := initDownstream(t)
	testutil.WriteFile(t, repo, "tracked.txt", "tracked base\n")
	testutil.Git(t, repo, "add", "tracked.txt")
	testutil.Git(t, repo, "commit", "-m", "add unrelated tracked file")
	head := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD").Stdout)

	testutil.WriteFile(t, repo, "staged.txt", "staged content\n")
	testutil.Git(t, repo, "add", "staged.txt")
	testutil.WriteFile(t, repo, "tracked.txt", "tracked dirty\n")
	testutil.WriteFile(t, repo, "untracked.txt", "untracked content\n")

	stdout, _ := runCommandOKWithOutput(t, repo, []string{"add", upstream, "vendor/basic", "--no-commit"})

	assertInOrder(t, stdout, unrelatedStagedWarning, "Braid: staged add of mirror 'vendor/basic'")
	if got := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD").Stdout); got != head {
		t.Fatalf("HEAD = %s, want unchanged %s", got, head)
	}
	assertFile(t, repo, "vendor/basic/README.md", "hello from upstream\n")
	m := loadMirror(t, repo, "vendor/basic")
	if m.URL != upstream || m.Branch != "main" || m.Revision != revision {
		t.Fatalf("mirror = %#v, want staged upstream main at %s", m, revision)
	}
	cached := strings.Fields(testutil.Git(t, repo, "diff", "--cached", "--name-only").Stdout)
	wantCached := []string{".braids.json", "staged.txt", "vendor/basic/README.md"}
	if strings.Join(cached, "\n") != strings.Join(wantCached, "\n") {
		t.Fatalf("cached names = %#v, want %#v", cached, wantCached)
	}
	if unstaged := strings.TrimSpace(testutil.Git(t, repo, "diff", "--name-only", "--", ".braids.json", "vendor/basic").Stdout); unstaged != "" {
		t.Fatalf("unstaged Braid-owned paths = %q, want none", unstaged)
	}
	if got := strings.TrimSpace(testutil.Git(t, repo, "show", ":staged.txt").Stdout); got != "staged content" {
		t.Fatalf("staged blob = %q, want staged content", got)
	}
	assertFile(t, repo, "tracked.txt", "tracked dirty\n")
	assertFile(t, repo, "untracked.txt", "untracked content\n")
	assertNoRemote(t, repo, m.Remote())
}

func TestAddCommandNoCommitQuietSuppressesSuccess(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "quiet\n")
	testutil.CommitAll(t, upstream, "upstream")
	repo := initDownstream(t)

	stdout, stderr := runCommandOKWithOutput(t, repo, []string{"--quiet", "add", upstream, "vendor/basic", "--no-commit"})

	if stdout != "" || stderr != "" {
		t.Fatalf("stdout/stderr = %q / %q, want quiet no-commit output empty", stdout, stderr)
	}
	if cached := strings.Fields(testutil.Git(t, repo, "diff", "--cached", "--name-only").Stdout); strings.Join(cached, "\n") != ".braids.json\nvendor/basic/README.md" {
		t.Fatalf("cached names = %#v, want staged config and mirror", cached)
	}
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

func TestAddCommandFromSubdirectoryNormalizesLocalPaths(t *testing.T) {
	upstreamExplicit := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamExplicit, "README.md", "explicit\n")
	explicitRevision := testutil.CommitAll(t, upstreamExplicit, "explicit")
	upstreamDefault := filepath.Join(t.TempDir(), "default-upstream")
	testutil.Git(t, filepath.Dir(upstreamDefault), "init", "--initial-branch=main", upstreamDefault)
	testutil.Git(t, upstreamDefault, "config", "--local", "user.name", testutil.DefaultName)
	testutil.Git(t, upstreamDefault, "config", "--local", "user.email", testutil.DefaultEmail)
	testutil.Git(t, upstreamDefault, "config", "--local", "commit.gpgsign", "false")
	testutil.WriteFile(t, upstreamDefault, "README.md", "default\n")
	defaultRevision := testutil.CommitAll(t, upstreamDefault, "default")

	repo := initDownstream(t)
	workDir := filepath.Join(repo, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	runCommandOKInDir(t, repo, workDir, []string{"add", upstreamExplicit, "vendor/basic"})
	runCommandOKInDir(t, repo, workDir, []string{"add", upstreamDefault})

	assertFile(t, repo, "apps/web/vendor/basic/README.md", "explicit\n")
	assertFile(t, repo, "apps/web/default-upstream/README.md", "default\n")
	if got := loadMirror(t, repo, "apps/web/vendor/basic").Revision; got != explicitRevision {
		t.Fatalf("explicit revision = %q, want %q", got, explicitRevision)
	}
	if got := loadMirror(t, repo, "apps/web/default-upstream").Revision; got != defaultRevision {
		t.Fatalf("default revision = %q, want %q", got, defaultRevision)
	}
}

func TestAddCommandGlobalVerboseTracesWorktreeAndCacheGit(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "trace\n")
	testutil.CommitAll(t, upstream, "upstream")

	repo := initDownstream(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAID_GLOBAL_CACHE_DIR", filepath.Join(t.TempDir(), "braid-cache"))
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

func TestAddCommandScopedPrecheckBlocksDirtyConfig(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "hello\n")
	testutil.CommitAll(t, upstream, "upstream")
	repo := initDownstream(t)
	cfg := config.Empty()
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("write config: %v", err)
	}
	testutil.Git(t, repo, "add", config.FileName)
	testutil.Git(t, repo, "commit", "-m", "add empty braid config")
	testutil.WriteFile(t, repo, config.FileName, "{\"config_version\":1,\"mirrors\":{}}\n")

	stderr := runCommandError(t, repo, []string{"add", upstream, "vendor/basic"})
	assertContains(t, stderr, "local changes are present in .braids.json")
}

func TestAddCommandScopedPrecheckRunsBeforeDefaultBranchLookup(t *testing.T) {
	repo := initDownstream(t)
	cfg := config.Empty()
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("write config: %v", err)
	}
	testutil.Git(t, repo, "add", config.FileName)
	testutil.Git(t, repo, "commit", "-m", "add empty braid config")
	testutil.WriteFile(t, repo, config.FileName, "{\"config_version\":1,\"mirrors\":{}}\n")

	missingUpstream := filepath.Join(t.TempDir(), "missing-upstream")
	stderr := runCommandError(t, repo, []string{"add", missingUpstream, "vendor/basic"})
	assertContains(t, stderr, "local changes are present in .braids.json")
	if strings.Contains(stderr, "failed to detect default branch") || strings.Contains(stderr, "ls-remote") {
		t.Fatalf("stderr = %q, want local scoped precheck before default branch lookup", stderr)
	}
}

func TestAddCommandScopedPrecheckBlocksUnavailableTargets(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, repo string)
		wantErr string
	}{
		{
			name: "clean tracked target file",
			prepare: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor/basic", "tracked\n")
				testutil.Git(t, repo, "add", "vendor/basic")
				testutil.Git(t, repo, "commit", "-m", "tracked target")
			},
			wantErr: `add target path "vendor/basic" already exists in git index`,
		},
		{
			name: "clean tracked descendant",
			prepare: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor/basic/existing.txt", "tracked\n")
				testutil.Git(t, repo, "add", "vendor/basic/existing.txt")
				testutil.Git(t, repo, "commit", "-m", "tracked descendant")
			},
			wantErr: `add target path "vendor/basic" already exists in git index`,
		},
		{
			name: "clean tracked ancestor file",
			prepare: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor", "tracked\n")
				testutil.Git(t, repo, "add", "vendor")
				testutil.Git(t, repo, "commit", "-m", "tracked ancestor")
			},
			wantErr: `add target path "vendor/basic" is blocked by existing git index path "vendor"`,
		},
		{
			name: "untracked target content",
			prepare: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor/basic/untracked.txt", "untracked\n")
			},
			wantErr: "local changes are present in vendor/basic",
		},
		{
			name: "untracked ancestor file",
			prepare: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor", "untracked\n")
			},
			wantErr: `add target path "vendor/basic" is blocked by worktree path "vendor"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			testutil.WriteFile(t, upstream, "README.md", "hello\n")
			testutil.CommitAll(t, upstream, "upstream")
			repo := initDownstream(t)
			test.prepare(t, repo)

			stderr := runCommandError(t, repo, []string{"add", upstream, "vendor/basic"})
			assertContains(t, stderr, test.wantErr)
		})
	}
}

func TestAddCommandRejectsMirrorPathOverlappingConfig(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "hello\n")
	testutil.CommitAll(t, upstream, "upstream")
	repo := initDownstream(t)

	stderr := runCommandError(t, repo, []string{"add", upstream, ".braids.json"})
	assertContains(t, stderr, `mirror path ".braids.json" overlaps .braids.json`)
}

func TestAddCommandBlocksUnresolvedGitOperationBeforeScopedStatus(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "hello\n")
	testutil.CommitAll(t, upstream, "upstream")
	repo := initDownstream(t)
	if err := os.WriteFile(filepath.Join(repo, ".git", "MERGE_HEAD"), []byte("abc123\n"), 0o644); err != nil {
		t.Fatalf("write MERGE_HEAD: %v", err)
	}

	stderr := runCommandError(t, repo, []string{"add", upstream, "vendor/basic"})
	assertContains(t, stderr, "unresolved git operation state is present: MERGE_HEAD")
}

func TestAddCommandTags(t *testing.T) {
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
		for _, mode := range []string{"no-cache", "global-cache"} {
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				upstream := testutil.InitRepo(t)
				revision := test.make(t, upstream, test.tag)
				repo := initDownstream(t)
				localPath := "vendor/" + test.name
				args := []string{"--no-cache", "add", upstream, localPath, "--tag", test.tag}
				if mode == "global-cache" {
					args = []string{"--global-cache-dir", t.TempDir(), "add", upstream, localPath, "--tag", test.tag}
				}
				runCommandOK(t, repo, args)

				assertFile(t, repo, localPath+"/README.md", test.name+"\n")
				m := loadMirror(t, repo, localPath)
				if m.Tag != test.tag || m.Branch != "" || m.Revision != revision {
					t.Fatalf("mirror = %#v, want tag %q at %s", m, test.tag, revision)
				}
				git := gitexec.New(repo, false, nil)
				assertRefMissing(t, context.Background(), git, "refs/tags/"+test.tag)
				assertRefMissing(t, context.Background(), git, m.LocalRef())
				assertClean(t, repo)
			})
		}
	}
}

func TestAddTagMirrorPreservesSameNamedDownstreamTag(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "upstream\n")
	testutil.CommitAll(t, upstream, "upstream")
	testutil.Git(t, upstream, "tag", "v1")

	repo := initDownstream(t)
	testutil.Git(t, repo, "tag", "-a", "v1", "-m", "downstream tag")
	downstreamTag := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "refs/tags/v1").Stdout)

	runCommandOK(t, repo, []string{"--no-cache", "add", upstream, "vendor/tagged", "--tag", "v1"})

	assertRefObjectID(t, context.Background(), gitexec.New(repo, false, nil), "refs/tags/v1", downstreamTag)
}

func TestAddCommandMissingUpstreamPathCleansUpTemporaryRemote(t *testing.T) {
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

func TestAddCommandReportsPostCommitRestoreFailure(t *testing.T) {
	repo := initDownstream(t)
	git := &fakeAddGit{restoreErr: errors.New("restore failed")}
	err := AddHandler{Options: Options{WorkDir: repo, ConfigRoot: repo}}.add(
		context.Background(),
		testRepoContext(repo, git),
		git,
		cli.Invocation{
			Global: cli.GlobalOptions{NoCache: true},
			Add:    cli.AddOptions{URL: "https://example.invalid/upstream.git", LocalPath: "vendor/basic", Branch: "main"},
		},
		progressReporter{},
		new(bytes.Buffer),
		new(bytes.Buffer),
	)
	if err == nil || !strings.Contains(err.Error(), "restore failed") {
		t.Fatalf("add error = %v, want restore failure", err)
	}
	if !git.committed {
		t.Fatal("CommitTreeWithTemporaryIndex was not called before restore failure")
	}
}

func TestAddCommandReportsPostCommitRemoteCleanupFailure(t *testing.T) {
	repo := initDownstream(t)
	git := &fakeAddGit{remoteRemoveErr: errors.New("remove remote failed")}
	err := AddHandler{Options: Options{WorkDir: repo, ConfigRoot: repo}}.add(
		context.Background(),
		testRepoContext(repo, git),
		git,
		cli.Invocation{
			Global: cli.GlobalOptions{NoCache: true},
			Add:    cli.AddOptions{URL: "https://example.invalid/upstream.git", LocalPath: "vendor/basic", Branch: "main"},
		},
		progressReporter{},
		new(bytes.Buffer),
		new(bytes.Buffer),
	)
	if err == nil || !strings.Contains(err.Error(), `add committed but failed to remove temporary remote "main_braid_vendor_basic"`) || !strings.Contains(err.Error(), "remove remote failed") {
		t.Fatalf("add error = %v, want post-commit remote cleanup failure", err)
	}
	if !git.committed {
		t.Fatal("CommitTreeWithTemporaryIndex was not called before remote cleanup failure")
	}
}

func TestAddCommandNoCommitReportsPostStageRemoteCleanupFailure(t *testing.T) {
	repo := initDownstream(t)
	git := &fakeAddGit{remoteRemoveErr: errors.New("remove remote failed")}
	err := AddHandler{Options: Options{WorkDir: repo, ConfigRoot: repo}}.add(
		context.Background(),
		testRepoContext(repo, git),
		git,
		cli.Invocation{
			Global: cli.GlobalOptions{NoCache: true},
			Add:    cli.AddOptions{URL: "https://example.invalid/upstream.git", LocalPath: "vendor/basic", Branch: "main", NoCommit: true},
		},
		progressReporter{},
		new(bytes.Buffer),
		new(bytes.Buffer),
	)
	if err == nil || !strings.Contains(err.Error(), `add staged changes but failed to remove temporary remote "main_braid_vendor_basic"`) || !strings.Contains(err.Error(), "remove remote failed") {
		t.Fatalf("add error = %v, want post-stage remote cleanup failure", err)
	}
	if git.committed {
		t.Fatal("CommitTreeWithTemporaryIndex was called for no-commit add")
	}
}

func TestAddCommandNoCommitReportsStagingFailure(t *testing.T) {
	repo := initDownstream(t)
	git := &fakeAddGit{restoreTreeErr: errors.New("stage failed")}
	err := AddHandler{Options: Options{WorkDir: repo, ConfigRoot: repo}}.add(
		context.Background(),
		testRepoContext(repo, git),
		git,
		cli.Invocation{
			Global: cli.GlobalOptions{NoCache: true},
			Add:    cli.AddOptions{URL: "https://example.invalid/upstream.git", LocalPath: "vendor/basic", Branch: "main", NoCommit: true},
		},
		progressReporter{},
		new(bytes.Buffer),
		new(bytes.Buffer),
	)
	if err == nil || !strings.Contains(err.Error(), "stage failed") {
		t.Fatalf("add error = %v, want staging failure", err)
	}
	if git.committed {
		t.Fatal("CommitTreeWithTemporaryIndex was called for no-commit add")
	}
}

type fakeAddGit struct {
	remoteRemoveErr error
	restoreErr      error
	restoreTreeErr  error
	committed       bool
}

func (f *fakeAddGit) RequireVersion(context.Context, string) error { return nil }
func (f *fakeAddGit) IsInsideWorkTree(context.Context) (bool, error) {
	return true, nil
}
func (f *fakeAddGit) RelativeWorkingDir(context.Context) (string, error) { return "", nil }
func (f *fakeAddGit) WorkTreeRoot(context.Context) (string, error)       { return ".", nil }
func (f *fakeAddGit) RemoteURL(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (f *fakeAddGit) RemoteAdd(context.Context, string, string) error { return nil }
func (f *fakeAddGit) RemoteRemove(context.Context, string) error {
	return f.remoteRemoveErr
}
func (f *fakeAddGit) RevParse(context.Context, string) (string, error) {
	return "abcdef1234567890", nil
}
func (f *fakeAddGit) LsRemote(context.Context, ...string) (string, error) {
	return "ref: refs/heads/main\tHEAD\n", nil
}
func (f *fakeAddGit) Fetch(context.Context, ...string) error { return nil }
func (f *fakeAddGit) LsTreeItem(context.Context, string, string) (gitexec.TreeItem, error) {
	return gitexec.TreeItem{Mode: "100644", Type: "blob", Hash: "blob"}, nil
}
func (f *fakeAddGit) LsFiles(context.Context, string) (string, error) {
	return "", nil
}
func (f *fakeAddGit) StatusPorcelainPathspecs(context.Context, ...string) (string, error) {
	return "", nil
}
func (f *fakeAddGit) BlockingOperation(context.Context) (string, bool, error) {
	return "", false, nil
}
func (f *fakeAddGit) Diff(context.Context, ...string) (string, error) {
	return "", nil
}
func (f *fakeAddGit) HashBytes(context.Context, []byte) (gitexec.TreeItem, error) {
	return gitexec.TreeItem{Mode: "100644", Type: "blob", Hash: "config"}, nil
}
func (f *fakeAddGit) MakeTreeWithItemIn(context.Context, string, string, gitexec.TreeItem) (string, error) {
	return "tree", nil
}
func (f *fakeAddGit) CommitTreeWithTemporaryIndex(context.Context, string, string) (bool, error) {
	f.committed = true
	return true, nil
}
func (f *fakeAddGit) RestorePathspecsFromHead(context.Context, ...string) error {
	return f.restoreErr
}
func (f *fakeAddGit) RestorePathspecsFromTree(context.Context, string, bool, bool, ...string) error {
	return f.restoreTreeErr
}

var _ AddGit = (*fakeAddGit)(nil)
