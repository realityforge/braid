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
			args: []string{"--verbose", "--global-cache-dir", ".cache", "add", "https://example.test/repo.git", "vendor/repo=lib", "--branch", "main", "--no-commit", "--sync-push"},
			want: Invocation{
				Global:  GlobalOptions{GlobalCacheDir: ".cache", GlobalCacheDirSet: true, Verbose: true},
				Command: CommandAdd,
				Add: AddOptions{
					URL:      "https://example.test/repo.git",
					Mirrors:  []MirrorMapping{{LocalPath: "vendor/repo", UpstreamPath: "lib"}},
					Branch:   "main",
					NoCommit: true,
					SyncPush: true,
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
			name: "pull one mirror",
			args: []string{"-v", "pull", "vendor/repo", "-r", "abc123", "--keep", "--no-commit"},
			want: Invocation{
				Global:  GlobalOptions{Verbose: true},
				Command: CommandPull,
				Update:  UpdateOptions{LocalPath: "vendor/repo", Revision: "abc123", Keep: true, NoCommit: true},
			},
		},
		{
			name: "pull all",
			args: []string{"pull"},
			want: Invocation{Command: CommandPull},
		},
		{
			name: "update alias",
			args: []string{"update", "vendor/repo", "--no-commit"},
			want: Invocation{Command: CommandPull, Update: UpdateOptions{LocalPath: "vendor/repo", NoCommit: true}},
		},
		{
			name: "up alias",
			args: []string{"up", "vendor/repo", "--no-commit"},
			want: Invocation{Command: CommandPull, Update: UpdateOptions{LocalPath: "vendor/repo", NoCommit: true}},
		},
		{
			name: "remove",
			args: []string{"--verbose", "remove", "vendor/repo", "--keep", "--no-commit"},
			want: Invocation{Global: GlobalOptions{Verbose: true}, Command: CommandRemove, Remove: RemoveOptions{LocalPath: "vendor/repo", Keep: true, NoCommit: true}},
		},
		{
			name: "diff passthrough",
			args: []string{"--no-cache", "--quiet", "diff", "vendor/repo", "--sync-push-only", "--head", "--", "--stat", "weird;path"},
			want: Invocation{
				Global:  GlobalOptions{NoCache: true, Quiet: true},
				Command: CommandDiff,
				Diff:    DiffOptions{LocalPath: "vendor/repo", SyncPushOnly: true, Head: true, GitDiffArgs: []string{"--stat", "weird;path"}},
			},
		},
		{
			name: "diff index before selector",
			args: []string{"diff", "--index", "vendor/repo"},
			want: Invocation{Command: CommandDiff, Diff: DiffOptions{LocalPath: "vendor/repo", Index: true}},
		},
		{
			name: "diff sync push only before selector",
			args: []string{"diff", "--sync-push-only", "vendor/repo"},
			want: Invocation{Command: CommandDiff, Diff: DiffOptions{LocalPath: "vendor/repo", SyncPushOnly: true}},
		},
		{
			name: "diff verbose passthrough",
			args: []string{"diff", "vendor/repo", "--", "--verbose"},
			want: Invocation{
				Command: CommandDiff,
				Diff:    DiffOptions{LocalPath: "vendor/repo", GitDiffArgs: []string{"--verbose"}},
			},
		},
		{
			name: "diff sync push only text after separator is passthrough",
			args: []string{"diff", "vendor/repo", "--", "--sync-push-only"},
			want: Invocation{
				Command: CommandDiff,
				Diff:    DiffOptions{LocalPath: "vendor/repo", GitDiffArgs: []string{"--sync-push-only"}},
			},
		},
		{
			name: "push",
			args: []string{"-v", "push", "vendor/repo", "-b", "main", "--keep"},
			want: Invocation{Global: GlobalOptions{Verbose: true}, Command: CommandPush, Push: PushOptions{LocalPath: "vendor/repo", Branch: "main", Keep: true}},
		},
		{
			name: "push message",
			args: []string{"push", "vendor/repo", "-m", "Push upstream\n\nBody"},
			want: Invocation{Command: CommandPush, Push: PushOptions{LocalPath: "vendor/repo", Message: "Push upstream\n\nBody"}},
		},
		{
			name: "sync all",
			args: []string{"sync"},
			want: Invocation{Command: CommandSync, Sync: SyncOptions{LocalPaths: []string{}}},
		},
		{
			name: "sync multiple paths",
			args: []string{"sync", "vendor/a", "vendor/b"},
			want: Invocation{Command: CommandSync, Sync: SyncOptions{LocalPaths: []string{"vendor/a", "vendor/b"}}},
		},
		{
			name: "sync pull only keep",
			args: []string{"sync", "vendor/a", "--pull-only", "--keep"},
			want: Invocation{Command: CommandSync, Sync: SyncOptions{LocalPaths: []string{"vendor/a"}, PullOnly: true, Keep: true}},
		},
		{
			name: "sync autostash",
			args: []string{"sync", "--autostash", "vendor/a"},
			want: Invocation{Command: CommandSync, Sync: SyncOptions{LocalPaths: []string{"vendor/a"}, Autostash: true}},
		},
		{
			name: "sync pull only autostash keep",
			args: []string{"sync", "vendor/a", "--pull-only", "--autostash", "--keep"},
			want: Invocation{Command: CommandSync, Sync: SyncOptions{LocalPaths: []string{"vendor/a"}, PullOnly: true, Autostash: true, Keep: true}},
		},
		{
			name: "version",
			args: []string{"version"},
			want: Invocation{Command: CommandVersion},
		},
		{
			name: "global verbose version",
			args: []string{"--verbose", "version"},
			want: Invocation{Global: GlobalOptions{Verbose: true}, Command: CommandVersion},
		},
		{
			name: "global quiet version",
			args: []string{"--quiet", "version"},
			want: Invocation{Global: GlobalOptions{Quiet: true}, Command: CommandVersion},
		},
		{
			name: "status",
			args: []string{"-v", "status", "vendor/repo"},
			want: Invocation{Global: GlobalOptions{Verbose: true}, Command: CommandStatus, Status: StatusOptions{LocalPath: "vendor/repo"}},
		},
		{
			name: "completion bash",
			args: []string{"completion", "bash"},
			want: Invocation{Command: CommandCompletion, Completion: CompletionOptions{Shell: "bash"}},
		},
		{
			name: "complete bash callback",
			args: []string{"__complete", "bash", "--", "status", "vendor"},
			want: Invocation{Command: CommandComplete, Complete: CompleteOptions{Shell: "bash", Args: []string{"status", "vendor"}}},
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
		{name: "unknown command", args: []string{"unknown-command"}, want: "unknown command unknown-command"},
		{name: "unknown global flag", args: []string{"--bogus", "version"}, want: "unknown global flag --bogus"},
		{name: "global no cache after command", args: []string{"add", "--no-cache", "url"}, want: "unknown flag for add: --no-cache"},
		{name: "global verbose after command", args: []string{"add", "url", "--verbose"}, want: "unknown flag for add: --verbose"},
		{name: "global verbose short after command", args: []string{"pull", "vendor/repo", "-v"}, want: "unknown flag for pull: -v"},
		{name: "global quiet after command", args: []string{"add", "url", "--quiet"}, want: "unknown flag for add: --quiet"},
		{name: "global cache flags conflict", args: []string{"--no-cache", "--global-cache-dir", "cache", "version"}, want: "--no-cache and --global-cache-dir cannot be used together"},
		{name: "old cache dir flag", args: []string{"--cache-dir", "cache", "version"}, want: "--cache-dir has been replaced by --global-cache-dir"},
		{name: "old cache dir equals flag", args: []string{"--cache-dir=cache", "version"}, want: "--cache-dir has been replaced by --global-cache-dir"},
		{name: "quiet verbose conflict", args: []string{"--quiet", "--verbose", "version"}, want: "--quiet and --verbose cannot be used together"},
		{name: "global boolean inline value", args: []string{"--verbose=true", "version"}, want: "flag --verbose does not take a value"},
		{name: "verbose quiet conflict", args: []string{"--verbose", "--quiet", "version"}, want: "--quiet and --verbose cannot be used together"},
		{name: "empty global cache dir", args: []string{"--global-cache-dir=", "version"}, want: "--global-cache-dir requires a non-empty value"},
		{name: "existing source needs mirror", args: []string{"add", ":source"}, want: "add to an existing source requires at least one mirror"},
		{name: "tag branch conflict", args: []string{"add", "url", "--tag", "v1", "--branch", "main"}, want: "add cannot combine --tag and --branch"},
		{name: "sync push tag conflict", args: []string{"add", "url", "--sync-push", "--tag", "v1"}, want: "add cannot combine --sync-push and --tag"},
		{name: "sync push revision conflict", args: []string{"add", "url", "--sync-push", "--revision", "abc"}, want: "add cannot combine --sync-push and --revision"},
		{name: "existing source sync push", args: []string{"add", ":source", "vendor/new", "--sync-push"}, want: "add to an existing source cannot use --name, --branch, --tag, --revision, --partial-clone, or --sync-push"},
		{name: "pull all strategy flag", args: []string{"pull", "--branch", "main"}, want: "pull without local_path cannot use --branch, --tag, or --revision"},
		{name: "diff args require separator", args: []string{"diff", "--stat"}, want: "unknown flag for diff: --stat"},
		{name: "diff endpoint conflict", args: []string{"diff", "--head", "--index"}, want: "diff cannot combine --head and --index"},
		{name: "sync unknown flag", args: []string{"sync", "--branch", "main"}, want: "unknown flag for sync: --branch"},
		{name: "sync no commit unsupported", args: []string{"sync", "--no-commit"}, want: "unknown flag for sync: --no-commit"},
		{name: "push whitespace message", args: []string{"push", "vendor/repo", "--message", " \t"}, want: "--message requires a non-empty message value"},
		{name: "version extra args", args: []string{"version", "extra"}, want: "version received extra argument(s)"},
		{name: "completion missing shell", args: []string{"completion"}, want: "completion requires 1 argument(s)"},
		{name: "completion unknown shell", args: []string{"completion", "zsh"}, want: "unknown completion shell zsh"},
		{name: "complete missing separator", args: []string{"__complete", "bash"}, want: "__complete requires shell and -- separator"},
		{name: "complete unknown shell", args: []string{"__complete", "zsh", "--"}, want: "unknown completion shell zsh"},
		{name: "complete missing separator token", args: []string{"__complete", "bash", "status"}, want: "__complete requires -- separator"},
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

func TestParseNormalizesLocalPathArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "add explicit local path", args: []string{"add", "url", `vendor\repo`}, want: "vendor/repo"},
		{name: "pull selector", args: []string{"pull", `vendor\repo`}, want: "vendor/repo"},
		{name: "update selector alias", args: []string{"update", `vendor\repo`}, want: "vendor/repo"},
		{name: "up selector alias", args: []string{"up", `vendor\repo`}, want: "vendor/repo"},
		{name: "remove selector", args: []string{"remove", `vendor\repo`}, want: "vendor/repo"},
		{name: "diff selector", args: []string{"diff", `vendor\repo`}, want: "vendor/repo"},
		{name: "push selector", args: []string{"push", `vendor\repo`}, want: "vendor/repo"},
		{name: "sync selector", args: []string{"sync", `vendor\repo`}, want: "vendor/repo"},
		{name: "status selector", args: []string{"status", `vendor\repo`}, want: "vendor/repo"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.args)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if gotLocalPath(got) != test.want {
				t.Fatalf("local path = %q, want %q", gotLocalPath(got), test.want)
			}
		})
	}
}

