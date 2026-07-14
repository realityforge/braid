package cli

import (
	"fmt"
	"strings"
)

type CompletionRole string

const (
	CompletionNone       CompletionRole = ""
	CompletionFree       CompletionRole = "free"
	CompletionDirectory  CompletionRole = "directory"
	CompletionFilesystem CompletionRole = "filesystem"
	CompletionMirror     CompletionRole = "mirror"
	CompletionStatic     CompletionRole = "static"
)

type HelpSpec struct {
	Command string
	Long    string
	Short   string
}

func (h HelpSpec) Names() []string { return []string{h.Command, h.Long, h.Short} }

func (h HelpSpec) Matches(value string) bool {
	return value == h.Command || value == h.Long || value == h.Short
}

type Availability struct {
	PositionalIndex int
	ForbiddenPrefix string
}

func (a Availability) Allows(positionals []string) bool {
	return a.ForbiddenPrefix == "" || len(positionals) <= a.PositionalIndex || !strings.HasPrefix(positionals[a.PositionalIndex], a.ForbiddenPrefix)
}

type OptionSpec struct {
	Long         string
	Short        string
	ValueName    string
	UsageValue   string
	Completion   CompletionRole
	Availability Availability
}

func (o OptionSpec) TakesValue() bool { return o.ValueName != "" }

func (o OptionSpec) UsageName() string {
	valueName := o.UsageValue
	if valueName == "" {
		valueName = o.ValueName
	}
	name := o.Long
	if o.Short != "" {
		name += "|" + o.Short
	}
	if valueName != "" {
		name += " <" + valueName + ">"
	}
	return name
}

func (s CommandSpec) Option(name string) (OptionSpec, bool) {
	for _, option := range s.Options {
		if name == option.Long || (option.Short != "" && name == option.Short) {
			return option, true
		}
	}
	return OptionSpec{}, false
}

type PositionalSpec struct {
	Name       string
	Usage      string
	Required   bool
	Repeatable bool
	Completion CompletionRole
	Values     []string
}

type PassthroughSpec struct {
	Required bool
	Usage    string
}

type ConflictSpec struct {
	Options []string
	Error   string
}

type RequiresPositionalsSpec struct {
	Options []string
	Minimum int
	Error   string
}

type ConditionalPositionalsSpec struct {
	Index   int
	Prefix  string
	Minimum int
	Error   string
}

type CommandSpec struct {
	Command                Command
	Name                   string
	Aliases                []string
	Summary                string
	Hidden                 bool
	Options                []OptionSpec
	Positionals            []PositionalSpec
	Passthrough            *PassthroughSpec
	Conflicts              []ConflictSpec
	RequiresPositionals    []RequiresPositionalsSpec
	ConditionalPositionals []ConditionalPositionalsSpec
	AvailabilityError      string
}

var globalOptions = []OptionSpec{
	{Long: "--verbose", Short: "-v"},
	{Long: "--quiet"},
	{Long: "--no-cache"},
	{Long: "--global-cache-dir", ValueName: "path", Completion: CompletionDirectory},
}

var help = HelpSpec{Command: "help", Long: "--help", Short: "-h"}

var globalConflicts = []ConflictSpec{
	{Options: []string{"--quiet", "--verbose"}, Error: "--quiet and --verbose cannot be used together"},
	{Options: []string{"--no-cache", "--global-cache-dir"}, Error: "--no-cache and --global-cache-dir cannot be used together"},
}

