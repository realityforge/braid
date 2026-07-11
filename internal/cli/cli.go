package cli

import (
	"fmt"
	"io"
	"strings"
)

var DefaultVersion = "0.0.0-dev"

type Command string

const (
	CommandAdd           Command = "add"
	CommandPull          Command = "pull"
	CommandRemove        Command = "remove"
	CommandDiff          Command = "diff"
	CommandPush          Command = "push"
	CommandSync          Command = "sync"
	CommandVersion       Command = "version"
	CommandStatus        Command = "status"
	CommandCompletion    Command = "completion"
	CommandComplete      Command = "__complete"
	CommandUpgradeConfig Command = "upgrade-config"
)

type GlobalOptions struct {
	NoCache           bool
	GlobalCacheDir    string
	GlobalCacheDirSet bool
	Verbose           bool
	Quiet             bool
}

type AddOptions struct {
	URL          string
	LocalPath    string
	Branch       string
	Tag          string
	Revision     string
	RemotePath   string
	NoCommit     bool
	PartialClone bool
}

type UpgradeConfigOptions struct{ NoCommit bool }

type UpdateOptions struct {
	LocalPath string
	Branch    string
	Tag       string
	Revision  string
	Keep      bool
	NoCommit  bool
}

type RemoveOptions struct {
	LocalPath string
	Keep      bool
	NoCommit  bool
}

type DiffOptions struct {
	LocalPath   string
	Keep        bool
	GitDiffArgs []string
}

type PushOptions struct {
	LocalPath string
	Branch    string
	Keep      bool
	Message   string
}

type SyncOptions struct {
	LocalPaths []string
	PullOnly   bool
	Autostash  bool
	Keep       bool
}

type StatusOptions struct {
	LocalPath string
}

type CompletionOptions struct {
	Shell string
}

type CompleteOptions struct {
	Shell string
	Args  []string
}

type Invocation struct {
	Global  GlobalOptions
	Command Command
	Help    bool

	Add           AddOptions
	Update        UpdateOptions
	Remove        RemoveOptions
	Diff          DiffOptions
	Push          PushOptions
	Sync          SyncOptions
	Status        StatusOptions
	Completion    CompletionOptions
	Complete      CompleteOptions
	UpgradeConfig UpgradeConfigOptions
}

type Handler interface {
	Run(inv Invocation, stdout, stderr io.Writer) error
}

type HandlerFunc func(inv Invocation, stdout, stderr io.Writer) error

func (f HandlerFunc) Run(inv Invocation, stdout, stderr io.Writer) error {
	return f(inv, stdout, stderr)
}

type App struct {
	Version string
	Handler map[Command]Handler
}

func New() App {
	return App{
		Version: DefaultVersion,
		Handler: map[Command]Handler{
			CommandAdd:           notImplemented(CommandAdd),
			CommandPull:          notImplemented(CommandPull),
			CommandRemove:        notImplemented(CommandRemove),
			CommandDiff:          notImplemented(CommandDiff),
			CommandPush:          notImplemented(CommandPush),
			CommandSync:          notImplemented(CommandSync),
			CommandStatus:        notImplemented(CommandStatus),
			CommandUpgradeConfig: notImplemented(CommandUpgradeConfig),
		},
	}
}

func (a App) Run(args []string, stdout, stderr io.Writer) int {
	inv, err := Parse(args)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "braid: %v\n\n%s", err, Usage()); writeErr != nil {
			return 2
		}
		return 2
	}

	if inv.Help {
		if inv.Command == "" {
			if _, err := fmt.Fprint(stdout, Usage()); err != nil {
				return writeRunError(stderr, err)
			}
		} else {
			if _, err := fmt.Fprint(stdout, CommandUsage(inv.Command)); err != nil {
				return writeRunError(stderr, err)
			}
		}
		return 0
	}

	if inv.Command == CommandVersion {
		if _, err := fmt.Fprintf(stdout, "braid %s\n", a.version()); err != nil {
			return writeRunError(stderr, err)
		}
		return 0
	}

	handler := a.Handler[inv.Command]
	if handler == nil {
		handler = notImplemented(inv.Command)
	}
	if err := handler.Run(inv, stdout, stderr); err != nil {
		return writeRunError(stderr, err)
	}
	return 0
}

func writeRunError(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintf(stderr, "braid: %v\n", err)
	return 1
}

func (a App) version() string {
	if a.Version == "" {
		return DefaultVersion
	}
	return a.Version
}

func notImplemented(command Command) HandlerFunc {
	return func(Invocation, io.Writer, io.Writer) error {
		return fmt.Errorf("%s command is not implemented yet", command)
	}
}

type UsageError struct {
	Message string
}

