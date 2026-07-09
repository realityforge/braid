package command

import (
	"context"
	"fmt"
	"io"
	"strings"

	"braid/internal/config"
)

const unrelatedStagedWarning = "Braid: warning: unrelated staged changes are present; unstage them before committing if they should not be included.\n"

type noCommitGit interface {
	Diff(context.Context, ...string) (string, error)
	RestorePathspecsFromTree(context.Context, string, bool, bool, ...string) error
}

type noCommitStageOptions struct {
	Tree       string
	Action     string
	MirrorPath string
	Paths      []string
	OwnedPaths []string
	Quiet      bool
	Warned     *bool
}

func stageNoCommitResult(ctx context.Context, git noCommitGit, stdout io.Writer, options noCommitStageOptions) error {
	if options.Warned == nil || !*options.Warned {
		staged, err := hasUnrelatedStagedEntries(ctx, git, options.OwnedPaths...)
		if err != nil {
			return err
		}
		if staged {
			if _, err := io.WriteString(stdout, unrelatedStagedWarning); err != nil {
				return err
			}
			if options.Warned != nil {
				*options.Warned = true
			}
		}
	}
	if err := git.RestorePathspecsFromTree(ctx, options.Tree, true, true, options.Paths...); err != nil {
		return err
	}
	if options.Quiet {
		return nil
	}
	_, err := fmt.Fprintf(stdout, "Braid: staged %s of mirror '%s'\n", options.Action, options.MirrorPath)
	return err
}

func hasUnrelatedStagedEntries(ctx context.Context, git interface {
	Diff(context.Context, ...string) (string, error)
}, mirrorPaths ...string) (bool, error) {
	out, err := git.Diff(ctx, "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	for _, path := range strings.Split(out, "\n") {
		path = strings.TrimSpace(path)
		if path == "" || path == config.FileName || pathWithinAny(path, mirrorPaths) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func pathWithinAny(path string, scopes []string) bool {
	for _, scope := range scopes {
		if pathWithin(path, scope) {
			return true
		}
	}
	return false
}
