package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/config"
	"braid/internal/mirror"
	"braid/internal/testutil"
)

func TestUpdateCommandFastForwardsAndUsesNoVerify(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	writeFailingPreCommitHook(t, repo)
	writePostCommitHook(t, repo)

	testutil.WriteFile(t, upstream, "README.md", "updated\n")
	revision := testutil.CommitAll(t, upstream, "updated")
	runCommandOK(t, repo, []string{"update", "vendor/basic"})

	assertFile(t, repo, "vendor/basic/README.md", "updated\n")
	m := loadMirror(t, repo, "vendor/basic")
	if m.Revision != revision {
		t.Fatalf("revision = %q, want %q", m.Revision, revision)
	}
	assertCommitSubject(t, repo, "Braid: Update mirror 'vendor/basic' to '"+revision[:7]+"'")
	assertFile(t, repo, "post-commit-ran", "ran\n")

	head := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD").Stdout)
	runCommandOK(t, repo, []string{"update", "vendor/basic"})
	gotHead := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD").Stdout)
	if gotHead != head {
		t.Fatalf("up-to-date update created commit %s, want HEAD %s", gotHead, head)
	}
}

func TestUpdateCommandPreservesUnrelatedIndexAndWorktreeState(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "tracked.txt", "tracked base\n")
	testutil.Git(t, repo, "add", "tracked.txt")
	testutil.Git(t, repo, "commit", "-m", "add unrelated tracked file")

	testutil.WriteFile(t, repo, "staged.txt", "staged content\n")
	testutil.Git(t, repo, "add", "staged.txt")
	testutil.WriteFile(t, repo, "tracked.txt", "tracked dirty\n")
	testutil.WriteFile(t, repo, "untracked.txt", "untracked content\n")

	testutil.WriteFile(t, upstream, "README.md", "updated\n")
	testutil.CommitAll(t, upstream, "updated")
	runCommandOK(t, repo, []string{"update", "vendor/basic"})

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

func TestUpdateCommandRepresentsMirrorDeletesAndRenames(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "keep.txt", "keep\n")
	testutil.WriteFile(t, upstream, "remove.txt", "remove\n")
	testutil.WriteFile(t, upstream, "old.txt", "old\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

	if err := os.Remove(filepath.Join(upstream, "remove.txt")); err != nil {
		t.Fatalf("remove upstream file: %v", err)
	}
	if err := os.Rename(filepath.Join(upstream, "old.txt"), filepath.Join(upstream, "new.txt")); err != nil {
		t.Fatalf("rename upstream file: %v", err)
	}
	testutil.CommitAll(t, upstream, "delete and rename")
	runCommandOK(t, repo, []string{"update", "vendor/basic"})

	if _, err := os.Stat(filepath.Join(repo, "vendor/basic/remove.txt")); !os.IsNotExist(err) {
		t.Fatalf("remove.txt stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "vendor/basic/old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old.txt stat error = %v, want not exist", err)
	}
	assertFile(t, repo, "vendor/basic/new.txt", "old\n")
	assertFile(t, repo, "vendor/basic/keep.txt", "keep\n")
}

func TestUpdateCommandScopedPrecheckBlocksDirtyConfigAndTarget(t *testing.T) {
	tests := []struct {
		name    string
		dirty   func(t *testing.T, repo string)
		wantErr string
	}{
		{
			name: "config",
			dirty: func(t *testing.T, repo string) {
				t.Helper()
				path := filepath.Join(repo, config.FileName)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read config: %v", err)
				}
				if err := os.WriteFile(path, append(data, []byte(" \n")...), 0o644); err != nil {
					t.Fatalf("dirty config: %v", err)
				}
			},
			wantErr: "local changes are present in .braids.json",
		},
		{
			name: "staged mirror change",
			dirty: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor/basic/README.md", "staged\n")
				testutil.Git(t, repo, "add", "vendor/basic/README.md")
			},
			wantErr: "local changes are present in vendor/basic",
		},
		{
			name: "unstaged mirror change",
			dirty: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor/basic/README.md", "unstaged\n")
			},
			wantErr: "local changes are present in vendor/basic",
		},
		{
			name: "untracked mirror change",
			dirty: func(t *testing.T, repo string) {
				t.Helper()
				testutil.WriteFile(t, repo, "vendor/basic/untracked.txt", "untracked\n")
			},
			wantErr: "local changes are present in vendor/basic",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			testutil.WriteFile(t, upstream, "README.md", "base\n")
			testutil.CommitAll(t, upstream, "base")
			repo := initDownstream(t)
			runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})

			test.dirty(t, repo)
			stderr := runCommandError(t, repo, []string{"update", "vendor/basic"})
			assertContains(t, stderr, test.wantErr)
		})
	}
}

