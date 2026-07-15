package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"braid/internal/gitexec"
	"braid/internal/source"
)

const (
	pushMessageCommandEnv           = "BRAID_PUSH_COMMIT_MESSAGE_COMMAND"
	pushMessageInlineDiffLimit      = 5 * 1024
	pushMessageGeneratorOutputLimit = 4 * 1024
	pushMessageTruncationMarker     = "[truncated after 4096 bytes]"
	pushMessagePromptFileName       = "prompt.txt"
	pushMessageOutputFileName       = "message.txt"
	pushMessageSeedFileName         = "commit-message-seed.txt"
	pushMessageLargeDiffFileName    = "upstream.diff"
)

type pushMessageGeneration struct {
	Enabled         bool
	CommandTemplate string
	ShellPath       string
}

type pushMessageCommandValues struct {
	RepoDir     string
	ContextDir  string
	PromptFile  string
	MessageFile string
}

type pushMessageDiffContext struct {
	ByteLen  int
	Inline   string
	FilePath string
}

type pushMessagePromptData struct {
	Mirror        source.SourceMirror
	Branch        string
	BaseRevision  string
	NewTree       string
	RepoDir       string
	Diff          pushMessageDiffContext
	Provenance    pushProvenance
	ProvenanceOK  bool
	ProvenanceErr error
}

type pushMessageGeneratorFailure struct {
	Summary string
	Stdout  string
	Stderr  string
}

type limitedOutput struct {
	limit     int
	builder   strings.Builder
	truncated bool
}

func configuredPushMessageGeneration() pushMessageGeneration {
	value, ok := os.LookupEnv(pushMessageCommandEnv)
	if !ok || value == "" {
		return pushMessageGeneration{}
	}
	return pushMessageGeneration{Enabled: true, CommandTemplate: value}
}

func preparePushMessageSeed(ctx context.Context, repo RepoContext, source PushGit, tempGit gitexec.Git, m source.SourceMirror, branch, baseRevision, newTree, contextDir string, generation pushMessageGeneration, verbose bool, trace io.Writer, provenance pushProvenance, provenanceOK bool, provenanceErr error) (string, error) {
	commentChar, err := pushMessageSeedCommentChar(ctx, source)
	if err != nil {
		return "", err
	}
	if err := tempGit.ConfigSet(ctx, "--local", "core.commentChar", commentChar); err != nil {
		return "", err
	}

	diff, err := pushMessageDiff(ctx, tempGit, m)
	if err != nil {
		return "", fmt.Errorf("generate upstream diff for commit-message prompt: %w", err)
	}
	diffContext, err := writePushMessageDiffContext(contextDir, diff)
	if err != nil {
		return "", err
	}

	promptPath := filepath.Join(contextDir, pushMessagePromptFileName)
	messagePath := filepath.Join(contextDir, pushMessageOutputFileName)
	seedPath := filepath.Join(contextDir, pushMessageSeedFileName)
	prompt := formatPushMessagePrompt(pushMessagePromptData{
		Mirror:        m,
		Branch:        branch,
		BaseRevision:  baseRevision,
		NewTree:       newTree,
		RepoDir:       repo.GitWorkTreeRoot,
		Diff:          diffContext,
		Provenance:    provenance,
		ProvenanceOK:  provenanceOK,
		ProvenanceErr: provenanceErr,
	})
	if err := os.WriteFile(promptPath, []byte(prompt), 0o644); err != nil {
		return "", fmt.Errorf("write push commit-message prompt: %w", err)
	}

	progress := trace
	if progress == nil {
		progress = io.Discard
	}
	if _, err := fmt.Fprintf(progress, "Braid: generating push commit message for %s using external tool\n", m.LocalPath); err != nil {
		return "", err
	}
	generated, failure, err := runPushMessageGenerator(ctx, generation.ShellPath, generation.CommandTemplate, pushMessageCommandValues{
		RepoDir:     repo.GitWorkTreeRoot,
		ContextDir:  contextDir,
		PromptFile:  promptPath,
		MessageFile: messagePath,
	}, verbose, trace)
	if err != nil {
		return "", err
	}

	var seed string
	if failure != nil {
		seed = formatPushMessageFailureSeed(commentChar, m, provenance, provenanceOK, *failure)
	} else {
		seed = formatPushMessageSuccessSeed(commentChar, generated, m, provenance, provenanceOK)
	}
	if err := os.WriteFile(seedPath, []byte(seed), 0o644); err != nil {
		return "", fmt.Errorf("write push commit-message seed: %w", err)
	}
	return seedPath, nil
}