func gotLocalPath(inv Invocation) string {
	switch inv.Command {
	case CommandAdd:
		if len(inv.Add.Mirrors) > 0 {
			return inv.Add.Mirrors[0].LocalPath
		}
		return ""
	case CommandPull:
		return inv.Update.LocalPath
	case CommandRemove:
		return inv.Remove.LocalPath
	case CommandDiff:
		return inv.Diff.LocalPath
	case CommandPush:
		return inv.Push.LocalPath
	case CommandSync:
		if len(inv.Sync.LocalPaths) == 0 {
			return ""
		}
		return inv.Sync.LocalPaths[0]
	case CommandStatus:
		return inv.Status.LocalPath
	default:
		return ""
	}
}

func TestHelpParsing(t *testing.T) {
	tests := []struct {
		args        []string
		wantCommand Command
	}{
		{args: []string{"help"}},
		{args: []string{"--help"}},
		{args: []string{"--verbose", "help"}},
		{args: []string{"-v", "help"}},
		{args: []string{"add", "help"}, wantCommand: CommandAdd},
		{args: []string{"pull", "help"}, wantCommand: CommandPull},
		{args: []string{"update", "--help"}, wantCommand: CommandPull},
		{args: []string{"up", "-h"}, wantCommand: CommandPull},
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

func TestUsageDocumentsVerboseAsGlobalOnly(t *testing.T) {
	if strings.Contains(Usage(), "usage: braid [--no-cache | --global-cache-dir <path>] <command> [options]") {
		t.Fatalf("top-level usage still contains old global syntax:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "usage: braid [--verbose|-v | --quiet] [--no-cache | --global-cache-dir <path>] <command> [options]") {
		t.Fatalf("top-level usage missing global output flag syntax:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "  pull      Pull one source or every eligible source") {
		t.Fatalf("top-level usage missing pull command:\n%s", Usage())
	}
	if strings.Contains(Usage(), "  update") || strings.Contains(Usage(), "\n  up ") {
		t.Fatalf("top-level usage exposes update aliases:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "  sync      Push opted-in local changes, then pull sources") {
		t.Fatalf("top-level usage missing sync command:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "  completion") {
		t.Fatalf("top-level usage missing completion command:\n%s", Usage())
	}
	if strings.Contains(Usage(), "__complete") {
		t.Fatalf("top-level usage exposes hidden completion callback:\n%s", Usage())
	}
	if got, want := CommandUsage(CommandAdd), "usage: braid add <url|:source> [local_path[=upstream_path]...] [--name <name>] [--branch|-b <branch>] [--tag|-t <tag>] [--revision|-r <rev>] [--no-commit] [--partial-clone] [--sync-push]\n"; got != want {
		t.Fatalf("CommandUsage(add) = %q, want %q", got, want)
	}
	if got, want := CommandUsage(CommandPull), "usage: braid pull [local_path|:source] [--branch|-b <branch>] [--tag|-t <tag>] [--revision|-r <rev>] [--keep] [--no-commit]\n"; got != want {
		t.Fatalf("CommandUsage(pull) = %q, want %q", got, want)
	}
	if got, want := CommandUsage(CommandRemove), "usage: braid remove <local_path|:source> [--keep] [--no-commit]\n"; got != want {
		t.Fatalf("CommandUsage(remove) = %q, want %q", got, want)
	}
	if got, want := CommandUsage(CommandDiff), "usage: braid diff [local_path|:source] [--keep] [--sync-push-only] [--head] [--index] [-- <git_diff_arg>...]\n"; got != want {
		t.Fatalf("CommandUsage(diff) = %q, want %q", got, want)
	}
	if got, want := CommandUsage(CommandPush), "usage: braid push <local_path|:source> [--branch|-b <branch>] [--message|-m <message>] [--keep]\n"; got != want {
		t.Fatalf("CommandUsage(push) = %q, want %q", got, want)
	}
	if got, want := CommandUsage(CommandSync), "usage: braid sync [local_path|:source ...] [--pull-only] [--autostash] [--keep]\n"; got != want {
		t.Fatalf("CommandUsage(sync) = %q, want %q", got, want)
	}
	if got, want := CommandUsage(CommandCompletion), "usage: braid completion bash\n"; got != want {
		t.Fatalf("CommandUsage(completion) = %q, want %q", got, want)
	}
	for _, command := range []Command{
		CommandAdd,
		CommandPull,
		CommandRemove,
		CommandDiff,
		CommandPush,
		CommandSync,
		CommandStatus,
	} {
		if usage := CommandUsage(command); strings.Contains(usage, "--verbose") || strings.Contains(usage, "|-v") || strings.Contains(usage, "--quiet") {
			t.Fatalf("CommandUsage(%s) = %q, want no command-local output flag syntax", command, usage)
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
	if got.Command != CommandAdd || got.Add.URL != "url" || len(got.Add.Mirrors) != 1 || got.Add.Mirrors[0].LocalPath != "vendor/repo" || got.Add.Branch != "main" {
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
