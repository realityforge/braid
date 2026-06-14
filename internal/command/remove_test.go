package command

import (
	"os"
	"path/filepath"
	"testing"

	"braid/internal/config"
	"braid/internal/testutil"
)

func TestRemoveCommandDeletesContentConfigAndRemote(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	runCommandOK(t, repo, []string{"setup", "vendor/basic"})
	remote := loadMirror(t, repo, "vendor/basic").Remote()
	writeFailingPreCommitHook(t, repo)

	runCommandOK(t, repo, []string{"remove", "vendor/basic"})
	assertPathMissing(t, repo, "vendor/basic")
	assertMirrorMissing(t, repo, "vendor/basic")
	assertNoRemote(t, repo, remote)
	assertCommitSubject(t, repo, "Braid: Remove mirror 'vendor/basic'")
	assertClean(t, repo)
}

func TestRemoveCommandKeepsRemote(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	testutil.CommitAll(t, upstream, "base")

	repo := initDownstream(t)
	runCommandOK(t, repo, []string{"add", upstream, "vendor/basic"})
	runCommandOK(t, repo, []string{"setup", "vendor/basic"})
	remote := loadMirror(t, repo, "vendor/basic").Remote()

	runCommandOK(t, repo, []string{"remove", "vendor/basic", "--keep"})
	remotes := testutil.Git(t, repo, "remote").Stdout
	assertContains(t, remotes, remote)
	assertMirrorMissing(t, repo, "vendor/basic")
	assertClean(t, repo)
}

func TestRemoveCommandPathVariants(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(t *testing.T, upstream string)
		addArgs   func(upstream string) []string
		localPath string
	}{
		{
			name: "subdirectory",
			prepare: func(t *testing.T, upstream string) {
				testutil.WriteFile(t, upstream, "lib/component.txt", "component\n")
			},
			addArgs:   func(upstream string) []string { return []string{"add", upstream, "vendor/lib", "--path", "lib"} },
			localPath: "vendor/lib",
		},
		{
			name: "path with spaces",
			prepare: func(t *testing.T, upstream string) {
				testutil.WriteFile(t, upstream, "README.md", "spaces\n")
			},
			addArgs:   func(upstream string) []string { return []string{"add", upstream, "vendor/path with spaces"} },
			localPath: "vendor/path with spaces",
		},
		{
			name: "single file",
			prepare: func(t *testing.T, upstream string) {
				testutil.WriteFile(t, upstream, "LICENSE.txt", "license\n")
			},
			addArgs: func(upstream string) []string {
				return []string{"add", upstream, "licenses/THIRD_PARTY.txt", "--path", "LICENSE.txt"}
			},
			localPath: "licenses/THIRD_PARTY.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.InitRepo(t)
			test.prepare(t, upstream)
			testutil.CommitAll(t, upstream, test.name)
			repo := initDownstream(t)
			runCommandOK(t, repo, test.addArgs(upstream))

			runCommandOK(t, repo, []string{"remove", test.localPath})
			assertPathMissing(t, repo, test.localPath)
			assertMirrorMissing(t, repo, test.localPath)
			assertClean(t, repo)
		})
	}
}

func assertPathMissing(t *testing.T, repo, relativePath string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(repo, filepath.FromSlash(relativePath)))
	if !os.IsNotExist(err) {
		t.Fatalf("%s exists after remove, stat err = %v", relativePath, err)
	}
}

func assertMirrorMissing(t *testing.T, repo, localPath string) {
	t.Helper()
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if _, ok := cfg.Get(localPath); ok {
		t.Fatalf("mirror %q still exists in config", localPath)
	}
}