func pushMessageSeedCommentChar(ctx context.Context, git PushGit) (string, error) {
	value, ok, err := git.CoreCommentChar(ctx)
	if err != nil {
		return "", err
	}
	if !ok || value == "" || value == "auto" || utf8.RuneCountInString(value) != 1 {
		return "#", nil
	}
	return value, nil
}

func pushMessageDiff(ctx context.Context, git gitexec.Git, m source.SourceMirror) (string, error) {
	return git.Diff(ctx, pushMessageDiffArgs("")...)
}

func pushMessageDiffArgs(remotePath string) []string {
	args := []string{"--cached", "--no-color", "--no-ext-diff", "--no-textconv", "--binary", "HEAD", "--"}
	if remotePath != "" {
		args = append(args, remotePath)
	}
	return args
}

func writePushMessageDiffContext(contextDir, diff string) (pushMessageDiffContext, error) {
	diffContext := pushMessageDiffContext{ByteLen: len(diff)}
	if len(diff) <= pushMessageInlineDiffLimit {
		diffContext.Inline = diff
		return diffContext, nil
	}
	diffPath := filepath.Join(contextDir, pushMessageLargeDiffFileName)
	if err := os.WriteFile(diffPath, []byte(diff), 0o644); err != nil {
		return pushMessageDiffContext{}, fmt.Errorf("write large push diff context: %w", err)
	}
	diffContext.FilePath = diffPath
	return diffContext, nil
}

