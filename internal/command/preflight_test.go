package command

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/cli"
)

func TestRequirementsForPreflightMatrix(t *testing.T) {
	tests := []struct {
		command cli.Command
		want    Requirements
	}{
		{command: cli.CommandVersion, want: Requirements{}},
		{command: cli.CommandSetup, want: Requirements{Git: true, Root: true, Config: true}},
		{command: cli.CommandStatus, want: Requirements{Git: true, Root: true, Config: true}},
		{command: cli.CommandDiff, want: Requirements{Git: true, Root: true, Config: true}},
		{command: cli.CommandAdd, want: Requirements{Git: true, Root: true, MayWrite: true}},
		{command: cli.CommandUpdate, want: Requirements{Git: true, Root: true, Config: true, MayWrite: true}},
		{command: cli.CommandRemove, want: Requirements{Git: true, Root: true, Config: true, MayWrite: true}},
		{command: cli.CommandSync, want: Requirements{Git: true, Root: true, Config: true, MayWrite: true}},
		{command: cli.CommandPush, want: Requirements{Git: true, Root: true, Config: true}},
	}

	for _, test := range tests {
		if got := RequirementsFor(test.command); got != test.want {
			t.Fatalf("RequirementsFor(%s) = %#v, want %#v", test.command, got, test.want)
		}
	}
}

func TestVersionHelpAndUsageDoNotRunPreflight(t *testing.T) {
	git := &fakeGit{inside: false, prefix: "sub/"}
	app := NewAppWithOptions(Options{Git: git, ConfigRoot: t.TempDir()})

	var stdout, stderr bytes.Buffer
	if code := app.Run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version exit = %d, stderr = %q", code, stderr.String())
	}
	if code := app.Run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit = %d, stderr = %q", code, stderr.String())
	}
	if code := app.Run([]string{"bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bogus exit = %d, want 2", code)
	}
	if git.calls != nil {
		t.Fatalf("git was called for no-preflight command: %#v", git.calls)
	}
}

func TestRepositoryCommandsRequireWorktreeAndResolveSubdirectoryContext(t *testing.T) {
	root := configRootWithModernConfig(t)
	subdir := filepath.Join(root, "nested", "dir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	_, err := Preflight(context.Background(), cli.CommandStatus, cli.Invocation{}, Options{
		Git:        &fakeGit{inside: false, root: root},
		WorkDir:    subdir,
		ConfigRoot: root,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "inside a git working tree") {
		t.Fatalf("outside-worktree Preflight error = %v, want inside-worktree diagnostic", err)
	}

	repo, err := Preflight(context.Background(), cli.CommandStatus, cli.Invocation{}, Options{
		Git:        &fakeGit{inside: true, prefix: "nested/dir/", root: root},
		WorkDir:    subdir,
		ConfigRoot: root,
	}, io.Discard)
	if err != nil {
		t.Fatalf("subdirectory Preflight returned error: %v", err)
	}
	if repo.ProcessWorkDir != subdir {
		t.Fatalf("ProcessWorkDir = %q, want %q", repo.ProcessWorkDir, subdir)
	}
	if repo.GitWorkTreeRoot != root {
		t.Fatalf("GitWorkTreeRoot = %q, want %q", repo.GitWorkTreeRoot, root)
	}
	if repo.WorkTreePrefix != "nested/dir" {
		t.Fatalf("WorkTreePrefix = %q, want nested/dir", repo.WorkTreePrefix)
	}
	if repo.LogicalWorkTreeRoot != root {
		t.Fatalf("LogicalWorkTreeRoot = %q, want %q", repo.LogicalWorkTreeRoot, root)
	}
}

func TestConfigRequirements(t *testing.T) {
	root := t.TempDir()
	_, err := Preflight(context.Background(), cli.CommandStatus, cli.Invocation{}, Options{
		Git:        &fakeGit{inside: true, root: root},
		WorkDir:    root,
		ConfigRoot: root,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "missing .braids.json") {
		t.Fatalf("status Preflight error = %v, want missing config", err)
	}

	_, err = Preflight(context.Background(), cli.CommandAdd, cli.Invocation{}, Options{
		Git:        &fakeGit{inside: true, root: root},
		WorkDir:    root,
		ConfigRoot: root,
	}, io.Discard)
	if err != nil && strings.Contains(err.Error(), "missing .braids.json") {
		t.Fatalf("add incorrectly required config: %v", err)
	}
}

func configRootWithModernConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	data := []byte(`{"config_version":1,"mirrors":{"vendor/repo":{"url":"u","branch":"main","revision":"r"}}}`)
	if err := os.WriteFile(filepath.Join(root, ".braids.json"), data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return root
}

type fakeGit struct {
	inside     bool
	prefix     string
	root       string
	versionErr error
	calls      []string
}

func (f *fakeGit) RequireVersion(context.Context, string) error {
	f.calls = append(f.calls, "version")
	return f.versionErr
}

func (f *fakeGit) IsInsideWorkTree(context.Context) (bool, error) {
	f.calls = append(f.calls, "inside")
	return f.inside, nil
}

func (f *fakeGit) RelativeWorkingDir(context.Context) (string, error) {
	f.calls = append(f.calls, "prefix")
	return f.prefix, nil
}

func (f *fakeGit) WorkTreeRoot(context.Context) (string, error) {
	f.calls = append(f.calls, "root")
	return f.root, nil
}

var _ Git = (*fakeGit)(nil)