func TestUpdateCommandBlocksUnresolvedGitOperationBeforeScopedStatus(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	if err := os.WriteFile(filepath.Join(repo, ".git", "MERGE_HEAD"), []byte("abc123\n"), 0o644); err != nil {
		t.Fatalf("write MERGE_HEAD: %v", err)
	}

	stderr := runCommandError(t, repo, []string{"update", "vendor/basic"})
	assertContains(t, stderr, "unresolved git operation state is present: MERGE_HEAD")
}

func TestUpdateCommandIgnoresDirtyNonTargetMirrorForNoOpPathUpdate(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})
	testutil.WriteFile(t, repo, "vendor/b/README.md", "dirty other mirror\n")

	runCommandOK(t, repo, []string{"update", "vendor/a"})
	assertFile(t, repo, "vendor/b/README.md", "dirty other mirror\n")
}

func TestUpdateCommandAllPrechecksEligibleMirrorsBeforeUpdating(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	aBase := testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	bBase := testutil.CommitAll(t, upstreamB, "b base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})
	testutil.WriteFile(t, upstreamA, "README.md", "a updated\n")
	testutil.CommitAll(t, upstreamA, "a updated")
	testutil.WriteFile(t, upstreamB, "README.md", "b updated\n")
	testutil.CommitAll(t, upstreamB, "b updated")
	testutil.WriteFile(t, repo, "vendor/b/README.md", "dirty b\n")

	stderr := runCommandError(t, repo, []string{"update"})
	assertContains(t, stderr, "local changes are present in vendor/b")
	if got := loadMirror(t, repo, "vendor/a").Revision; got != aBase {
		t.Fatalf("vendor/a revision = %q, want unchanged %q", got, aBase)
	}
	if got := loadMirror(t, repo, "vendor/b").Revision; got != bBase {
		t.Fatalf("vendor/b revision = %q, want unchanged %q", got, bBase)
	}
	if remotes := strings.TrimSpace(testutil.Git(t, repo, "remote").Stdout); remotes != "" {
		t.Fatalf("remotes = %q, want no setup side effects", remotes)
	}
}

func TestUpdateCommandAllSkipsLockedMirrorsBeforeScopedPrecheck(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamLocked := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamLocked, "README.md", "locked base\n")
	lockedRevision := testutil.CommitAll(t, upstreamLocked, "locked base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamLocked, "vendor/locked", "--revision", lockedRevision})
	testutil.WriteFile(t, repo, "vendor/locked/README.md", "dirty locked\n")

	runCommandOK(t, repo, []string{"update"})
	assertFile(t, repo, "vendor/locked/README.md", "dirty locked\n")
}

func TestUpdateCommandRejectsMirrorPathOverlappingConfig(t *testing.T) {
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

	stderr := runCommandError(t, repo, []string{"update", ".braids.json"})
	assertContains(t, stderr, `mirror path ".braids.json" overlaps .braids.json`)
}

func TestUpdateCommandMirrorVariants(t *testing.T) {
	tests := []struct {
		name       string
		addArgs    func(upstream, baseRevision string) []string
		updateArgs func(upstream, nextRevision string) []string
		prepare    func(t *testing.T, upstream string) (string, string)
		localPath  string
		wantFile   string
		wantText   string
	}{
		{
			name:      "revision",
			localPath: "vendor/revision",
			prepare: func(t *testing.T, upstream string) (string, string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "base\n")
				base := testutil.CommitAll(t, upstream, "base")
				testutil.WriteFile(t, upstream, "README.md", "revision\n")
				next := testutil.CommitAll(t, upstream, "revision")
				return base, next
			},
			addArgs: func(upstream, base string) []string {
				return []string{"add", upstream, "vendor/revision", "--revision", base}
			},
			updateArgs: func(_ string, next string) []string { return []string{"update", "vendor/revision", "--revision", next} },
			wantFile:   "vendor/revision/README.md",
			wantText:   "revision\n",
		},
		{
			name:      "subdirectory",
			localPath: "vendor/lib",
			prepare: func(t *testing.T, upstream string) (string, string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "lib/component.txt", "base\n")
				base := testutil.CommitAll(t, upstream, "base")
				testutil.WriteFile(t, upstream, "lib/component.txt", "subdir\n")
				next := testutil.CommitAll(t, upstream, "subdir")
				return base, next
			},
			addArgs:    func(upstream, _ string) []string { return []string{"add", upstream, "vendor/lib", "--path", "lib"} },
			updateArgs: func(_ string, _ string) []string { return []string{"update", "vendor/lib"} },
			wantFile:   "vendor/lib/component.txt",
			wantText:   "subdir\n",
		},
		{
			name:      "path with spaces",
			localPath: "vendor/path with spaces",
			prepare: func(t *testing.T, upstream string) (string, string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "base\n")
				base := testutil.CommitAll(t, upstream, "base")
				testutil.WriteFile(t, upstream, "README.md", "spaces\n")
				next := testutil.CommitAll(t, upstream, "spaces")
				return base, next
			},
			addArgs:    func(upstream, _ string) []string { return []string{"add", upstream, "vendor/path with spaces"} },
			updateArgs: func(_ string, _ string) []string { return []string{"update", "vendor/path with spaces"} },
			wantFile:   "vendor/path with spaces/README.md",
			wantText:   "spaces\n",
		},
		{
			name:      "single file",
			localPath: "licenses/THIRD_PARTY.txt",
			prepare: func(t *testing.T, upstream string) (string, string) {
				t.Helper()
				testutil.WriteFile(t, upstream, "LICENSE.txt", "base\n")
				base := testutil.CommitAll(t, upstream, "base")
				testutil.WriteFile(t, upstream, "LICENSE.txt", "single\n")
				next := testutil.CommitAll(t, upstream, "single")
				return base, next
			},
			addArgs: func(upstream, _ string) []string {
				return []string{"add", upstream, "licenses/THIRD_PARTY.txt", "--path", "LICENSE.txt"}
			},
			updateArgs: func(_ string, _ string) []string { return []string{"update", "licenses/THIRD_PARTY.txt"} },
			wantFile:   "licenses/THIRD_PARTY.txt",
			wantText:   "single\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			base, next := test.prepare(t, upstream)
			repo := initDownstream(t)
			runCommandOK(t, repo, test.addArgs(upstream, base))
			runCommandOK(t, repo, test.updateArgs(upstream, next))

			assertFile(t, repo, test.wantFile, test.wantText)
			m := loadMirror(t, repo, test.localPath)
			if m.Revision != next {
				t.Fatalf("revision = %q, want %q", m.Revision, next)
			}
		})
	}
}