var commandSpecs = []CommandSpec{
	{
		Command: CommandAdd, Name: "add", Summary: "Add a source or mirrors to an existing source",
		Options: []OptionSpec{
			{Long: "--name", ValueName: "name", Availability: Availability{ForbiddenPrefix: ":"}},
			{Long: "--branch", Short: "-b", ValueName: "branch", Availability: Availability{ForbiddenPrefix: ":"}},
			{Long: "--tag", Short: "-t", ValueName: "tag", Availability: Availability{ForbiddenPrefix: ":"}},
			{Long: "--revision", Short: "-r", ValueName: "revision", UsageValue: "rev", Availability: Availability{ForbiddenPrefix: ":"}},
			{Long: "--no-commit"},
			{Long: "--partial-clone", Availability: Availability{ForbiddenPrefix: ":"}},
			{Long: "--sync-push", Availability: Availability{ForbiddenPrefix: ":"}},
		},
		Positionals: []PositionalSpec{
			{Name: "url|:source", Usage: "<url|:source>", Required: true, Completion: CompletionFree},
			{Name: "local_path[=upstream_path]", Usage: "[local_path[=upstream_path]...]", Repeatable: true, Completion: CompletionFilesystem},
		},
		Conflicts: []ConflictSpec{
			{Options: []string{"--tag", "--branch"}, Error: "add cannot combine --tag and --branch"},
			{Options: []string{"--tag", "--revision"}, Error: "add cannot combine --tag and --revision"},
			{Options: []string{"--sync-push", "--tag"}, Error: "add cannot combine --sync-push and --tag"},
			{Options: []string{"--sync-push", "--revision"}, Error: "add cannot combine --sync-push and --revision"},
		},
		ConditionalPositionals: []ConditionalPositionalsSpec{{Index: 0, Prefix: ":", Minimum: 2, Error: "add to an existing source requires at least one mirror"}},
		AvailabilityError:      "add to an existing source cannot use --name, --branch, --tag, --revision, --partial-clone, or --sync-push",
	},
	{
		Command: CommandPull, Name: "pull", Aliases: []string{"update", "up"}, Summary: "Pull one source or every eligible source",
		Options: []OptionSpec{
			{Long: "--branch", Short: "-b", ValueName: "branch"},
			{Long: "--tag", Short: "-t", ValueName: "tag"},
			{Long: "--revision", Short: "-r", ValueName: "revision", UsageValue: "rev"},
			{Long: "--keep"},
			{Long: "--no-commit"},
		},
		Positionals: []PositionalSpec{{Name: "local_path|:source", Usage: "[local_path|:source]", Completion: CompletionMirror}},
		Conflicts: []ConflictSpec{
			{Options: []string{"--tag", "--branch"}, Error: "pull cannot combine --tag and --branch"},
			{Options: []string{"--tag", "--revision"}, Error: "pull cannot combine --tag and --revision"},
		},
		RequiresPositionals: []RequiresPositionalsSpec{{Options: []string{"--branch", "--tag", "--revision"}, Minimum: 1, Error: "pull without local_path cannot use --branch, --tag, or --revision"}},
	},
	{
		Command: CommandRemove, Name: "remove", Summary: "Remove a mirror or source",
		Options:     []OptionSpec{{Long: "--keep"}, {Long: "--no-commit"}},
		Positionals: []PositionalSpec{{Name: "local_path|:source", Usage: "<local_path|:source>", Required: true, Completion: CompletionMirror}},
	},
	{
		Command: CommandDiff, Name: "diff", Summary: "Show local mirror changes",
		Options:     []OptionSpec{{Long: "--keep"}, {Long: "--sync-push-only"}, {Long: "--head"}, {Long: "--index"}},
		Positionals: []PositionalSpec{{Name: "local_path|:source", Usage: "[local_path|:source]", Completion: CompletionMirror}},
		Passthrough: &PassthroughSpec{Usage: "[-- <git_diff_arg>...]"},
		Conflicts:   []ConflictSpec{{Options: []string{"--head", "--index"}, Error: "diff cannot combine --head and --index"}},
	},
	{
		Command: CommandPush, Name: "push", Summary: "Push one source's local mirror changes upstream",
		Options:     []OptionSpec{{Long: "--branch", Short: "-b", ValueName: "branch"}, {Long: "--message", Short: "-m", ValueName: "message"}, {Long: "--keep"}},
		Positionals: []PositionalSpec{{Name: "local_path|:source", Usage: "<local_path|:source>", Required: true, Completion: CompletionMirror}},
	},
	{
		Command: CommandSync, Name: "sync", Summary: "Push opted-in local changes, then pull sources",
		Options:     []OptionSpec{{Long: "--pull-only"}, {Long: "--autostash"}, {Long: "--keep"}},
		Positionals: []PositionalSpec{{Name: "local_path|:source", Usage: "[local_path|:source ...]", Repeatable: true, Completion: CompletionMirror}},
	},
	{Command: CommandStatus, Name: "status", Summary: "Show mirror status", Positionals: []PositionalSpec{{Name: "local_path|:source", Usage: "[local_path|:source]", Completion: CompletionMirror}}},
	{Command: CommandVersion, Name: "version", Summary: "Show braid version"},
	{Command: CommandCompletion, Name: "completion", Summary: "Print Bash completion script", Positionals: []PositionalSpec{{Name: "shell", Usage: "bash", Required: true, Completion: CompletionStatic, Values: []string{"bash"}}}},
	{Command: CommandComplete, Name: "__complete", Hidden: true, Positionals: []PositionalSpec{{Name: "shell", Usage: "<shell>", Required: true, Completion: CompletionStatic, Values: []string{"bash"}}}, Passthrough: &PassthroughSpec{Required: true}},
	{Command: CommandUpgradeConfig, Name: "upgrade-config", Summary: "Upgrade .braids.json to the current version", Options: []OptionSpec{{Long: "--no-commit"}}},
}

