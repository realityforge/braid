package command

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
)

type RemoveHandler struct {
	Options Options
}

func (h RemoveHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	if err := Preflight(ctx, cli.CommandRemove, inv, h.Options, stderr); err != nil {
		return err
	}

	git := h.removeGit(inv, stderr)
	head, err := git.Head(ctx)
	if err != nil {
		return err
	}
	if err := h.remove(ctx, git, inv.Remove); err != nil {
		return resetRemoveOnError(ctx, git, head, err)
	}
	return nil
}

func (h RemoveHandler) removeGit(inv cli.Invocation, trace io.Writer) RemoveGit {
	if git, ok := h.Options.Git.(RemoveGit); ok {
		return git
	}
	return gitexec.New(workDir(h.Options.WorkDir), verbose(inv), trace)
}

func (h RemoveHandler) remove(ctx context.Context, git RemoveGit, options cli.RemoveOptions) error {
	cfg, err := config.Load(configRoot(h.Options))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	m, err := cfg.GetRequired(options.LocalPath)
	if err != nil {
		return err
	}

	if err := git.RemoveRecursive(ctx, m.Path); err != nil {
		return err
	}
	if err := cfg.Remove(m.Path); err != nil {
		return err
	}
	if err := cfg.WriteFile(filepath.Join(configRoot(h.Options), config.FileName)); err != nil {
		return err
	}
	if err := git.Add(ctx, config.FileName); err != nil {
		return err
	}
	if !options.Keep {
		if _, ok, err := git.RemoteURL(ctx, m.Remote()); err != nil {
			return err
		} else if ok {
			if err := git.RemoteRemove(ctx, m.Remote()); err != nil {
				return err
			}
		}
	}
	_, err = git.CommitMessage(ctx, removeCommitSubject(m))
	return err
}

func resetRemoveOnError(ctx context.Context, git RemoveGit, head string, cause error) error {
	if resetErr := git.ResetHard(ctx, head); resetErr != nil {
		return fmt.Errorf("%w; failed to reset to %s: %w", cause, shortRevision(head), resetErr)
	}
	return cause
}

func removeCommitSubject(m mirror.Mirror) string {
	return fmt.Sprintf("Braid: Remove mirror '%s'", m.Path)
}