func (e UsageError) Error() string {
	return e.Message
}

func Parse(args []string) (Invocation, error) {
	var inv Invocation
	if len(args) == 0 {
		return inv, usageError("missing command")
	}

	rest, err := parseGlobal(args, &inv.Global)
	if err != nil {
		return inv, err
	}
	if len(rest) == 0 {
		return inv, usageError("missing command")
	}
	if inv.Global.NoCache && inv.Global.GlobalCacheDirSet {
		return inv, usageError("--no-cache and --global-cache-dir cannot be used together")
	}
	if inv.Global.Verbose && inv.Global.Quiet {
		return inv, usageError("--quiet and --verbose cannot be used together")
	}

	commandText := rest[0]
	if commandText == "help" || commandText == "--help" || commandText == "-h" {
		inv.Help = true
		return inv, requireNoArgs("help", rest[1:])
	}
	if strings.HasPrefix(commandText, "-") {
		return inv, usageError("unknown global flag %s", commandText)
	}

	command, ok := parseCommand(commandText)
	if !ok {
		return inv, usageError("unknown command %s", commandText)
	}
	inv.Command = command

	commandArgs := rest[1:]
	if isCommandHelp(commandArgs) {
		inv.Help = true
		return inv, nil
	}

	switch command {
	case CommandAdd:
		return inv, parseAdd(commandArgs, &inv.Add)
	case CommandPull:
		return inv, parseUpdate(commandArgs, &inv.Update)
	case CommandRemove:
		return inv, parseRemove(commandArgs, &inv.Remove)
	case CommandDiff:
		return inv, parseDiff(commandArgs, &inv.Diff)
	case CommandPush:
		return inv, parsePush(commandArgs, &inv.Push)
	case CommandSync:
		return inv, parseSync(commandArgs, &inv.Sync)
	case CommandVersion:
		return inv, requireNoArgs("version", commandArgs)
	case CommandStatus:
		return inv, parseStatus(commandArgs, &inv.Status)
	case CommandCompletion:
		return inv, parseCompletion(commandArgs, &inv.Completion)
	case CommandComplete:
		return inv, parseComplete(commandArgs, &inv.Complete)
	case CommandUpgradeConfig:
		return inv, parseUpgradeConfig(commandArgs, &inv.UpgradeConfig)
	default:
		return inv, usageError("unknown command %s", commandText)
	}
}

func parseGlobal(args []string, global *GlobalOptions) ([]string, error) {
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--no-cache":
			global.NoCache = true
			i++
		case arg == "--verbose" || arg == "-v":
			global.Verbose = true
			i++
		case arg == "--quiet":
			global.Quiet = true
			i++
		case arg == "--cache-dir" || strings.HasPrefix(arg, "--cache-dir="):
			return nil, usageError("--cache-dir has been replaced by --global-cache-dir")
		case arg == "--global-cache-dir":
			if i+1 >= len(args) {
				return nil, usageError("--global-cache-dir requires a value")
			}
			if args[i+1] == "" {
				return nil, usageError("--global-cache-dir requires a non-empty value")
			}
			global.GlobalCacheDir = args[i+1]
			global.GlobalCacheDirSet = true
			i += 2
		case strings.HasPrefix(arg, "--global-cache-dir="):
			value := strings.TrimPrefix(arg, "--global-cache-dir=")
			if value == "" {
				return nil, usageError("--global-cache-dir requires a non-empty value")
			}
			global.GlobalCacheDir = value
			global.GlobalCacheDirSet = true
			i++
		default:
			return args[i:], nil
		}
	}
	return nil, nil
}

