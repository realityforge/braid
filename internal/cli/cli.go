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
	URL            string
	SourceName     string
	ExistingSource string
	Mirrors        []MirrorMapping
	Branch         string
	Tag            string
	Revision       string
	NoCommit       bool
	PartialClone   bool
}

type MirrorMapping struct {
	LocalPath    string
	UpstreamPath string
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
	commandText := rest[0]
	if Help().Matches(commandText) {
		inv.Help = true
		return inv, requireNoArgs(Help().Command, rest[1:])
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
		_, err := parseCommandSyntax(CommandVersion, commandArgs)
		return inv, err
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
	options := GlobalOptionsSpec()
	present := map[string]bool{}
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--cache-dir" || strings.HasPrefix(arg, "--cache-dir=") {
			return nil, usageError("--cache-dir has been replaced by --global-cache-dir")
		}
		name, inlineValue, hasEquals := strings.Cut(arg, "=")
		option, matched := matchOption(name, options)
		if !matched {
			return args[i:], validateOptionConflicts(present, GlobalConflictsSpec())
		}
		present[option.Long] = true
		if !option.TakesValue() && hasEquals {
			return nil, usageError("flag %s does not take a value", option.Long)
		}
		value := inlineValue
		if option.TakesValue() && !hasEquals {
			if i+1 >= len(args) {
				return nil, usageError("%s requires a value", option.Long)
			}
			i++
			value = args[i]
		}
		if option.TakesValue() && value == "" {
			return nil, usageError("%s requires a non-empty value", option.Long)
		}
		switch option.Long {
		case "--no-cache":
			global.NoCache = true
		case "--verbose":
			global.Verbose = true
		case "--quiet":
			global.Quiet = true
		case "--global-cache-dir":
			global.GlobalCacheDir = value
			global.GlobalCacheDirSet = true
		}
		i++
	}
	return nil, validateOptionConflicts(present, GlobalConflictsSpec())
}

func validateOptionConflicts(present map[string]bool, conflicts []ConflictSpec) error {
	for _, conflict := range conflicts {
		all := true
		for _, option := range conflict.Options {
			all = all && present[option]
		}
		if all {
			return usageError("%s", conflict.Error)
		}
	}
	return nil
}

func matchOption(name string, options []OptionSpec) (OptionSpec, bool) {
	for _, option := range options {
		if name == option.Long || (option.Short != "" && name == option.Short) {
			return option, true
		}
	}
	return OptionSpec{}, false
}

func parseAdd(args []string, options *AddOptions) error {
	parsed, err := parseCommandSyntax(CommandAdd, args)
	if err != nil {
		return err
	}
	positionals := parsed.positionals
	options.SourceName = parsed.value("--name")
	options.Branch = parsed.value("--branch")
	options.Tag = parsed.value("--tag")
	options.Revision = parsed.value("--revision")
	options.NoCommit = parsed.has("--no-commit")
	options.PartialClone = parsed.has("--partial-clone")
	if strings.HasPrefix(positionals[0], ":") {
		options.ExistingSource = strings.TrimPrefix(positionals[0], ":")
		if options.ExistingSource == "" {
			return usageError("add source selector requires a name")
		}
	} else {
		options.URL = positionals[0]
	}
	for _, raw := range positionals[1:] {
		local, upstream, found := strings.Cut(raw, "=")
		local = normalizeLocalPathArg(local)
		if local == "" {
			return usageError("add mirror requires a local path")
		}
		if strings.Contains(local, "=") {
			return usageError("add mirror local path cannot contain =")
		}
		if !found {
			upstream = ""
		}
		for _, existing := range options.Mirrors {
			if existing.LocalPath == local {
				return usageError("duplicate add mirror path %s", local)
			}
		}
		options.Mirrors = append(options.Mirrors, MirrorMapping{LocalPath: local, UpstreamPath: upstream})
	}
	return nil
}

func parseUpdate(args []string, options *UpdateOptions) error {
	parsed, err := parseCommandSyntax(CommandPull, args)
	if err != nil {
		return err
	}
	positionals := parsed.positionals
	options.Branch = parsed.value("--branch")
	options.Tag = parsed.value("--tag")
	options.Revision = parsed.value("--revision")
	options.Keep = parsed.has("--keep")
	options.NoCommit = parsed.has("--no-commit")
	if len(positionals) == 1 {
		options.LocalPath = normalizeLocalPathArg(positionals[0])
	}
	return nil
}

func parseRemove(args []string, options *RemoveOptions) error {
	parsed, err := parseCommandSyntax(CommandRemove, args)
	if err != nil {
		return err
	}
	options.LocalPath = normalizeLocalPathArg(parsed.positionals[0])
	options.Keep = parsed.has("--keep")
	options.NoCommit = parsed.has("--no-commit")
	return nil
}

