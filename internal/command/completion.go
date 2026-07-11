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
	long  string
	short string
	value bool
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

var globalCompletionFlags = []completionFlag{
	{long: "--verbose", short: "-v"},
	{long: "--quiet"},
	{long: "--no-cache"},
	{long: "--global-cache-dir", value: true},
}

var commandCompletionFlags = map[string][]completionFlag{
	string(cli.CommandAdd): {
		{long: "--branch", short: "-b", value: true},
		{long: "--tag", short: "-t", value: true},
		{long: "--revision", short: "-r", value: true},
		{long: "--path", short: "-p", value: true},
		{long: "--no-commit"},
		{long: "--partial-clone"},
	},
	string(cli.CommandPull): {
		{long: "--branch", short: "-b", value: true},
		{long: "--tag", short: "-t", value: true},
		{long: "--revision", short: "-r", value: true},
		{long: "--keep"},
		{long: "--no-commit"},
	},
	string(cli.CommandRemove): {
		{long: "--keep"},
		{long: "--no-commit"},
	},
	string(cli.CommandDiff): {
		{long: "--keep"},
		{long: "--"},
	},
	string(cli.CommandPush): {
		{long: "--branch", short: "-b", value: true},
		{long: "--message", short: "-m", value: true},
		{long: "--keep"},
	},
	string(cli.CommandSync): {
		{long: "--pull-only"},
		{long: "--autostash"},
		{long: "--keep"},
	},
	string(cli.CommandStatus):        {},
	string(cli.CommandUpgradeConfig): {{long: "--no-commit"}},
}

var rootCompletionCommands = []string{
	string(cli.CommandAdd),
	string(cli.CommandPull),
	string(cli.CommandRemove),
	string(cli.CommandDiff),
	string(cli.CommandPush),
	string(cli.CommandSync),
	string(cli.CommandStatus),
	string(cli.CommandVersion),
	string(cli.CommandCompletion),
	string(cli.CommandUpgradeConfig),
	"help",
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
	if flag, ok := valueFlagAwaitingCurrent(line.globalArgs, globalCompletionFlags); ok {
		if flag.long == "--global-cache-dir" {
			return pathCandidates(h.Options.WorkDir, line.current, true, "")
		}
		return nil
	}
	if strings.HasPrefix(line.current, "--global-cache-dir=") {
		value := strings.TrimPrefix(line.current, "--global-cache-dir=")
		return pathCandidates(h.Options.WorkDir, value, true, "--global-cache-dir=")
	}

	var candidates []string
	if line.current == "" || strings.HasPrefix(line.current, "-") {
		candidates = append(candidates, optionCandidates(line.current, globalCompletionFlags, line.globalArgs, true)...)
	}
	if line.current == "" || !strings.HasPrefix(line.current, "-") {
		candidates = append(candidates, prefixed(rootCompletionCommands, line.current)...)
	}
	return uniqueSorted(candidates)
}

func (h CompleteHandler) completeCommand(ctx context.Context, line completionLine) []string {
	if line.command == string(cli.CommandComplete) {
		return nil
	}
	if line.command == string(cli.CommandCompletion) {
		return completeCompletionCommand(line)
	}

	command := canonicalCompletionCommand(line.command)
	flags, ok := commandCompletionFlags[command]
	if !ok {
		return nil
	}
	if _, ok := valueFlagAwaitingCurrent(line.commandArgs, flags); ok {
		return nil
	}
	if command == string(cli.CommandDiff) && commandArgsContainDiffSeparator(line.commandArgs) {
		return pathCandidates(h.Options.WorkDir, line.current, false, "")
	}

	state := parseCompletedCommandArgs(command, line.commandArgs, flags)
	var candidates []string
	if line.current == "" || strings.HasPrefix(line.current, "-") {
		candidates = append(candidates, optionCandidates(line.current, flags, line.commandArgs, false)...)
	}
	if line.current == "" || !strings.HasPrefix(line.current, "-") {
		candidates = append(candidates, h.pathCandidatesForCommand(ctx, command, line, state)...)
	}
	return uniqueSorted(candidates)
}

func canonicalCompletionCommand(command string) string {
	switch command {
	case "update", "up":
		return string(cli.CommandPull)
	default:
		return command
	}
}

func completeCompletionCommand(line completionLine) []string {
	state := parseCompletedCommandArgs(line.command, line.commandArgs, nil)
	if len(state.positionals) > 0 || strings.HasPrefix(line.current, "-") {
		return nil
	}
	return prefixed([]string{"bash"}, line.current)
}

func (h CompleteHandler) pathCandidatesForCommand(ctx context.Context, command string, line completionLine, state commandArgState) []string {
	switch command {
	case string(cli.CommandAdd):
		if len(state.positionals) == 1 {
			return pathCandidates(h.Options.WorkDir, line.current, false, "")
		}
	case string(cli.CommandPull), string(cli.CommandRemove), string(cli.CommandDiff), string(cli.CommandPush), string(cli.CommandStatus):
		if len(state.positionals) == 0 {
			return h.mirrorPathCandidates(ctx, line.current, nil)
		}
	case string(cli.CommandSync):
		return h.mirrorPathCandidates(ctx, line.current, state.positionals)
	}
	return nil
}

func parseCompletionLine(words []string) completionLine {
	line := completionLine{}
	if len(words) == 0 {
		words = []string{""}
	}
	line.current = words[len(words)-1]
	completed := words[:len(words)-1]

	for i := 0; i < len(completed); {
		word := completed[i]
		if flag, inlineValue, ok := matchCompletionFlag(word, globalCompletionFlags); ok {
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

func parseCompletedCommandArgs(command string, args []string, flags []completionFlag) commandArgState {
	var state commandArgState
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if command == string(cli.CommandDiff) && arg == "--" {
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

func optionCandidates(prefix string, flags []completionFlag, args []string, global bool) []string {
	var candidates []string
	for _, flag := range flags {
		if suppressFlag(flag, args, global) {
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

func suppressFlag(flag completionFlag, args []string, global bool) bool {
	if !flag.value && flagPresent(flag, args) {
		return true
	}
	if !global {
		return false
	}

	switch flag.long {
	case "--quiet":
		return flagPresent(completionFlag{long: "--verbose", short: "-v"}, args)
	case "--verbose":
		return flagPresent(completionFlag{long: "--quiet"}, args)
	case "--no-cache":
		return flagPresent(completionFlag{long: "--global-cache-dir", value: true}, args)
	case "--global-cache-dir":
		return flagPresent(completionFlag{long: "--no-cache"}, args)
	default:
		return false
	}
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
	for _, value := range selected {
		localPath, err := normalizeLocalPath(repo, value)
		if err == nil {
			selectedPaths[localPath] = true
		}
	}

	var candidates []string
	for _, localPath := range cfg.Paths() {
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
