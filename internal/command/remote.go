package command

import (
	"context"
	"errors"

	"braid/internal/config"
	"braid/internal/mirror"
	"braid/internal/pathcheck"
)

func configureMirrorRemote(ctx context.Context, git RemoteGit, m mirror.Mirror, force bool, cache CacheConfig) error {
	return configureMirrorRemoteWithProgress(ctx, git, m, force, cache, progressReporter{})
}

func configureMirrorRemoteWithProgress(ctx context.Context, git RemoteGit, m mirror.Mirror, force bool, cache CacheConfig, progress progressReporter) error {
	remote := m.Remote()
	if _, ok, err := git.RemoteURL(ctx, remote); err != nil {
		return err
	} else if ok {
		if !force {
			return nil
		}
		return runProgress(progress, "Braid: setting up mirror remote "+m.Path, "Braid: set up mirror remote "+m.Path, func() error {
			if err := git.RemoteRemove(ctx, remote); err != nil {
				return err
			}
			return addMirrorRemote(ctx, git, m, cache)
		})
	}

	return runProgress(progress, "Braid: setting up mirror remote "+m.Path, "Braid: set up mirror remote "+m.Path, func() error {
		return addMirrorRemote(ctx, git, m, cache)
	})
}

func addMirrorRemote(ctx context.Context, git RemoteGit, m mirror.Mirror, cache CacheConfig) error {
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
	var existingMirrors []mirror.Mirror
	for _, localPath := range cfg.Paths() {
		m := cfg.Mirrors[localPath]
		if err := pathcheck.ValidateLocal(m.Path, existingPaths); err != nil {
			return err
		}
		if m.RemotePath != "" {
			if err := pathcheck.ValidateUpstream(m.RemotePath); err != nil {
				return err
			}
		}
		if err := pathcheck.CheckRemoteCollision(m, existingMirrors); err != nil {
			return err
		}
		existingPaths = append(existingPaths, m.Path)
		existingMirrors = append(existingMirrors, m)
	}
	return nil
}
