package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
)

type CompletionHandler struct {
	Options Options
}

type CompleteHandler struct {
	Options Options
}

const bashCompletionScript = `# bash completion for braid

_braid()
{
    local candidate
    COMPREPLY=()
    if type compopt >/dev/null 2>&1; then
        compopt +o default +o bashdefault 2>/dev/null || true
    fi
    while IFS= read -r candidate; do
        COMPREPLY[${#COMPREPLY[@]}]="$candidate"
    done < <("${COMP_WORDS[0]}" __complete bash -- "${COMP_WORDS[@]:1}" 2>/dev/null)
    return 0
}

complete -o default -o bashdefault -F _braid braid
`

type completionFlag struct {
	long         string
	short        string
	value        bool
	completion   cli.CompletionRole
	availability cli.Availability
}

type completionLine struct {
	current       string
	globalArgs    []string
	command       string
	commandArgs   []string
	commandFound  bool
	invalidGlobal bool
}

type commandArgState struct {
	positionals []string
}

func globalCompletionFlags() []completionFlag {
	help := cli.Help()
	return append(completionFlags(cli.GlobalOptionsSpec()), completionFlag{long: help.Long, short: help.Short})
}

func completionFlags(options []cli.OptionSpec) []completionFlag {
	flags := make([]completionFlag, 0, len(options))
	for _, option := range options {
		flags = append(flags, completionFlag{long: option.Long, short: option.Short, value: option.TakesValue(), completion: option.Completion, availability: option.Availability})
	}
	return flags
}

func rootCompletionCommands() []string {
	commands := []string{cli.Help().Command}
	for _, spec := range cli.CommandSpecs() {
		if spec.Hidden {
			continue
		}
		commands = append(commands, spec.Name)
		commands = append(commands, spec.Aliases...)
	}
	return commands
}

func (h CompletionHandler) Run(inv cli.Invocation, stdout, _ io.Writer) error {
	if inv.Completion.Shell != "bash" {
		return fmt.Errorf("unknown completion shell %s", inv.Completion.Shell)
	}
	_, err := io.WriteString(stdout, bashCompletionScript)
	return err
}

func (h CompleteHandler) Run(inv cli.Invocation, stdout, _ io.Writer) error {
	if inv.Complete.Shell != "bash" {
		return nil
	}
	candidates := h.completeBash(context.Background(), inv.Complete.Args)
	for _, candidate := range candidates {
		if _, err := fmt.Fprintln(stdout, candidate); err != nil {
			return err
		}
	}
	return nil
}

func (h CompleteHandler) completeBash(ctx context.Context, words []string) []string {
	line := parseCompletionLine(words)
	if line.invalidGlobal {
		return nil
	}
	if !line.commandFound {
		return h.completeRoot(line)
	}
	return h.completeCommand(ctx, line)
}

func (h CompleteHandler) completeRoot(line completionLine) []string {
	flags := globalCompletionFlags()
	if helpPresent(line.globalArgs) {
		return nil
	}
	if flag, ok := valueFlagAwaitingCurrent(line.globalArgs, flags); ok {
		return h.optionValueCandidates(flag, line.current, "")
	}
	if name, value, ok := strings.Cut(line.current, "="); ok {
		if flag, _, matched := matchCompletionFlag(name, flags); matched {
			return h.optionValueCandidates(flag, value, name+"=")
		}
	}

	var candidates []string
	if line.current == "" || strings.HasPrefix(line.current, "-") {
		candidates = append(candidates, optionCandidates(line.current, flags, line.globalArgs, cli.GlobalConflictsSpec(), nil)...)
	}
	if line.current == "" || !strings.HasPrefix(line.current, "-") {
		candidates = append(candidates, prefixed(rootCompletionCommands(), line.current)...)
	}
	return uniqueSorted(candidates)
}

func (h CompleteHandler) optionValueCandidates(flag completionFlag, current, prefix string) []string {
	switch flag.completion {
	case cli.CompletionDirectory:
		return pathCandidates(h.Options.WorkDir, current, true, prefix)
	case cli.CompletionFilesystem:
		return pathCandidates(h.Options.WorkDir, current, false, prefix)
	default:
		return nil
	}
}