func parseAdd(args []string, options *AddOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandAdd, args, []flagSpec{
		valueFlag("--branch", "-b", "branch", func(value string) { options.Branch = value }),
		valueFlag("--tag", "-t", "tag", func(value string) { options.Tag = value }),
		valueFlag("--revision", "-r", "revision", func(value string) { options.Revision = value }),
		valueFlag("--path", "-p", "path", func(value string) { options.RemotePath = value }),
		boolFlag("--no-commit", "", func() { options.NoCommit = true }),
		boolFlag("--partial-clone", "", func() { options.PartialClone = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("add", positionals, 1, 2); err != nil {
		return err
	}
	if options.Tag != "" && options.Branch != "" {
		return usageError("add cannot combine --tag and --branch")
	}
	if options.Tag != "" && options.Revision != "" {
		return usageError("add cannot combine --tag and --revision")
	}
	if options.PartialClone && options.RemotePath == "" {
		return usageError("add --partial-clone requires --path")
	}
	options.URL = positionals[0]
	if len(positionals) == 2 {
		options.LocalPath = normalizeLocalPathArg(positionals[1])
	}
	return nil
}

func parseUpdate(args []string, options *UpdateOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandPull, args, []flagSpec{
		valueFlag("--branch", "-b", "branch", func(value string) { options.Branch = value }),
		valueFlag("--tag", "-t", "tag", func(value string) { options.Tag = value }),
		valueFlag("--revision", "-r", "revision", func(value string) { options.Revision = value }),
		boolFlag("--keep", "", func() { options.Keep = true }),
		boolFlag("--no-commit", "", func() { options.NoCommit = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("pull", positionals, 0, 1); err != nil {
		return err
	}
	if len(positionals) == 1 {
		options.LocalPath = normalizeLocalPathArg(positionals[0])
	} else if options.Branch != "" || options.Tag != "" || options.Revision != "" {
		return usageError("pull without local_path cannot use --branch, --tag, or --revision")
	}
	if options.Tag != "" && options.Branch != "" {
		return usageError("pull cannot combine --tag and --branch")
	}
	if options.Tag != "" && options.Revision != "" {
		return usageError("pull cannot combine --tag and --revision")
	}
	return nil
}

func parseRemove(args []string, options *RemoveOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandRemove, args, []flagSpec{
		boolFlag("--keep", "", func() { options.Keep = true }),
		boolFlag("--no-commit", "", func() { options.NoCommit = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("remove", positionals, 1, 1); err != nil {
		return err
	}
	options.LocalPath = normalizeLocalPathArg(positionals[0])
	return nil
}

func parseDiff(args []string, options *DiffOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandDiff, args, []flagSpec{
		boolFlag("--keep", "", func() { options.Keep = true }),
	}, func(pos []string, passthrough []string) {
		positionals = pos
		options.GitDiffArgs = passthrough
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("diff", positionals, 0, 1); err != nil {
		return err
	}
	if len(positionals) == 1 {
		options.LocalPath = normalizeLocalPathArg(positionals[0])
	}
	return nil
}

func parsePush(args []string, options *PushOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandPush, args, []flagSpec{
		valueFlag("--branch", "-b", "branch", func(value string) { options.Branch = value }),
		valueFlag("--message", "-m", "message", func(value string) { options.Message = value }),
		boolFlag("--keep", "", func() { options.Keep = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("push", positionals, 1, 1); err != nil {
		return err
	}
	if options.Message != "" && strings.TrimSpace(options.Message) == "" {
		return usageError("--message requires a non-empty message value")
	}
	options.LocalPath = normalizeLocalPathArg(positionals[0])
	return nil
}

func parseSync(args []string, options *SyncOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandSync, args, []flagSpec{
		boolFlag("--pull-only", "", func() { options.PullOnly = true }),
		boolFlag("--autostash", "", func() { options.Autostash = true }),
		boolFlag("--keep", "", func() { options.Keep = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	options.LocalPaths = make([]string, 0, len(positionals))
	for _, positional := range positionals {
		options.LocalPaths = append(options.LocalPaths, normalizeLocalPathArg(positional))
	}
	return nil
}

func parseStatus(args []string, options *StatusOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandStatus, args, nil, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("status", positionals, 0, 1); err != nil {
		return err
	}
	if len(positionals) == 1 {
		options.LocalPath = normalizeLocalPathArg(positionals[0])
	}
	return nil
}

func parseCompletion(args []string, options *CompletionOptions) error {
	if err := requireArgRange("completion", args, 1, 1); err != nil {
		return err
	}
	if args[0] != "bash" {
		return usageError("unknown completion shell %s", args[0])
	}
	options.Shell = args[0]
	return nil
}

func parseComplete(args []string, options *CompleteOptions) error {
	if len(args) < 2 {
		return usageError("__complete requires shell and -- separator")
	}
	if args[0] != "bash" {
		return usageError("unknown completion shell %s", args[0])
	}
	if args[1] != "--" {
		return usageError("__complete requires -- separator")
	}
	options.Shell = args[0]
	options.Args = append([]string(nil), args[2:]...)
	return nil
}

func parseUpgradeConfig(args []string, options *UpgradeConfigOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandUpgradeConfig, args, []flagSpec{
		boolFlag("--no-commit", "", func() { options.NoCommit = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	return requireArgRange("upgrade-config", positionals, 0, 0)
}

func normalizeLocalPathArg(value string) string {
	return strings.ReplaceAll(value, `\`, "/")
}

type flagSpec struct {
	long      string
	short     string
	valueName string
	setValue  func(string)
	setBool   func()
}

func valueFlag(long, short, valueName string, set func(string)) flagSpec {
	return flagSpec{long: long, short: short, valueName: valueName, setValue: set}
}

func boolFlag(long, short string, set func()) flagSpec {
	return flagSpec{long: long, short: short, setBool: set}
}

func parseCommandArgs(command Command, args []string, flags []flagSpec, done func([]string, []string)) error {
	var positionals []string
	var passthrough []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if command == CommandDiff && arg == "--" {
			passthrough = append(passthrough, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			spec, inlineValue, matched := matchFlag(arg, flags)
			if !matched {
				return usageError("unknown flag for %s: %s", command, arg)
			}
			if spec.setBool != nil {
				if inlineValue != "" {
					return usageError("flag %s does not take a value", spec.long)
				}
				spec.setBool()
				continue
			}
			value := inlineValue
			if value == "" {
				if i+1 >= len(args) {
					return usageError("%s requires a %s value", spec.long, spec.valueName)
				}
				value = args[i+1]
				i++
			}
			if value == "" {
				return usageError("%s requires a non-empty %s value", spec.long, spec.valueName)
			}
			spec.setValue(value)
			continue
		}
		positionals = append(positionals, arg)
	}
	done(positionals, passthrough)
	return nil
}

func matchFlag(arg string, flags []flagSpec) (flagSpec, string, bool) {
	name := arg
	inlineValue := ""
	if before, after, ok := strings.Cut(arg, "="); ok {
		name = before
		inlineValue = after
	}
	for _, spec := range flags {
		if name == spec.long || (spec.short != "" && name == spec.short) {
			return spec, inlineValue, true
		}
	}
	return flagSpec{}, "", false
}

func parseCommand(value string) (Command, bool) {
	switch value {
	case string(CommandAdd):
		return CommandAdd, true
	case string(CommandPull), "update", "up":
		return CommandPull, true
	case string(CommandRemove):
		return CommandRemove, true
	case string(CommandDiff):
		return CommandDiff, true
	case string(CommandPush):
		return CommandPush, true
	case string(CommandSync):
		return CommandSync, true
	case string(CommandVersion):
		return CommandVersion, true
	case string(CommandStatus):
		return CommandStatus, true
	case string(CommandCompletion):
		return CommandCompletion, true
	case string(CommandComplete):
		return CommandComplete, true
	case string(CommandUpgradeConfig):
		return CommandUpgradeConfig, true
	default:
		return "", false
	}
}

func isCommandHelp(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func requireNoArgs(command string, args []string) error {
	if len(args) > 0 {
		return usageError("%s received extra argument(s)", command)
	}
	return nil
}

func requireArgRange(command string, args []string, min, max int) error {
	if len(args) < min {
		return usageError("%s requires %d argument(s)", command, min)
	}
	if len(args) > max {
		return usageError("%s received extra argument(s)", command)
	}
	return nil
}

func usageError(format string, args ...interface{}) error {
	return UsageError{Message: fmt.Sprintf(format, args...)}
}

func Usage() string {
	return strings.TrimLeft(`
usage: braid [--verbose|-v | --quiet] [--no-cache | --global-cache-dir <path>] <command> [options]

commands:
  add       Add a new mirror
  pull      Pull one mirror or every eligible mirror
  remove    Remove a mirror
  diff      Show local mirror changes
  push      Push local mirror changes upstream
  sync      Push local mirror changes, then pull mirrors
  status    Show mirror status
  version   Show braid version
  completion
            Print Bash completion script
  upgrade-config
            Upgrade .braids.json to the current version

Run "braid <command> help" for command-specific usage.
`, "\n")
}

func CommandUsage(command Command) string {
	switch command {
	case CommandAdd:
		return "usage: braid add <url> [local_path] [--branch|-b <branch>] [--tag|-t <tag>] [--revision|-r <rev>] [--path|-p <remote_path>] [--no-commit] [--partial-clone]\n"
	case CommandPull:
		return "usage: braid pull [local_path] [--branch|-b <branch>] [--tag|-t <tag>] [--revision|-r <rev>] [--keep] [--no-commit]\n"
	case CommandRemove:
		return "usage: braid remove <local_path> [--keep] [--no-commit]\n"
	case CommandDiff:
		return "usage: braid diff [local_path] [--keep] [-- <git_diff_arg>...]\n"
	case CommandPush:
		return "usage: braid push <local_path> [--branch|-b <branch>] [--message|-m <message>] [--keep]\n"
	case CommandSync:
		return "usage: braid sync [local_path...] [--pull-only] [--autostash] [--keep]\n"
	case CommandVersion:
		return "usage: braid version\n"
	case CommandStatus:
		return "usage: braid status [local_path]\n"
	case CommandCompletion:
		return "usage: braid completion bash\n"
	case CommandUpgradeConfig:
		return "usage: braid upgrade-config [--no-commit]\n"
	default:
		return Usage()
	}
}