func parseDiff(args []string, options *DiffOptions) error {
	parsed, err := parseCommandSyntax(CommandDiff, args)
	if err != nil {
		return err
	}
	if len(parsed.positionals) == 1 {
		options.LocalPath = normalizeLocalPathArg(parsed.positionals[0])
	}
	options.Keep = parsed.has("--keep")
	options.GitDiffArgs = parsed.passthrough
	return nil
}

func parsePush(args []string, options *PushOptions) error {
	parsed, err := parseCommandSyntax(CommandPush, args)
	if err != nil {
		return err
	}
	options.Branch = parsed.value("--branch")
	options.Message = parsed.value("--message")
	options.Keep = parsed.has("--keep")
	if options.Message != "" && strings.TrimSpace(options.Message) == "" {
		return usageError("--message requires a non-empty message value")
	}
	options.LocalPath = normalizeLocalPathArg(parsed.positionals[0])
	return nil
}

func parseSync(args []string, options *SyncOptions) error {
	parsed, err := parseCommandSyntax(CommandSync, args)
	if err != nil {
		return err
	}
	options.PullOnly = parsed.has("--pull-only")
	options.Autostash = parsed.has("--autostash")
	options.Keep = parsed.has("--keep")
	options.LocalPaths = make([]string, 0, len(parsed.positionals))
	for _, positional := range parsed.positionals {
		options.LocalPaths = append(options.LocalPaths, normalizeLocalPathArg(positional))
	}
	return nil
}

func parseStatus(args []string, options *StatusOptions) error {
	parsed, err := parseCommandSyntax(CommandStatus, args)
	if err != nil {
		return err
	}
	if len(parsed.positionals) == 1 {
		options.LocalPath = normalizeLocalPathArg(parsed.positionals[0])
	}
	return nil
}

func parseCompletion(args []string, options *CompletionOptions) error {
	parsed, err := parseCommandSyntax(CommandCompletion, args)
	if err != nil {
		return err
	}
	if parsed.positionals[0] != "bash" {
		return usageError("unknown completion shell %s", parsed.positionals[0])
	}
	options.Shell = parsed.positionals[0]
	return nil
}

func parseComplete(args []string, options *CompleteOptions) error {
	if len(args) < 2 {
		return usageError("__complete requires shell and -- separator")
	}
	parsed, err := parseCommandSyntax(CommandComplete, args)
	if err != nil {
		return err
	}
	if parsed.positionals[0] != "bash" {
		return usageError("unknown completion shell %s", parsed.positionals[0])
	}
	options.Shell = parsed.positionals[0]
	options.Args = append([]string(nil), parsed.passthrough...)
	return nil
}

func parseUpgradeConfig(args []string, options *UpgradeConfigOptions) error {
	parsed, err := parseCommandSyntax(CommandUpgradeConfig, args)
	if err != nil {
		return err
	}
	options.NoCommit = parsed.has("--no-commit")
	return nil
}

func normalizeLocalPathArg(value string) string {
	return strings.ReplaceAll(value, `\`, "/")
}

func parseCommand(value string) (Command, bool) {
	spec, ok := CommandSpecForName(value)
	return spec.Command, ok
}

func isCommandHelp(args []string) bool {
	return len(args) == 1 && Help().Matches(args[0])
}

func requireNoArgs(command string, args []string) error {
	if len(args) > 0 {
		return usageError("%s received extra argument(s)", command)
	}
	return nil
}

func usageError(format string, args ...interface{}) error {
	return UsageError{Message: fmt.Sprintf(format, args...)}
}

func Usage() string {
	var groups []string
	for _, conflict := range GlobalConflictsSpec() {
		var options []string
		for _, option := range GlobalOptionsSpec() {
			for _, name := range conflict.Options {
				if option.Long == name {
					options = append(options, option.UsageName())
				}
			}
		}
		groups = append(groups, "["+strings.Join(options, " | ")+"]")
	}
	var output strings.Builder
	fmt.Fprintf(&output, "usage: braid %s <command> [options]\n\ncommands:\n", strings.Join(groups, " "))
	for _, spec := range CommandSpecs() {
		if spec.Hidden {
			continue
		}
		if len(spec.Name) <= 7 {
			fmt.Fprintf(&output, "  %-9s %s\n", spec.Name, spec.Summary)
		} else {
			fmt.Fprintf(&output, "  %s\n            %s\n", spec.Name, spec.Summary)
		}
	}
	fmt.Fprintf(&output, "\nRun \"braid <command> %s\" for command-specific usage.\n", Help().Command)
	return output.String()
}

func CommandUsage(command Command) string {
	spec, ok := CommandSpecForCommand(command)
	if !ok || spec.Hidden {
		return Usage()
	}
	parts := []string{"usage:", "braid", spec.Name}
	for _, positional := range spec.Positionals {
		parts = append(parts, positional.Usage)
	}
	for _, option := range spec.Options {
		parts = append(parts, "["+option.UsageName()+"]")
	}
	if spec.Passthrough != nil && spec.Passthrough.Usage != "" {
		parts = append(parts, spec.Passthrough.Usage)
	}
	return strings.Join(parts, " ") + "\n"
}
