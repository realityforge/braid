package cli

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestParseCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Invocation
	}{
		{
			name: "add branch mirror",
			args: []string{"--cache-dir", ".cache", "add", "https://example.test/repo.git", "vendor/repo", "--branch", "main", "--path", "lib", "--verbose"},
			want: Invocation{
				Global:  GlobalOptions{CacheDir: ".cache", CacheDirSet: true},
				Command: CommandAdd,
				Add: AddOptions{
					URL:        "https://example.test/repo.git",
					LocalPath:  "vendor/repo",
					Branch:     "main",
					RemotePath: "lib",
					Verbose:    true,
				},
			},
		},
		{
			name: "add tag mirror",
			args: []string{"add", "https://example.test/repo.git", "--tag=v1.0.0"},
			want: Invocation{
				Command: CommandAdd,
				Add: AddOptions{
					URL: "https://example.test/repo.git",
					Tag: "v1.0.0",
				},
			},
		},
		{
			name: "update one mirror",
			args: []string{"update", "vendor/repo", "-r", "abc123", "--keep", "-v"},
			want: Invocation{
				Command: CommandUpdate,
				Update:  UpdateOptions{LocalPath: "vendor/repo", Revision: "abc123", Keep: true, Verbose: true},
			},
		},
		{
			name: "update all",
			args: []string{"update"},
			want: Invocation{Command: CommandUpdate},
		},
		{
			name: "remove",
			args: []string{"remove", "vendor/repo", "--keep", "-v"},
			want: Invocation{Command: CommandRemove, Remove: RemoveOptions{LocalPath: "vendor/repo", Keep: true, Verbose: true}},
		},
		{
			name: "diff passthrough",
			args: []string{"--no-cache", "diff", "vendor/repo", "-v", "--", "--stat", "weird;path"},
			want: Invocation{
				Global:  GlobalOptions{NoCache: true},
				Command: CommandDiff,
				Diff:    DiffOptions{LocalPath: "vendor/repo", Verbose: true, GitDiffArgs: []string{"--stat", "weird;path"}},
			},
		},
		{
			name: "push",
			args: []string{"push", "vendor/repo", "-b", "main", "--keep", "-v"},
			want: Invocation{Command: CommandPush, Push: PushOptions{LocalPath: "vendor/repo", Branch: "main", Keep: true, Verbose: true}},
		},
		{
			name: "setup",
			args: []string{"setup", "vendor/repo", "--force", "-v"},
			want: Invocation{Command: CommandSetup, Setup: SetupOptions{LocalPath: "vendor/repo", Force: true, Verbose: true}},
		},
		{
			name: "version",
			args: []string{"version"},
			want: Invocation{Command: CommandVersion},
		},
		{
			name: "status",
			args: []string{"status", "vendor/repo", "-v"},
			want: Invocation{Command: CommandStatus, Status: StatusOptions{LocalPath: "vendor/repo", Verbose: true}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.args)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Parse = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing command", args: nil, want: "missing command"},
		{name: "unknown command", args: []string{"upgrade-config"}, want: "unknown command upgrade-config"},
		{name: "unknown global flag", args: []string{"--bogus", "version"}, want: "unknown global flag --bogus"},
		{name: "global no cache after command", args: []string{"add", "--no-cache", "url"}, want: "unknown flag for add: --no-cache"},
		{name: "cache flags conflict", args: []string{"--no-cache", "--cache-dir", "cache", "version"}, want: "--no-cache and --cache-dir cannot be used together"},
		{name: "empty cache dir", args: []string{"--cache-dir=", "version"}, want: "--cache-dir requires a non-empty value"},
		{name: "add extra args", args: []string{"add", "url", "path", "extra"}, want: "add received extra argument(s)"},
		{name: "tag branch conflict", args: []string{"add", "url", "--tag", "v1", "--branch", "main"}, want: "add cannot combine --tag and --branch"},
		{name: "update all strategy flag", args: []string{"update", "--branch", "main"}, want: "update without local_path cannot use --branch, --tag, or --revision"},
		{name: "update head removed", args: []string{"update", "vendor/repo", "--head"}, want: "unknown flag for update: --head"},
		{name: "diff args require separator", args: []string{"diff", "--stat"}, want: "unknown flag for diff: --stat"},
		{name: "version extra args", args: []string{"version", "extra"}, want: "version received extra argument(s)"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.args)
			if err == nil {
				t.Fatal("Parse returned nil error")
			}
			if got := err.Error(); got != test.want {
				t.Fatalf("error = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHelpParsing(t *testing.T) {
	tests := []struct {
		args        []string
		wantCommand Command
	}{
		{args: []string{"help"}},
		{args: []string{"--help"}},
		{args: []string{"add", "help"}, wantCommand: CommandAdd},
		{args: []string{"diff", "--help"}, wantCommand: CommandDiff},
	}

	for _, test := range tests {
		got, err := Parse(test.args)
		if err != nil {
			t.Fatalf("Parse(%v) returned error: %v", test.args, err)
		}
		if !got.Help {
			t.Fatalf("Parse(%v) did not mark help", test.args)
		}
		if got.Command != test.wantCommand {
			t.Fatalf("Parse(%v) command = %q, want %q", test.args, got.Command, test.wantCommand)
		}
	}
}

func TestRunVersionAndHelpDoNotRequireHandlers(t *testing.T) {
	app := App{Version: "test-version"}

	var stdout, stderr bytes.Buffer
	if code := app.Run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version exit = %d, want 0", code)
	}
	if got, want := stdout.String(), "braid test-version\n"; got != want {
		t.Fatalf("version stdout = %q, want %q", got, want)
	}
	if stderr.String() != "" {
		t.Fatalf("version stderr = %q, want empty", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := app.Run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "usage: braid") {
		t.Fatalf("help stdout missing usage: %q", stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("help stderr = %q, want empty", stderr.String())
	}
}

func TestRunDispatchesParsedCommand(t *testing.T) {
	var got Invocation
	app := App{
		Version: "test-version",
		Handler: map[Command]Handler{
			CommandAdd: HandlerFunc(func(inv Invocation, _ io.Writer, _ io.Writer) error {
				got = inv
				return nil
			}),
		},
	}

	var stdout, stderr bytes.Buffer
	code := app.Run([]string{"add", "url", "vendor/repo", "--branch", "main"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if got.Command != CommandAdd || got.Add.URL != "url" || got.Add.LocalPath != "vendor/repo" || got.Add.Branch != "main" {
		t.Fatalf("dispatched invocation = %#v", got)
	}
}

func TestRunMapsUsageAndHandlerErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := App{}

	if code := app.Run([]string{"nope"}, &stdout, &stderr); code != 2 {
		t.Fatalf("usage exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command nope") {
		t.Fatalf("usage stderr = %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	app.Handler = map[Command]Handler{
		CommandStatus: HandlerFunc(func(Invocation, io.Writer, io.Writer) error {
			return errors.New("handler failed")
		}),
	}
	if code := app.Run([]string{"status"}, &stdout, &stderr); code != 1 {
		t.Fatalf("handler exit = %d, want 1", code)
	}
	if got, want := strings.TrimSpace(stderr.String()), "braid: handler failed"; got != want {
		t.Fatalf("handler stderr = %q, want %q", got, want)
	}
}