func TestUpdateCommandNoCacheTags(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		move func(t *testing.T, upstream, tag string) string
	}{
		{
			name: "lightweight",
			tag:  "v1-light",
			move: func(t *testing.T, upstream, tag string) string {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "lightweight updated\n")
				revision := testutil.CommitAll(t, upstream, "lightweight updated")
				testutil.Git(t, upstream, "tag", "-f", tag)
				return revision
			},
		},
		{
			name: "annotated",
			tag:  "v1-annotated",
			move: func(t *testing.T, upstream, tag string) string {
				t.Helper()
				testutil.WriteFile(t, upstream, "README.md", "annotated updated\n")
				revision := testutil.CommitAll(t, upstream, "annotated updated")
				testutil.Git(t, upstream, "tag", "-f", "-a", tag, "-m", "updated tag")
				return revision
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			testutil.WriteFile(t, upstream, "README.md", test.name+" base\n")
			testutil.CommitAll(t, upstream, "base")
			if test.name == "annotated" {
				testutil.Git(t, upstream, "tag", "-a", test.tag, "-m", "base tag")
			} else {
				testutil.Git(t, upstream, "tag", test.tag)
			}
			repo := initDownstream(t)
			localPath := "vendor/" + test.name
			runCommandOK(t, repo, []string{"--no-cache", "add", upstream, localPath, "--tag", test.tag})

			revision := test.move(t, upstream, test.tag)
			runCommandOK(t, repo, []string{"--no-cache", "update", localPath, "--tag", test.tag})
			assertFile(t, repo, localPath+"/README.md", test.name+" updated\n")
			if got := loadMirror(t, repo, localPath).Revision; got != revision {
				t.Fatalf("revision = %q, want %q", got, revision)
			}
		})
	}
}