func GlobalOptionsSpec() []OptionSpec { return globalOptions }

func Help() HelpSpec { return help }

func GlobalConflictsSpec() []ConflictSpec { return globalConflicts }

func CommandSpecs() []CommandSpec { return commandSpecs }

func CommandSpecForName(name string) (CommandSpec, bool) {
	for _, spec := range commandSpecs {
		if spec.Name == name {
			return spec, true
		}
		for _, alias := range spec.Aliases {
			if alias == name {
				return spec, true
			}
		}
	}
	return CommandSpec{}, false
}

func CommandSpecForCommand(command Command) (CommandSpec, bool) {
	for _, spec := range commandSpecs {
		if spec.Command == command {
			return spec, true
		}
	}
	return CommandSpec{}, false
}

type parsedCommandArgs struct {
	positionals []string
	passthrough []string
	values      map[string]string
	present     map[string]bool
}

func (p parsedCommandArgs) value(name string) string { return p.values[name] }

func (p parsedCommandArgs) has(name string) bool { return p.present[name] }

func parseCommandSyntax(command Command, args []string) (parsedCommandArgs, error) {
	spec, ok := CommandSpecForCommand(command)
	if !ok {
		return parsedCommandArgs{}, usageError("unknown command %s", command)
	}
	parsed := parsedCommandArgs{values: map[string]string{}, present: map[string]bool{}}
	separatorFound := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" && spec.Passthrough != nil {
			separatorFound = true
			parsed.passthrough = append(parsed.passthrough, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			name, inlineValue, hasEquals := strings.Cut(arg, "=")
			option, matched := spec.Option(name)
			if !matched {
				return parsed, usageError("unknown flag for %s: %s", command, arg)
			}
			parsed.present[option.Long] = true
			if !option.TakesValue() {
				if hasEquals {
					return parsed, usageError("flag %s does not take a value", option.Long)
				}
				continue
			}
			value := inlineValue
			if !hasEquals {
				if i+1 >= len(args) {
					return parsed, usageError("%s requires a %s value", option.Long, option.ValueName)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return parsed, usageError("%s requires a non-empty %s value", option.Long, option.ValueName)
			}
			parsed.values[option.Long] = value
			continue
		}
		parsed.positionals = append(parsed.positionals, arg)
	}
	if spec.Passthrough != nil && spec.Passthrough.Required && !separatorFound {
		return parsed, usageError("%s requires -- separator", command)
	}
	min, max := positionalRange(spec.Positionals)
	if len(parsed.positionals) < min {
		return parsed, usageError("%s requires %d argument(s)", command, min)
	}
	if max >= 0 && len(parsed.positionals) > max {
		return parsed, usageError("%s received extra argument(s)", command)
	}
	for _, requirement := range spec.ConditionalPositionals {
		if len(parsed.positionals) > requirement.Index && strings.HasPrefix(parsed.positionals[requirement.Index], requirement.Prefix) && len(parsed.positionals) < requirement.Minimum {
			return parsed, usageError("%s", requirement.Error)
		}
	}
	for _, option := range spec.Options {
		if parsed.has(option.Long) && !option.Availability.Allows(parsed.positionals) {
			return parsed, usageError("%s", spec.AvailabilityError)
		}
	}
	for _, conflict := range spec.Conflicts {
		all := true
		for _, option := range conflict.Options {
			all = all && parsed.has(option)
		}
		if all {
			return parsed, usageError("%s", conflict.Error)
		}
	}
	for _, requirement := range spec.RequiresPositionals {
		if len(parsed.positionals) >= requirement.Minimum {
			continue
		}
		for _, option := range requirement.Options {
			if parsed.has(option) {
				return parsed, usageError("%s", requirement.Error)
			}
		}
	}
	return parsed, nil
}

