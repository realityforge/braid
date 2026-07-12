package command

import (
	"context"
	"errors"
	"fmt"

	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/pathcheck"
	"braid/internal/source"
)

type exactRemoteConfigGit interface {
	SnapshotRemoteConfig(context.Context, string) (gitexec.RemoteConfigSnapshot, error)
	RestoreRemoteConfig(context.Context, string, gitexec.RemoteConfigSnapshot) error
}

func configureMirrorRemote(ctx context.Context, git RemoteGit, m source.SourceMirror, force bool, cache CacheConfig) error {
	return configureMirrorRemoteWithProgress(ctx, git, m, force, cache, progressReporter{})
}

func configureMirrorRemoteWithProgress(ctx context.Context, git RemoteGit, m source.SourceMirror, force bool, cache CacheConfig, progress progressReporter) error {
	remote := m.Remote()
	if url, ok, err := git.RemoteURL(ctx, remote); err != nil {
		return err
	} else if ok {
		if !force {
			return nil
		}
		return runProgress(progress, "Braid: setting up source remote :"+m.Name, "Braid: set up source remote :"+m.Name, func() error {
			var snapshot gitexec.RemoteConfigSnapshot
			if exact, ok := git.(exactRemoteConfigGit); ok {
				var snapshotErr error
				snapshot, snapshotErr = exact.SnapshotRemoteConfig(ctx, remote)
				if snapshotErr != nil {
					return snapshotErr
				}
			}
			if err := git.RemoteRemove(ctx, remote); err != nil {
				return err
			}
			if err := addMirrorRemote(ctx, git, m, cache); err != nil {
				if exact, ok := git.(exactRemoteConfigGit); ok {
					if restoreErr := exact.RestoreRemoteConfig(ctx, remote, snapshot); restoreErr != nil {
						return fmt.Errorf("%w; failed to restore existing remote: %w", err, restoreErr)
					}
					return err
				}
				if _, ok, inspectErr := git.RemoteURL(ctx, remote); inspectErr == nil && ok {
					_ = git.RemoteRemove(ctx, remote)
				}
				if restoreErr := git.RemoteAdd(ctx, remote, url); restoreErr != nil {
					return fmt.Errorf("%w; failed to restore existing remote: %w", err, restoreErr)
				}
				return err
			}
			return nil
		})
	}

	return runProgress(progress, "Braid: setting up source remote :"+m.Name, "Braid: set up source remote :"+m.Name, func() error {
		return addMirrorRemote(ctx, git, m, cache)
	})
}

func addMirrorRemote(ctx context.Context, git RemoteGit, m source.SourceMirror, cache CacheConfig) error {
	remote := m.Remote()
	if err := git.RemoteAdd(ctx, remote, cache.RemoteURL(m)); err != nil {
		return err
	}
	if m.PartialClone && cache.Enabled && cache.Mode == CacheModeRepositoryLocal {
		if configurable, ok := git.(interface {
			ConfigSet(context.Context, ...string) error
		}); ok {
			if err := configurable.ConfigSet(ctx, "remote."+remote+".promisor", "true"); err != nil {
				return err
			}
			if err := configurable.ConfigSet(ctx, "remote."+remote+".partialclonefilter", "blob:none"); err != nil {
				return err
			}
		} else {
			return errors.New("git implementation cannot configure partial clone remote")
		}
	}
	return nil
}

func validateConfigPaths(cfg config.Config) error {
	var existingPaths []string
	for _, m := range cfg.MirrorsSorted() {
		if err := pathcheck.ValidateLocal(m.LocalPath, existingPaths); err != nil {
			return err
		}
		if m.UpstreamPath != "" {
			if err := pathcheck.ValidateUpstream(m.UpstreamPath); err != nil {
				return err
			}
		}
		existingPaths = append(existingPaths, m.LocalPath)
	}
	var existingSources []source.Source
	for _, s := range cfg.SourcesSorted() {
		if err := pathcheck.CheckRemoteCollision(s, existingSources); err != nil {
			return err
		}
		existingSources = append(existingSources, s)
	}
	return nil
}
