package cli

import (
	"fmt"
	"io"
	"strings"
)

const DefaultVersion = "0.0.0-dev"

type Command string

const (
	CommandAdd     Command = "add"
	CommandUpdate  Command = "update"
	CommandRemove  Command = "remove"
	CommandDiff    Command = "diff"
	CommandPush    Command = "push"
	CommandSetup   Command = "setup"
	CommandVersion Command = "version"
	CommandStatus  Command = "status"
)

type GlobalOptions struct {
	NoCache     bool
	CacheDir    string
	CacheDirSet bool
}

type AddOptions struct {
	URL        string
	LocalPath  string
	Branch     string
	Tag        string
	Revision   string
	RemotePath string
	Verbose    bool
}

type UpdateOptions struct {
	LocalPath string
	Branch    string
	Tag       string
	Revision  string
	Keep      bool
	Verbose   bool
}

type RemoveOptions struct {
	LocalPath string
	Keep      bool
	Verbose   bool
}

type DiffOptions struct {
	LocalPath   string
	Keep        bool
	Verbose     bool
	GitDiffArgs []string
}

type PushOptions struct {
	LocalPath string
	Branch    string
	Keep      bool
	Verbose   bool
}

type SetupOptions struct {
	LocalPath string
	Force     bool
	Verbose   bool
}

type StatusOptions struct {
	LocalPath string
	Verbose   bool
}

type Invocation struct {
	Global  GlobalOptions
	Command Command
	Help    bool

	Add    AddOptions
	Update UpdateOptions
	Remove RemoveOptions
	Diff   DiffOptions
	Push   PushOptions
	Setup  SetupOptions
	Status StatusOptions
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
			CommandAdd:    notImplemented(CommandAdd),
			CommandUpdate: notImplemented(CommandUpdate),
			CommandRemove: notImplemented(CommandRemove),
			CommandDiff:   notImplemented(CommandDiff),
			CommandPush:   notImplemented(CommandPush),
			CommandSetup:  notImplemented(CommandSetup),
			CommandStatus: notImplemented(CommandStatus),
		},
	}
}

func (a App) Run(args []string, stdout, stderr io.Writer) int {
	inv, err := Parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "braid: %v\n\n%s", err, Usage())
		return 2
	}

	if inv.Help {
		if inv.Command == "" {
			fmt.Fprint(stdout, Usage())
		} else {
			fmt.Fprint(stdout, CommandUsage(inv.Command))
		}
		return 0
	}

	if inv.Command == CommandVersion {
		fmt.Fprintf(stdout, "braid %s\n", a.version())
		return 0
	}

	handler := a.Handler[inv.Command]
	if handler == nil {
		handler = notImplemented(inv.Command)
	}
	if err := handler.Run(inv, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "braid: %v\n", err)
		return 1
	}
	return 0
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
	if inv.Global.NoCache && inv.Global.CacheDirSet {
		return inv, usageError("--no-cache and --cache-dir cannot be used together")
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
	case CommandUpdate:
		return inv, parseUpdate(commandArgs, &inv.Update)
	case CommandRemove:
		return inv, parseRemove(commandArgs, &inv.Remove)
	case CommandDiff:
		return inv, parseDiff(commandArgs, &inv.Diff)
	case CommandPush:
		return inv, parsePush(commandArgs, &inv.Push)
	case CommandSetup:
		return inv, parseSetup(commandArgs, &inv.Setup)
	case CommandVersion:
		return inv, requireNoArgs("version", commandArgs)
	case CommandStatus:
		return inv, parseStatus(commandArgs, &inv.Status)
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
		case arg == "--cache-dir":
			if i+1 >= len(args) {
				return nil, usageError("--cache-dir requires a value")
			}
			if args[i+1] == "" {
				return nil, usageError("--cache-dir requires a non-empty value")
			}
			global.CacheDir = args[i+1]
			global.CacheDirSet = true
			i += 2
		case strings.HasPrefix(arg, "--cache-dir="):
			value := strings.TrimPrefix(arg, "--cache-dir=")
			if value == "" {
				return nil, usageError("--cache-dir requires a non-empty value")
			}
			global.CacheDir = value
			global.CacheDirSet = true
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
		boolFlag("--verbose", "-v", func() { options.Verbose = true }),
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
	options.URL = positionals[0]
	if len(positionals) == 2 {
		options.LocalPath = positionals[1]
	}
	return nil
}

