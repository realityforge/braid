package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/source"
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
	return h.remove(ctx, repo, git, inv.Remove, inv.Global.Quiet, stdout)
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

func (h RemoveHandler) remove(ctx context.Context, repo RepoContext, git RemoveGit, options cli.RemoveOptions, quiet bool, stdout io.Writer) error {
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	selection, err := resolveSourceSelection(repo, cfg, options.LocalPath, false)
	if err != nil {
		return err
	}
	s := selection.Source
	m := s.WithMirror(selection.Mirrors[0])
	removeSource := strings.HasPrefix(options.LocalPath, ":") || len(s.Mirrors) == 1
	var paths []string
	if removeSource {
		paths = s.LocalPaths()
	} else {
		paths = []string{m.LocalPath}
	}
	for _, path := range paths {
		if mirrorOverlapsConfig(path) {
			return fmt.Errorf("mirror path %q overlaps %s", path, config.FileName)
		}
	}
	if err := ensureCommandScopesClean(ctx, git, configRoot(h.Options, repo), true, paths...); err != nil {
		return err
	}
	if removeSource {
		err = cfg.RemoveSource(s.Name)
	} else {
		_, _, err = cfg.RemoveMirror(m.LocalPath)
	}
	if err != nil {
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

	treeWithoutMirror := "HEAD"
	for _, path := range paths {
		treeWithoutMirror, err = git.MakeTreeWithoutPath(ctx, treeWithoutMirror, path)
		if err != nil {
			return err
		}
	}
	finalTree, err := git.MakeTreeWithItemIn(ctx, treeWithoutMirror, config.FileName, configItem)
	if err != nil {
		return err
	}
	if options.NoCommit {
		var warned bool
		display := m.LocalPath
		description := "removal of mirror '" + m.LocalPath + "' from source ':" + s.Name + "'"
		if removeSource {
			display = ":" + s.Name
			description = ""
		}
		if err := stageNoCommitResult(ctx, git, stdout, noCommitStageOptions{
			Tree:        finalTree,
			Action:      "removal",
			MirrorPath:  display,
			Description: description,
			Paths:       append(append([]string{}, paths...), config.FileName),
			OwnedPaths:  paths,
			Quiet:       quiet,
			Warned:      &warned,
		}); err != nil {
			return err
		}
		return cleanupRemote(ctx, git, options, m, nil, "staged changes")
	}
	subject := fmt.Sprintf("Braid: Remove mirror '%s' from source '%s'", m.LocalPath, s.Name)
	if removeSource {
		subject = fmt.Sprintf("Braid: Remove source '%s'", s.Name)
	}
	committed, err := git.CommitTreeWithTemporaryIndex(ctx, finalTree, subject)
	if err != nil {
		return err
	}
	if !committed {
		return errors.New("remove produced no commit")
	}

	if err := git.RestorePathspecsFromHead(ctx, append(paths, config.FileName)...); err != nil {
		return cleanupRemote(ctx, git, options, m, err, "")
	}
	return cleanupRemote(ctx, git, options, m, nil, "committed")
}

func cleanupRemote(ctx context.Context, git RemoveGit, options cli.RemoveOptions, m source.SourceMirror, cause error, completed string) error {
	if options.Keep {
		return cause
	}
	if _, ok, err := git.RemoteURL(ctx, m.Remote()); err != nil {
		if cause != nil {
			return fmt.Errorf("%w; failed to inspect Braid remote %q: %w", cause, m.Remote(), err)
		}
		return fmt.Errorf("remove %s but failed to inspect Braid remote %q: %w", completed, m.Remote(), err)
	} else if ok {
		if err := git.RemoteRemove(ctx, m.Remote()); err != nil {
			if cause != nil {
				return fmt.Errorf("%w; failed to remove Braid remote %q: %w", cause, m.Remote(), err)
			}
			return fmt.Errorf("remove %s but failed to remove Braid remote %q: %w", completed, m.Remote(), err)
		}
	}
	return cause
}
