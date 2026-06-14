package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/config"
	"braid/internal/testutil"
)

func TestUpdateCommandFastForwardsAndUsesNoVerify(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	writeFailingPreCommitHook(t, repo)

	testutil.WriteFile(t, upstream, "README.md", "updated\n")
	revision := testutil.CommitAll(t, upstream, "updated")
	runCommandOK(t, repo, []string{"update", "vendor/basic"})

	assertFile(t, repo, "vendor/basic/README.md", "updated\n")
	m := loadMirror(t, repo, "vendor/basic")
	if m.Revision != revision {
		t.Fatalf("revision = %q, want %q", m.Revision, revision)
	}
	assertCommitSubject(t, repo, "Braid: Update mirror 'vendor/basic' to '"+revision[:7]+"'")

	head := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD").Stdout)
	runCommandOK(t, repo, []string{"update", "vendor/basic"})
	gotHead := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD").Stdout)
	if gotHead != head {
		t.Fatalf("up-to-date update created commit %s, want HEAD %s", gotHead, head)
	}
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

	testutil.WriteFile(t, upstream, "README.md", "remote\n")
	revision := testutil.CommitAll(t, upstream, "remote change")
	runCommandOK(t, repo, []string{"update", "vendor/basic"})

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