func positionalRange(positionals []PositionalSpec) (int, int) {
	min := 0
	max := len(positionals)
	for _, positional := range positionals {
		if positional.Required {
			min++
		}
		if positional.Repeatable {
			max = -1
		}
	}
	return min, max
}

func ValidateSchema() error {
	names := map[string]bool{}
	for _, name := range help.Names() {
		if name == "" || names[name] {
			return fmt.Errorf("invalid help name %q", name)
		}
		names[name] = true
	}
	commands := map[Command]bool{}
	for _, spec := range commandSpecs {
		if spec.Command == "" || spec.Name == "" {
			return fmt.Errorf("command has empty identity")
		}
		if commands[spec.Command] {
			return fmt.Errorf("duplicate command %s", spec.Command)
		}
		commands[spec.Command] = true
		for _, name := range append([]string{spec.Name}, spec.Aliases...) {
			if names[name] {
				return fmt.Errorf("duplicate command name %s", name)
			}
			names[name] = true
		}
		if err := validateOptions(spec.Name, spec.Options); err != nil {
			return err
		}
		for _, conflict := range spec.Conflicts {
			if err := validateConstraintOptions(spec, conflict.Options); err != nil {
				return err
			}
		}
		for _, requirement := range spec.RequiresPositionals {
			if err := validateConstraintOptions(spec, requirement.Options); err != nil {
				return err
			}
		}
		for _, requirement := range spec.ConditionalPositionals {
			if requirement.Index < 0 || requirement.Index >= len(spec.Positionals) || requirement.Prefix == "" || requirement.Minimum <= requirement.Index || requirement.Error == "" {
				return fmt.Errorf("%s has invalid conditional positional requirement", spec.Name)
			}
		}
		for _, option := range spec.Options {
			if option.Availability.ForbiddenPrefix != "" && spec.AvailabilityError == "" {
				return fmt.Errorf("%s option %s has availability without an error", spec.Name, option.Long)
			}
		}
		for i, positional := range spec.Positionals {
			if positional.Name == "" || positional.Usage == "" {
				return fmt.Errorf("%s positional %d has incomplete metadata", spec.Name, i)
			}
			if positional.Repeatable && i != len(spec.Positionals)-1 {
				return fmt.Errorf("%s repeatable positional must be last", spec.Name)
			}
			if positional.Completion == CompletionStatic && len(positional.Values) == 0 {
				return fmt.Errorf("%s static positional %s has no values", spec.Name, positional.Name)
			}
			if i > 0 && positional.Required && !spec.Positionals[i-1].Required {
				return fmt.Errorf("%s required positional follows optional positional", spec.Name)
			}
		}
	}
	if err := validateOptions("global", globalOptions); err != nil {
		return err
	}
	global := CommandSpec{Name: "global", Options: globalOptions}
	for _, conflict := range globalConflicts {
		if err := validateConstraintOptions(global, conflict.Options); err != nil {
			return err
		}
	}
	return nil
}

func validateConstraintOptions(spec CommandSpec, options []string) error {
	if len(options) == 0 {
		return fmt.Errorf("%s has an empty option constraint", spec.Name)
	}
	for _, name := range options {
		option, ok := spec.Option(name)
		if !ok || option.Long != name {
			return fmt.Errorf("%s constraint references unknown long option %s", spec.Name, name)
		}
	}
	return nil
}

func validateOptions(owner string, options []OptionSpec) error {
	names := map[string]bool{}
	for _, option := range options {
		if option.Long == "" || !strings.HasPrefix(option.Long, "--") {
			return fmt.Errorf("%s option has invalid long name %q", owner, option.Long)
		}
		if !option.TakesValue() && option.Completion != CompletionNone {
			return fmt.Errorf("%s boolean option %s has a completion role", owner, option.Long)
		}
		for _, name := range []string{option.Long, option.Short} {
			if name == "" {
				continue
			}
			if names[name] {
				return fmt.Errorf("%s has duplicate option %s", owner, name)
			}
			names[name] = true
		}
	}
	return nil
}