func parseUpdate(args []string, options *UpdateOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandUpdate, args, []flagSpec{
		valueFlag("--branch", "-b", "branch", func(value string) { options.Branch = value }),
		valueFlag("--tag", "-t", "tag", func(value string) { options.Tag = value }),
		valueFlag("--revision", "-r", "revision", func(value string) { options.Revision = value }),
		boolFlag("--keep", "", func() { options.Keep = true }),
		boolFlag("--verbose", "-v", func() { options.Verbose = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("update", positionals, 0, 1); err != nil {
		return err
	}
	if len(positionals) == 1 {
		options.LocalPath = positionals[0]
	} else if options.Branch != "" || options.Tag != "" || options.Revision != "" {
		return usageError("update without local_path cannot use --branch, --tag, or --revision")
	}
	if options.Tag != "" && options.Branch != "" {
		return usageError("update cannot combine --tag and --branch")
	}
	if options.Tag != "" && options.Revision != "" {
		return usageError("update cannot combine --tag and --revision")
	}
	return nil
}

func parseRemove(args []string, options *RemoveOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandRemove, args, []flagSpec{
		boolFlag("--keep", "", func() { options.Keep = true }),
		boolFlag("--verbose", "-v", func() { options.Verbose = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("remove", positionals, 1, 1); err != nil {
		return err
	}
	options.LocalPath = positionals[0]
	return nil
}

func parseDiff(args []string, options *DiffOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandDiff, args, []flagSpec{
		boolFlag("--keep", "", func() { options.Keep = true }),
		boolFlag("--verbose", "-v", func() { options.Verbose = true }),
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
		options.LocalPath = positionals[0]
	}
	return nil
}

func parsePush(args []string, options *PushOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandPush, args, []flagSpec{
		valueFlag("--branch", "-b", "branch", func(value string) { options.Branch = value }),
		boolFlag("--keep", "", func() { options.Keep = true }),
		boolFlag("--verbose", "-v", func() { options.Verbose = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("push", positionals, 1, 1); err != nil {
		return err
	}
	options.LocalPath = positionals[0]
	return nil
}

func parseSetup(args []string, options *SetupOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandSetup, args, []flagSpec{
		boolFlag("--force", "-f", func() { options.Force = true }),
		boolFlag("--verbose", "-v", func() { options.Verbose = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("setup", positionals, 0, 1); err != nil {
		return err
	}
	if len(positionals) == 1 {
		options.LocalPath = positionals[0]
	}
	return nil
}

func parseStatus(args []string, options *StatusOptions) error {
	var positionals []string
	err := parseCommandArgs(CommandStatus, args, []flagSpec{
		boolFlag("--verbose", "-v", func() { options.Verbose = true }),
	}, func(pos []string, _ []string) {
		positionals = pos
	})
	if err != nil {
		return err
	}
	if err := requireArgRange("status", positionals, 0, 1); err != nil {
		return err
	}
	if len(positionals) == 1 {
		options.LocalPath = positionals[0]
	}
	return nil
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
	command := Command(value)
	switch command {
	case CommandAdd, CommandUpdate, CommandRemove, CommandDiff, CommandPush, CommandSetup, CommandVersion, CommandStatus:
		return command, true
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
usage: braid [--no-cache | --cache-dir <path>] <command> [options]

commands:
  add       Add a new mirror
  update    Update one mirror or every eligible mirror
  remove    Remove a mirror
  diff      Show local mirror changes
  push      Push local mirror changes upstream
  setup     Set up mirror remotes
  status    Show mirror status
  version   Show braid version

Run "braid <command> help" for command-specific usage.
`, "\n")
}

func CommandUsage(command Command) string {
	switch command {
	case CommandAdd:
		return "usage: braid add <url> [local_path] [--branch|-b <branch>] [--tag|-t <tag>] [--revision|-r <rev>] [--path|-p <remote_path>] [--verbose|-v]\n"
	case CommandUpdate:
		return "usage: braid update [local_path] [--branch|-b <branch>] [--tag|-t <tag>] [--revision|-r <rev>] [--keep] [--verbose|-v]\n"
	case CommandRemove:
		return "usage: braid remove <local_path> [--keep] [--verbose|-v]\n"
	case CommandDiff:
		return "usage: braid diff [local_path] [--keep] [--verbose|-v] [-- <git_diff_arg>...]\n"
	case CommandPush:
		return "usage: braid push <local_path> [--branch|-b <branch>] [--keep] [--verbose|-v]\n"
	case CommandSetup:
		return "usage: braid setup [local_path] [--force|-f] [--verbose|-v]\n"
	case CommandVersion:
		return "usage: braid version\n"
	case CommandStatus:
		return "usage: braid status [local_path] [--verbose|-v]\n"
	default:
		return Usage()
	}
}
