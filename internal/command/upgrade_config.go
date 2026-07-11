package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
)

type UpgradeConfigHandler struct{ Options Options }

func (h UpgradeConfigHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandUpgradeConfig, inv, h.Options, stderr)
	if err != nil {
		return err
	}
	git := h.upgradeGit(repo, inv, stderr)
	if err := ensureCommandScopesClean(ctx, git, configRoot(h.Options, repo), true); err != nil {
		return err
	}
	path := filepath.Join(configRoot(h.Options, repo), config.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := config.Parse(data); err == nil {
		if !inv.Global.Quiet {
			_, err = fmt.Fprintln(stdout, "Braid: config is already version 2")
		}
		return err
	}
	cfg, err := config.UpgradeV1(data)
	if err != nil {
		return err
	}
	upgraded, err := cfg.MarshalJSON()
	if err != nil {
		return err
	}
	item, err := git.HashBytes(ctx, upgraded)
	if err != nil {
		return err
	}
	tree, err := git.MakeTreeWithItemIn(ctx, "HEAD", config.FileName, item)
	if err != nil {
		return err
	}
	if inv.UpgradeConfig.NoCommit {
		if err := git.RestorePathspecsFromTree(ctx, tree, true, true, config.FileName); err != nil {
			if restoreErr := git.RestorePathspecsFromHead(ctx, config.FileName); restoreErr != nil {
				return fmt.Errorf("%w; failed to restore config: %w", err, restoreErr)
			}
			return err
		}
		if !inv.Global.Quiet {
			_, err = fmt.Fprintln(stdout, "Braid: staged config upgrade to version 2")
		}
		return err
	}
	originalHead, err := git.RevParse(ctx, "HEAD")
	if err != nil {
		return err
	}
	committed, err := git.CommitTreeWithTemporaryIndex(ctx, tree, "Upgrade Braid config to version 2")
	if err != nil {
		return err
	}
	if !committed {
		return errors.New("config upgrade produced no commit")
	}
	newHead, err := git.RevParse(ctx, "HEAD")
	if err != nil {
		if rollbackErr := git.UpdateRef(ctx, "HEAD", originalHead); rollbackErr != nil {
			return fmt.Errorf("%w; failed to roll back HEAD: %w", err, rollbackErr)
		}
		if restoreErr := git.RestorePathspecsFromHead(ctx, config.FileName); restoreErr != nil {
			return fmt.Errorf("%w; failed to restore config after rollback: %w", err, restoreErr)
		}
		return err
	}
	if err := git.RestorePathspecsFromHead(ctx, config.FileName); err != nil {
		if rollbackErr := git.UpdateRef(ctx, "HEAD", originalHead, newHead); rollbackErr != nil {
			return fmt.Errorf("%w; failed to roll back HEAD: %w", err, rollbackErr)
		}
		if restoreErr := git.RestorePathspecsFromHead(ctx, config.FileName); restoreErr != nil {
			return fmt.Errorf("%w; failed to restore config after rollback: %w", err, restoreErr)
		}
		return err
	}
	if !inv.Global.Quiet {
		_, err = fmt.Fprintln(stdout, "Braid: upgraded config to version 2")
	}
	return err
}

func (h UpgradeConfigHandler) upgradeGit(repo RepoContext, inv cli.Invocation, trace io.Writer) UpgradeConfigGit {
	if git, ok := h.Options.Git.(UpgradeConfigGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(UpgradeConfigGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}
