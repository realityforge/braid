package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitRepoConfiguresHermeticIdentity(t *testing.T) {
	repo := InitRepo(t)
	WriteFile(t, repo, "path with spaces/file.txt", "hello\n")
	revision := CommitAll(t, repo, "initial")

	if len(revision) != 40 {
		t.Fatalf("revision length = %d, want 40: %q", len(revision), revision)
	}

	identity := Git(t, repo, "log", "-1", "--pretty=%an <%ae>").Stdout
	if got, want := strings.TrimSpace(identity), DefaultName+" <"+DefaultEmail+">"; got != want {
		t.Fatalf("identity = %q, want %q", got, want)
	}

	gpgSign := Git(t, repo, "config", "--local", "commit.gpgsign").Stdout
	if got := strings.TrimSpace(gpgSign); got != "false" {
		t.Fatalf("commit.gpgsign = %q, want false", got)
	}
}

func TestCopyDirCopiesFixtureTree(t *testing.T) {
	src := filepath.Join(t.TempDir(), "fixture")
	WriteFile(t, src, "nested/file.txt", "content\n")
	WriteFile(t, src, "path with spaces/file.txt", "space\n")

	dst := filepath.Join(t.TempDir(), "dst")
	CopyDir(t, src, dst)

	for _, relative := range []string{"nested/file.txt", "path with spaces/file.txt"} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("copied file %s missing: %v", relative, err)
		}
	}
}