func formatPushMessagePrompt(data pushMessagePromptData) string {
	var b strings.Builder
	b.WriteString("Generate a Git commit message for an upstream commit created by braid push.\n")
	b.WriteString("The commit will be written to the mirrored/upstream repository, not to the downstream/hosting repository that contains the local source.\n")
	b.WriteString("Describe the staged mirror changes from the mirrored repository's perspective: focus on what changed in the upstream project files at the upstream path below.\n")
	b.WriteString("Use downstream commit provenance only as background for intent; do not frame the message around vendoring, updating a mirror, syncing from the hosting repository, local mirror paths, or .braids.json unless those are part of the upstream content change itself.\n")
	b.WriteString("Respond only with the proposed commit message. Do not include commentary, Markdown fences, explanations, or any other content. The user will review the message in Git's editor before Braid commits.\n\n")
	b.WriteString("Source metadata:\n")
	fmt.Fprintf(&b, "- Source name: %s\n", data.Mirror.Name)
	fmt.Fprintf(&b, "- Upstream URL: %s\n", data.Mirror.URL)
	b.WriteString("- Mirrors:\n")
	for _, mirror := range data.Mirror.SortedMirrors() {
		upstream := mirror.UpstreamPath
		if upstream == "" {
			upstream = "(repository root)"
		}
		fmt.Fprintf(&b, "  - %s -> %s\n", mirror.LocalPath, upstream)
	}
	fmt.Fprintf(&b, "- Recorded base revision: %s\n", data.BaseRevision)
	fmt.Fprintf(&b, "- Synthetic upstream tree: %s\n", data.NewTree)
	fmt.Fprintf(&b, "- Target branch: %s\n", data.Branch)
	fmt.Fprintf(&b, "- Downstream repository root: %s\n\n", data.RepoDir)

	b.WriteString("Downstream commit provenance:\n")
	b.WriteString(formatPushMessagePromptProvenance(data.Provenance, data.ProvenanceOK, data.ProvenanceErr))
	b.WriteString("\nUpstream staged diff:\n")
	if data.Diff.FilePath != "" {
		fmt.Fprintf(&b, "Full diff file: %s\n", data.Diff.FilePath)
		fmt.Fprintf(&b, "Diff byte length: %d\n", data.Diff.ByteLen)
		b.WriteString("The full diff exceeded the inline prompt limit; read the file above for the exact staged upstream changes.\n")
	} else {
		fmt.Fprintf(&b, "Inline diff byte length: %d\n", data.Diff.ByteLen)
		b.WriteString("```diff\n")
		b.WriteString(data.Diff.Inline)
		if !strings.HasSuffix(data.Diff.Inline, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("```\n")
	}
	return b.String()
}

func formatPushMessagePromptProvenance(provenance pushProvenance, ok bool, err error) string {
	if err != nil {
		return fmt.Sprintf("Unavailable: %v\n", err)
	}
	if !ok || len(provenance.Commits) == 0 {
		return "No eligible downstream commits were found for this mirror push.\n"
	}

	var b strings.Builder
	if provenance.NoCleanAnchor {
		b.WriteString("No clean mirror anchor was found; all reachable commits for the current mirror identity are shown, subject to the normal cap.\n\n")
	}
	if provenance.Omitted > 0 {
		b.WriteString(omittedPushProvenanceMessage(provenance.Omitted))
		b.WriteString("\n\n")
	}
	for _, commit := range provenance.Commits {
		fmt.Fprintf(&b, "Commit %s\n", commit.Hash)
		b.WriteString(commit.Message)
		if !strings.HasSuffix(commit.Message, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func resolvePushMessageGeneration(ctx context.Context, git PushGit, generation pushMessageGeneration) (pushMessageGeneration, error) {
	if !generation.Enabled || generation.ShellPath != "" {
		return generation, nil
	}
	shellPath, err := git.ShellPath(ctx)
	if err != nil {
		return pushMessageGeneration{}, fmt.Errorf("resolve POSIX shell with git var GIT_SHELL_PATH: %w", err)
	}
	if shellPath == "" {
		return pushMessageGeneration{}, errors.New("resolve POSIX shell with git var GIT_SHELL_PATH: Git returned an empty path")
	}
	if err := probePushMessageShell(ctx, shellPath); err != nil {
		return pushMessageGeneration{}, err
	}
	generation.ShellPath = shellPath
	return generation, nil
}

func probePushMessageShell(ctx context.Context, shellPath string) error {
	if err := exec.CommandContext(ctx, shellPath, "-c", ":").Run(); err != nil {
		return fmt.Errorf("start Git POSIX shell %q: %w", shellPath, err)
	}
	return nil
}

func runPushMessageGenerator(ctx context.Context, shellPath, commandTemplate string, values pushMessageCommandValues, verbose bool, trace io.Writer) (string, *pushMessageGeneratorFailure, error) {
	command := expandPushMessageCommand(commandTemplate, values)
	if verbose {
		if trace == nil {
			trace = io.Discard
		}
		if _, err := fmt.Fprintf(trace, "Braid: Executing %s in %s\n", gitexec.FormatArgv([]string{shellPath, "-c", command}), values.RepoDir); err != nil {
			return "", nil, err
		}
	}
	cmd := exec.CommandContext(ctx, shellPath, "-c", command)
	cmd.Dir = values.RepoDir
	var stdout, stderr limitedOutput
	stdout.limit = pushMessageGeneratorOutputLimit
	stderr.limit = pushMessageGeneratorOutputLimit
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", &pushMessageGeneratorFailure{
				Summary: fmt.Sprintf("generator exited with status %d", exitErr.ExitCode()),
				Stdout:  stdout.String(),
				Stderr:  stderr.String(),
			}, nil
		}
		return "", nil, fmt.Errorf("run push commit-message generator: %w", err)
	}

	data, err := os.ReadFile(values.MessageFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", &pushMessageGeneratorFailure{
				Summary: "generator did not create the message output file",
				Stdout:  stdout.String(),
				Stderr:  stderr.String(),
			}, nil
		}
		return "", nil, fmt.Errorf("inspect generated push commit message: %w", err)
	}
	message := strings.TrimSpace(string(data))
	if message == "" {
		return "", &pushMessageGeneratorFailure{
			Summary: "generator wrote only whitespace to the message output file",
			Stdout:  stdout.String(),
			Stderr:  stderr.String(),
		}, nil
	}
	return message, nil, nil
}

