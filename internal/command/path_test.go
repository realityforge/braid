package command

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeLocalPathFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	repo := RepoContext{
		ProcessWorkDir:      filepath.Join(root, "apps", "web"),
		GitWorkTreeRoot:     root,
		LogicalWorkTreeRoot: root,
		WorkTreePrefix:      "apps/web",
	}

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "relative", value: "vendor/foo", want: "apps/web/vendor/foo"},
		{name: "trailing slash", value: "vendor/foo/", want: "apps/web/vendor/foo"},
		{name: "backslash", value: `vendor\foo`, want: "apps/web/vendor/foo"},
		{name: "parent remains inside", value: "../../vendor/foo", want: "vendor/foo"},
		{name: "dot at mirror root", value: ".", want: "apps/web"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeLocalPath(repo, test.value)
			if err != nil {
				t.Fatalf("normalizeLocalPath returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("normalizeLocalPath = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeLocalPathRejectsRootAndEscapes(t *testing.T) {
	root := t.TempDir()
	repo := RepoContext{
		ProcessWorkDir:      root,
		GitWorkTreeRoot:     root,
		LogicalWorkTreeRoot: root,
	}

	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "dot root", value: ".", wantErr: "worktree root"},
		{name: "empty", value: "", wantErr: "empty"},
		{name: "parent escapes", value: "../outside", wantErr: "escapes"},
		{name: "absolute outside", value: filepath.Join(t.TempDir(), "outside"), wantErr: "outside"},
		{name: "absolute root", value: root, wantErr: "worktree root"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeLocalPath(repo, test.value)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("normalizeLocalPath error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestNormalizeLocalPathAbsoluteInputs(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "real")
	logicalRoot := filepath.Join(parent, "link")
	repo := RepoContext{
		ProcessWorkDir:      filepath.Join(logicalRoot, "apps", "web"),
		GitWorkTreeRoot:     realRoot,
		LogicalWorkTreeRoot: logicalRoot,
		WorkTreePrefix:      "apps/web",
	}

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "under logical root", value: filepath.Join(logicalRoot, "apps", "web", "vendor", "foo"), want: "apps/web/vendor/foo"},
		{name: "under git root", value: filepath.Join(realRoot, "vendor", "foo"), want: "vendor/foo"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeLocalPath(repo, test.value)
			if err != nil {
				t.Fatalf("normalizeLocalPath returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("normalizeLocalPath = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeLocalPathInternalSymlinkMismatch(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "real")
	processDir := filepath.Join(parent, "link", "web")
	repo := RepoContext{
		ProcessWorkDir:  processDir,
		GitWorkTreeRoot: realRoot,
		WorkTreePrefix:  "apps/web",
	}

	got, err := normalizeLocalPath(repo, "vendor/foo")
	if err != nil {
		t.Fatalf("relative normalizeLocalPath returned error: %v", err)
	}
	if got != "apps/web/vendor/foo" {
		t.Fatalf("relative normalizeLocalPath = %q, want apps/web/vendor/foo", got)
	}

	_, err = normalizeLocalPath(repo, filepath.Join(processDir, "vendor", "foo"))
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("absolute symlink-spelled error = %v, want outside-worktree diagnostic", err)
	}
}

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
