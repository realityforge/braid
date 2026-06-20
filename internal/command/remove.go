package command

import (
	"context"
	"errors"
	"fmt"
	"io"

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
	repo, err := Preflight(ctx, cli.CommandRemove, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.removeGit(repo, inv, stderr)
	return h.remove(ctx, repo, git, inv.Remove)
}

func (h RemoveHandler) removeGit(repo RepoContext, inv cli.Invocation, trace io.Writer) RemoveGit {
	if git, ok := h.Options.Git.(RemoveGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(RemoveGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (h RemoveHandler) remove(ctx context.Context, repo RepoContext, git RemoveGit, options cli.RemoveOptions) error {
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	localPath, err := normalizeLocalPath(repo, options.LocalPath)
	if err != nil {
		return err
	}
	m, err := cfg.GetRequired(localPath)
	if err != nil {
		return err
	}
	if mirrorOverlapsConfig(m.Path) {
		return fmt.Errorf("mirror path %q overlaps %s", m.Path, config.FileName)
	}
	if err := ensureCommandScopesClean(ctx, git, configRoot(h.Options, repo), true, m.Path); err != nil {
		return err
	}

	if err := cfg.Remove(m.Path); err != nil {
		return err
	}
	configData, err := cfg.MarshalJSON()
	if err != nil {
		return err
	}
	configItem, err := git.HashBytes(ctx, configData)
	if err != nil {
		return err
	}

	treeWithoutMirror, err := git.MakeTreeWithoutPath(ctx, "HEAD", m.Path)
	if err != nil {
		return err
	}
	finalTree, err := git.MakeTreeWithItemIn(ctx, treeWithoutMirror, config.FileName, configItem)
	if err != nil {
		return err
	}
	committed, err := git.CommitTreeWithTemporaryIndex(ctx, finalTree, removeCommitSubject(m))
	if err != nil {
		return err
	}
	if !committed {
		return errors.New("remove produced no commit")
	}

	cleanupRemote := func(cause error) error {
		if options.Keep {
			return cause
		}
		if _, ok, err := git.RemoteURL(ctx, m.Remote()); err != nil {
			if cause != nil {
				return fmt.Errorf("%w; failed to inspect Braid remote %q: %w", cause, m.Remote(), err)
			}
			return fmt.Errorf("remove committed but failed to inspect Braid remote %q: %w", m.Remote(), err)
		} else if ok {
			if err := git.RemoteRemove(ctx, m.Remote()); err != nil {
				if cause != nil {
					return fmt.Errorf("%w; failed to remove Braid remote %q: %w", cause, m.Remote(), err)
				}
				return fmt.Errorf("remove committed but failed to remove Braid remote %q: %w", m.Remote(), err)
			}
		}
		return cause
	}
	if err := git.RestorePathspecsFromHead(ctx, m.Path, config.FileName); err != nil {
		return cleanupRemote(err)
	}
	return cleanupRemote(nil)
}

func removeCommitSubject(m mirror.Mirror) string {
	return fmt.Sprintf("Braid: Remove mirror '%s'", m.Path)
}