func expandPushMessageCommand(commandTemplate string, values pushMessageCommandValues) string {
	replacer := strings.NewReplacer(
		"{REPO_DIR}", shellQuote(values.RepoDir),
		"{CONTEXT_DIR}", shellQuote(values.ContextDir),
		"{PROMPT_FILE}", shellQuote(values.PromptFile),
		"{MESSAGE_FILE}", shellQuote(values.MessageFile),
	)
	return replacer.Replace(commandTemplate)
}

func formatPushMessageSuccessSeed(commentChar, generatedMessage string, m source.SourceMirror, provenance pushProvenance, provenanceOK bool) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(generatedMessage))
	b.WriteString("\n")
	if provenanceOK && len(provenance.Commits) > 0 {
		b.WriteString("\n")
		b.WriteString(formatPushProvenanceTemplate(m, provenance.Commits, provenance.Omitted, provenance.NoCleanAnchor, commentChar))
	}
	return b.String()
}

func formatPushMessageFailureSeed(commentChar string, m source.SourceMirror, provenance pushProvenance, provenanceOK bool, failure pushMessageGeneratorFailure) string {
	lines := []string{
		"Braid AI push commit-message generation failed.",
		"Reason: " + failure.Summary,
		"Write the upstream commit message manually. Commented lines will be stripped before commit.",
	}
	if failure.Stdout != "" {
		lines = append(lines, "", "Generator stdout:")
		lines = append(lines, strings.Split(strings.TrimRight(failure.Stdout, "\n"), "\n")...)
	}
	if failure.Stderr != "" {
		lines = append(lines, "", "Generator stderr:")
		lines = append(lines, strings.Split(strings.TrimRight(failure.Stderr, "\n"), "\n")...)
	}

	var b strings.Builder
	b.WriteString(commentLines(commentChar, lines))
	if provenanceOK && len(provenance.Commits) > 0 {
		b.WriteByte('\n')
		b.WriteString(formatPushProvenanceTemplate(m, provenance.Commits, provenance.Omitted, provenance.NoCleanAnchor, commentChar))
	}
	return b.String()
}

func commentLines(commentChar string, lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(commentChar)
		b.WriteByte(' ')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func verifyTempPushIndex(ctx context.Context, git gitexec.Git, newTree string) error {
	actual, err := git.WriteTree(ctx)
	if err != nil {
		return fmt.Errorf("verify temporary push index: %w", err)
	}
	if actual != newTree {
		return fmt.Errorf("temporary push index changed unexpectedly: write-tree produced %s, want %s", actual, newTree)
	}
	return nil
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	if w.limit <= 0 {
		w.truncated = true
		return len(p), nil
	}
	remaining := w.limit - w.builder.Len()
	if remaining > 0 {
		if len(p) <= remaining {
			w.builder.Write(p)
		} else {
			w.builder.Write(p[:remaining])
			w.truncated = true
		}
	} else if len(p) > 0 {
		w.truncated = true
	}
	return len(p), nil
}

func (w *limitedOutput) String() string {
	value := w.builder.String()
	if !w.truncated {
		return value
	}
	if value != "" && !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	return value + pushMessageTruncationMarker
}