func (h CompleteHandler) completeCommand(ctx context.Context, line completionLine) []string {
	if line.command == string(cli.CommandComplete) {
		return nil
	}
	spec, ok := cli.CommandSpecForName(line.command)
	if !ok || spec.Hidden {
		return nil
	}
	if helpPresent(line.commandArgs) {
		return nil
	}
	flags := completionFlags(spec.Options)
	if spec.Passthrough != nil {
		flags = append(flags, completionFlag{long: "--"})
	}
	if len(line.commandArgs) == 0 {
		help := cli.Help()
		flags = append(append([]completionFlag(nil), flags...), completionFlag{long: help.Long, short: help.Short})
	}
	if flag, ok := valueFlagAwaitingCurrent(line.commandArgs, flags); ok {
		return h.optionValueCandidates(flag, line.current, "")
	}
	if name, value, hasEquals := strings.Cut(line.current, "="); hasEquals {
		if flag, _, matched := matchCompletionFlag(name, flags); matched {
			return h.optionValueCandidates(flag, value, name+"=")
		}
	}
	if spec.Passthrough != nil && commandArgsContainDiffSeparator(line.commandArgs) {
		return pathCandidates(h.Options.WorkDir, line.current, false, "")
	}

	state := parseCompletedCommandArgs(line.commandArgs, flags)
	var candidates []string
	if line.current == "" || strings.HasPrefix(line.current, "-") {
		candidates = append(candidates, optionCandidates(line.current, flags, line.commandArgs, spec.Conflicts, state.positionals)...)
	}
	if line.current == "" || !strings.HasPrefix(line.current, "-") {
		candidates = append(candidates, h.positionalCandidates(ctx, spec, line, state)...)
		if len(line.commandArgs) == 0 {
			candidates = append(candidates, prefixed([]string{cli.Help().Command}, line.current)...)
		}
	}
	return uniqueSorted(candidates)
}

func helpPresent(args []string) bool {
	for _, arg := range args {
		if cli.Help().Matches(arg) {
			return true
		}
	}
	return false
}

func (h CompleteHandler) positionalCandidates(ctx context.Context, spec cli.CommandSpec, line completionLine, state commandArgState) []string {
	position, ok := positionalAt(spec.Positionals, len(state.positionals))
	if !ok {
		return nil
	}
	switch position.Completion {
	case cli.CompletionFilesystem:
		return pathCandidates(h.Options.WorkDir, line.current, false, "")
	case cli.CompletionMirror:
		return h.mirrorPathCandidates(ctx, line.current, state.positionals)
	case cli.CompletionStatic:
		return prefixed(position.Values, line.current)
	default:
		return nil
	}
}

func positionalAt(positionals []cli.PositionalSpec, index int) (cli.PositionalSpec, bool) {
	if index < len(positionals) {
		return positionals[index], true
	}
	if len(positionals) > 0 && positionals[len(positionals)-1].Repeatable {
		return positionals[len(positionals)-1], true
	}
	return cli.PositionalSpec{}, false
}

func parseCompletionLine(words []string) completionLine {
	line := completionLine{}
	if len(words) == 0 {
		words = []string{""}
	}
	line.current = words[len(words)-1]
	completed := words[:len(words)-1]

	globalFlags := globalCompletionFlags()
	for i := 0; i < len(completed); {
		word := completed[i]
		if flag, inlineValue, ok := matchCompletionFlag(word, globalFlags); ok {
			line.globalArgs = append(line.globalArgs, word)
			i++
			if flag.value && inlineValue == "" && i < len(completed) {
				line.globalArgs = append(line.globalArgs, completed[i])
				i++
			}
			continue
		}
		if strings.HasPrefix(word, "-") {
			line.invalidGlobal = true
			return line
		}
		line.command = word
		line.commandArgs = append([]string(nil), completed[i+1:]...)
		line.commandFound = true
		return line
	}
	return line
}

func parseCompletedCommandArgs(args []string, flags []completionFlag) commandArgState {
	var state commandArgState
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return state
		}
		if strings.HasPrefix(arg, "-") {
			flag, inlineValue, ok := matchCompletionFlag(arg, flags)
			if ok && flag.value && inlineValue == "" {
				i++
			}
			continue
		}
		state.positionals = append(state.positionals, arg)
	}
	return state
}

func commandArgsContainDiffSeparator(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return true
		}
	}
	return false
}

func optionCandidates(prefix string, flags []completionFlag, args []string, conflicts []cli.ConflictSpec, positionals []string) []string {
	var candidates []string
	for _, flag := range flags {
		if !flag.availability.Allows(positionals) {
			continue
		}
		if suppressFlag(flag, flags, args, conflicts) {
			continue
		}
		for _, name := range []string{flag.long, flag.short} {
			if name == "" {
				continue
			}
			if strings.HasPrefix(name, prefix) {
				candidates = append(candidates, name)
			}
		}
	}
	return uniqueSorted(candidates)
}

