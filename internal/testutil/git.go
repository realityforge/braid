package testutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"braid/internal/gitexec"
)

const (
	DefaultName  = "Braid Test"
	DefaultEmail = "braid-test@example.invalid"
)

func InitRepo(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	Git(t, dir, "init", "--initial-branch=main")
	Git(t, dir, "config", "--local", "user.name", DefaultName)
	Git(t, dir, "config", "--local", "user.email", DefaultEmail)
	Git(t, dir, "config", "--local", "commit.gpgsign", "false")
	return dir
}

func Git(t testing.TB, dir string, args ...string) gitexec.Result {
	t.Helper()
	result, err := gitexec.Runner{WorkDir: dir}.RunOK(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %v failed in %s: %v\nstdout:\n%s\nstderr:\n%s", args, dir, err, result.Stdout, result.Stderr)
	}
	return result
}

func WriteFile(t testing.TB, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", relativePath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relativePath, err)
	}
}

func CommitAll(t testing.TB, repo, message string) string {
	t.Helper()
	Git(t, repo, "add", ".")
	Git(t, repo, "commit", "-m", message)
	return CurrentRevision(t, repo)
}

func CurrentRevision(t testing.TB, repo string) string {
	t.Helper()
	result := Git(t, repo, "rev-parse", "HEAD")
	return trimTrailingNewline(result.Stdout)
}

func CopyDir(t testing.TB, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(dst, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatalf("copy fixture %s to %s: %v", src, dst, err)
	}
}

func trimTrailingNewline(value string) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r') {
		value = value[:len(value)-1]
	}
	return value
}
