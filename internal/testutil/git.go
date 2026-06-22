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

func InitRepo(tb testing.TB) string {
	tb.Helper()
	dir := tb.TempDir()
	Git(tb, dir, "init", "--initial-branch=main")
	Git(tb, dir, "config", "--local", "user.name", DefaultName)
	Git(tb, dir, "config", "--local", "user.email", DefaultEmail)
	Git(tb, dir, "config", "--local", "commit.gpgsign", "false")
	Git(tb, dir, "config", "--local", "core.autocrlf", "false")
	Git(tb, dir, "config", "--local", "core.eol", "lf")
	return dir
}

func Git(tb testing.TB, dir string, args ...string) gitexec.Result {
	tb.Helper()
	result, err := gitexec.Runner{WorkDir: dir}.RunOK(context.Background(), args...)
	if err != nil {
		tb.Fatalf("git %v failed in %s: %v\nstdout:\n%s\nstderr:\n%s", args, dir, err, result.Stdout, result.Stderr)
	}
	return result
}

func WriteFile(tb testing.TB, root, relativePath, content string) {
	tb.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		tb.Fatalf("create parent for %s: %v", relativePath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		tb.Fatalf("write %s: %v", relativePath, err)
	}
}

func CommitAll(tb testing.TB, repo, message string) string {
	tb.Helper()
	Git(tb, repo, "add", ".")
	Git(tb, repo, "commit", "-m", message)
	return CurrentRevision(tb, repo)
}

func CurrentRevision(tb testing.TB, repo string) string {
	tb.Helper()
	result := Git(tb, repo, "rev-parse", "HEAD")
	return trimTrailingNewline(result.Stdout)
}

func CopyDir(tb testing.TB, src, dst string) {
	tb.Helper()
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
		tb.Fatalf("copy fixture %s to %s: %v", src, dst, err)
	}
}

func trimTrailingNewline(value string) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r') {
		value = value[:len(value)-1]
	}
	return value
}