func suppressFlag(flag completionFlag, flags []completionFlag, args []string, conflicts []cli.ConflictSpec) bool {
	if !flag.value && flagPresent(flag, args) {
		return true
	}
	for _, conflict := range conflicts {
		contains := false
		otherPresent := false
		for _, option := range conflict.Options {
			if option == flag.long {
				contains = true
				continue
			}
			otherPresent = otherPresent || flagPresent(completionFlagForLong(flags, option), args)
		}
		if contains && otherPresent {
			return true
		}
	}
	return false
}

func completionFlagForLong(flags []completionFlag, long string) completionFlag {
	for _, flag := range flags {
		if flag.long == long {
			return flag
		}
	}
	return completionFlag{long: long}
}

func flagPresent(flag completionFlag, args []string) bool {
	for _, arg := range args {
		name := arg
		if before, _, ok := strings.Cut(arg, "="); ok {
			name = before
		}
		if name == flag.long || (flag.short != "" && name == flag.short) {
			return true
		}
	}
	return false
}

func valueFlagAwaitingCurrent(args []string, flags []completionFlag) (completionFlag, bool) {
	if len(args) == 0 {
		return completionFlag{}, false
	}
	flag, inlineValue, ok := matchCompletionFlag(args[len(args)-1], flags)
	if !ok || !flag.value || inlineValue != "" {
		return completionFlag{}, false
	}
	return flag, true
}

func matchCompletionFlag(arg string, flags []completionFlag) (completionFlag, string, bool) {
	name := arg
	inlineValue := ""
	if before, after, ok := strings.Cut(arg, "="); ok {
		name = before
		inlineValue = after
	}
	for _, flag := range flags {
		if name == flag.long || (flag.short != "" && name == flag.short) {
			return flag, inlineValue, true
		}
	}
	return completionFlag{}, "", false
}

func (h CompleteHandler) mirrorPathCandidates(ctx context.Context, current string, selected []string) []string {
	repo, _, err := ResolveRepoContext(ctx, cli.Invocation{}, h.Options, io.Discard)
	if err != nil || repo.GitWorkTreeRoot == "" {
		return nil
	}
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return nil
	}

	selectedPaths := map[string]bool{}
	selectedSources := map[string]bool{}
	for _, value := range selected {
		if selection, selectionErr := resolveSourceSelection(repo, cfg, value, false); selectionErr == nil {
			selectedSources[selection.Source.Name] = true
			continue
		}
		localPath, err := normalizeLocalPath(repo, value)
		if err == nil {
			selectedPaths[localPath] = true
		}
	}

	var candidates []string
	for _, name := range cfg.SourceNames() {
		candidate := ":" + name
		if !selectedSources[name] && strings.HasPrefix(candidate, current) {
			candidates = append(candidates, candidate)
		}
	}
	for _, item := range cfg.MirrorsSorted() {
		localPath := item.LocalPath
		if selectedSources[item.Name] {
			continue
		}
		if selectedPaths[localPath] {
			continue
		}
		candidate, ok := relativeMirrorCandidate(repo.WorkTreePrefix, localPath)
		if !ok {
			continue
		}
		candidate = applyCurrentPathPrefix(current, candidate)
		if strings.HasPrefix(candidate, current) {
			candidates = append(candidates, candidate)
		}
	}
	return uniqueSorted(candidates)
}

func relativeMirrorCandidate(prefix, localPath string) (string, bool) {
	from := "."
	if prefix != "" {
		from = filepath.FromSlash(prefix)
	}
	relative, err := filepath.Rel(from, filepath.FromSlash(localPath))
	if err != nil {
		return "", false
	}
	relative = filepath.ToSlash(relative)
	if relative == "" {
		return ".", true
	}
	return relative, true
}

func applyCurrentPathPrefix(current, candidate string) string {
	if strings.HasPrefix(current, "./") && candidate != "." && !strings.HasPrefix(candidate, ".") {
		return "./" + candidate
	}
	return candidate
}

func pathCandidates(baseDir, current string, dirsOnly bool, candidatePrefix string) []string {
	dir, base := path.Split(current)
	readDir := dir
	if readDir == "" {
		readDir = "."
	}

	nativeDir := filepath.FromSlash(readDir)
	if baseDir != "" && !filepath.IsAbs(nativeDir) {
		nativeDir = filepath.Join(baseDir, nativeDir)
	}

	entries, err := os.ReadDir(nativeDir)
	if err != nil {
		return nil
	}

	var candidates []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if !strings.HasPrefix(name, base) {
			continue
		}
		if dirsOnly && !entry.IsDir() {
			continue
		}
		candidate := dir + name
		if entry.IsDir() {
			candidate += "/"
		}
		candidates = append(candidates, candidatePrefix+candidate)
	}
	return uniqueSorted(candidates)
}

func prefixed(values []string, prefix string) []string {
	var candidates []string
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			candidates = append(candidates, value)
		}
	}
	return candidates
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