func TestUpdateCommandAllSkipsLockedAndUsesSortedOrder(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")
	testutil.Git(t, upstreamB, "tag", "v1")
	upstreamZ := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamZ, "README.md", "z base\n")
	zBase := testutil.CommitAll(t, upstreamZ, "z base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b", "--tag", "v1"})
	runCommandOK(t, repo, []string{"add", upstreamZ, "vendor/z", "--revision", zBase})

	testutil.WriteFile(t, upstreamA, "README.md", "a updated\n")
	aRevision := testutil.CommitAll(t, upstreamA, "a updated")
	testutil.WriteFile(t, upstreamB, "README.md", "b updated\n")
	bRevision := testutil.CommitAll(t, upstreamB, "b updated")
	testutil.Git(t, upstreamB, "tag", "-f", "v1")
	testutil.WriteFile(t, upstreamZ, "README.md", "z updated\n")
	testutil.CommitAll(t, upstreamZ, "z updated")

	runCommandOK(t, repo, []string{"update"})
	if got := loadMirror(t, repo, "vendor/a").Revision; got != aRevision {
		t.Fatalf("vendor/a revision = %q, want %q", got, aRevision)
	}
	if got := loadMirror(t, repo, "vendor/b").Revision; got != bRevision {
		t.Fatalf("vendor/b revision = %q, want %q", got, bRevision)
	}
	if got := loadMirror(t, repo, "vendor/z").Revision; got != zBase {
		t.Fatalf("vendor/z revision = %q, want locked %q", got, zBase)
	}

	subjects := strings.Split(strings.TrimSpace(testutil.Git(t, repo, "log", "-2", "--pretty=%s").Stdout), "\n")
	if len(subjects) != 2 || !strings.Contains(subjects[0], "vendor/b") || !strings.Contains(subjects[1], "vendor/a") {
		t.Fatalf("last update subjects = %#v, want newest vendor/b then vendor/a", subjects)
	}
}

