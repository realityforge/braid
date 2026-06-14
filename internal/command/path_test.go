package command

import (
	"path/filepath"
	"testing"
)

func TestGitRepoOSPathNormalizesRelativeGitPaths(t *testing.T) {
	workDir := t.TempDir()

	tests := []struct {
		name    string
		gitPath string
		want    string
	}{
		{name: "slash path", gitPath: ".git/MERGE_MSG", want: filepath.Join(workDir, ".git", "MERGE_MSG")},
		{name: "backslash path", gitPath: `.git\MERGE_MSG`, want: filepath.Join(workDir, ".git", "MERGE_MSG")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := gitRepoOSPath(test.gitPath, workDir)
			if err != nil {
				t.Fatalf("gitRepoOSPath returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("gitRepoOSPath = %q, want %q", got, test.want)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("gitRepoOSPath = %q, want absolute path", got)
			}
		})
	}
}

func TestGitRepoOSPathPreservesAbsolutePath(t *testing.T) {
	workDir := t.TempDir()
	absolutePath := filepath.Join(workDir, ".git", "MERGE_MSG")

	got, err := gitRepoOSPath(absolutePath, t.TempDir())
	if err != nil {
		t.Fatalf("gitRepoOSPath returned error: %v", err)
	}
	if got != absolutePath {
		t.Fatalf("gitRepoOSPath = %q, want %q", got, absolutePath)
	}
}

func TestGitRepoOSPathRejectsEmptyPath(t *testing.T) {
	if _, err := gitRepoOSPath("", t.TempDir()); err == nil {
		t.Fatal("gitRepoOSPath returned nil error for empty path")
	}
}
