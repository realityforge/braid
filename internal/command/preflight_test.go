package command

import (
	"bytes"
	"context"
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

func TestRepositoryCommandsRequireWorktreeRoot(t *testing.T) {
	root := configRootWithModernConfig(t)

	tests := []struct {
		name string
		git  *fakeGit
		want string
	}{
		{name: "outside worktree", git: &fakeGit{inside: false}, want: "inside a git working tree"},
		{name: "subdirectory", git: &fakeGit{inside: true, prefix: "sub/"}, want: "working tree root"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := NewAppWithOptions(Options{Git: test.git, ConfigRoot: root})
			var stdout, stderr bytes.Buffer
			code := app.Run([]string{"status"}, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("exit = %d, want 1; stderr = %q", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want containing %q", stderr.String(), test.want)
			}
		})
	}
}

func TestConfigRequirements(t *testing.T) {
	root := t.TempDir()
	app := NewAppWithOptions(Options{Git: &fakeGit{inside: true}, ConfigRoot: root})
	var stdout, stderr bytes.Buffer

	if code := app.Run([]string{"status"}, &stdout, &stderr); code != 1 {
		t.Fatalf("status exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "missing .braids.json") {
		t.Fatalf("status stderr = %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := app.Run([]string{"add", "https://example.test/repo.git"}, &stdout, &stderr); code != 1 {
		t.Fatalf("add exit = %d, want 1", code)
	}
	if strings.Contains(stderr.String(), "missing .braids.json") {
		t.Fatalf("add incorrectly required config: %q", stderr.String())
	}
}

func TestLegacyConfigRejectedEvenWhenCommandDoesNotRequireConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".braids"), []byte("legacy"), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	app := NewAppWithOptions(Options{Git: &fakeGit{inside: true}, ConfigRoot: root})
	var stdout, stderr bytes.Buffer
	code := app.Run([]string{"add", "https://example.test/repo.git"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "legacy .braids config is unsupported") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestLegacyConfigRejectedForConfigRequiredCommand(t *testing.T) {
	root := configRootWithModernConfig(t)
	if err := os.WriteFile(filepath.Join(root, ".braids"), []byte("legacy"), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	app := NewAppWithOptions(Options{Git: &fakeGit{inside: true}, ConfigRoot: root})
	var stdout, stderr bytes.Buffer
	code := app.Run([]string{"status"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "legacy .braids config is unsupported") {
		t.Fatalf("stderr = %q", stderr.String())
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

var _ Git = (*fakeGit)(nil)