func TestUpdateCommandStopsAllOnFirstFailure(t *testing.T) {
	upstreamA := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamA, "README.md", "a base\n")
	testutil.CommitAll(t, upstreamA, "a base")
	upstreamB := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamB, "README.md", "b base\n")
	testutil.CommitAll(t, upstreamB, "b base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstreamA, "vendor/a"})
	runCommandOK(t, repo, []string{"add", upstreamB, "vendor/b"})
	bBase := loadMirror(t, repo, "vendor/b").Revision

	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	broken := cfg.Mirrors["vendor/a"]
	broken.RemotePath = "missing"
	if err := cfg.Update(broken); err != nil {
		t.Fatalf("Update config: %v", err)
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("Write config: %v", err)
	}
	testutil.Git(t, repo, "add", config.FileName)
	testutil.Git(t, repo, "commit", "-m", "break first mirror")

	testutil.WriteFile(t, upstreamA, "README.md", "a updated\n")
	testutil.CommitAll(t, upstreamA, "a updated")
	testutil.WriteFile(t, upstreamB, "README.md", "b updated\n")
	testutil.CommitAll(t, upstreamB, "b updated")
	stderr := runCommandError(t, repo, []string{"update"})
	assertContains(t, stderr, "update vendor/a")
	if got := loadMirror(t, repo, "vendor/b").Revision; got != bBase {
		t.Fatalf("vendor/b revision = %q, want unchanged %q", got, bBase)
	}
}

func TestUpdateCommandWritesMergeMessageOnConflict(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	testutil.WriteFile(t, repo, "vendor/basic/README.md", "local\n")
	testutil.CommitAll(t, repo, "local change")
	testutil.WriteFile(t, repo, "staged.txt", "staged content\n")
	testutil.Git(t, repo, "add", "staged.txt")

	testutil.WriteFile(t, upstream, "README.md", "remote\n")
	revision := testutil.CommitAll(t, upstream, "remote change")
	out := runCommandOK(t, repo, []string{"update", "vendor/basic"})
	assertContains(t, out, "CONFLICT (content): Merge conflict in vendor/basic/README.md")
	assertContains(t, out, "Braid: warning: unrelated staged changes are present")
	assertContains(t, out, "git commit -F .git/MERGE_MSG")

	data, err := os.ReadFile(filepath.Join(repo, "vendor", "basic", "README.md"))
	if err != nil {
		t.Fatalf("read conflicted file: %v", err)
	}
	assertContains(t, string(data), "<<<<<<<")
	assertContains(t, string(data), "local")
	assertContains(t, string(data), "remote")

	mergeMsg, err := os.ReadFile(filepath.Join(repo, ".git", "MERGE_MSG"))
	if err != nil {
		t.Fatalf("read MERGE_MSG: %v", err)
	}
	assertContains(t, string(mergeMsg), "Braid: Update mirror 'vendor/basic' to '"+revision[:7]+"'")
	if unmerged := strings.TrimSpace(testutil.Git(t, repo, "ls-files", "-u").Stdout); unmerged != "" {
		t.Fatalf("unmerged entries = %q, want none for marker fallback", unmerged)
	}
	if cached := strings.Fields(testutil.Git(t, repo, "diff", "--cached", "--name-only").Stdout); strings.Join(cached, "\n") != ".braids.json\nstaged.txt" {
		t.Fatalf("cached names = %#v, want .braids.json and staged.txt", cached)
	}
	if unstaged := strings.TrimSpace(testutil.Git(t, repo, "diff", "--name-only", "--", "vendor/basic").Stdout); unstaged != "vendor/basic/README.md" {
		t.Fatalf("unstaged mirror diff = %q, want conflicted README", unstaged)
	}
	if got := strings.TrimSpace(testutil.Git(t, repo, "show", ":staged.txt").Stdout); got != "staged content" {
		t.Fatalf("staged blob = %q, want staged content", got)
	}
}

func TestUpdateCommandSwitchesTrackingStrategy(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "tag", "v2")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	runCommandOK(t, repo, []string{"update", "vendor/basic", "--tag", "v2"})

	m := loadMirror(t, repo, "vendor/basic")
	if m.Tag != "v2" || m.Branch != "" {
		t.Fatalf("mirror tracking = branch %q tag %q, want tag v2", m.Branch, m.Tag)
	}
}
