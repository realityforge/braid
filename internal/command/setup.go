package command

import (
	"context"
	"errors"
	"io"
	"os"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
	"braid/internal/pathcheck"
)

type SetupHandler struct {
	Options Options
}

func (h SetupHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandSetup, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.remoteGit(repo, inv, stderr)
	progress := newProgressReporter(stderr, inv.Global.Quiet)
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	cache, err := runtimeCacheForRepo(ctx, repo, inv.Global, inv.Global.Verbose, stderr)
	if err != nil {
		return err
	}

	if inv.Setup.LocalPath != "" {
		localPath, err := normalizeLocalPath(repo, inv.Setup.LocalPath)
		if err != nil {
			return err
		}
		m, err := cfg.GetRequired(localPath)
		if err != nil {
			return err
		}
		hydrate, err := shouldHydrateSetupCache(ctx, git, m, inv.Setup.Force, cache)
		if err != nil {
			return err
		}
		if hydrate {
			if err := fetchCache(ctx, cache, m, inv.Global.Verbose, progress, stderr); err != nil {
				return err
			}
		}
		return setupOneWithProgress(ctx, git, m, inv.Setup.Force, cache, progress)
	}

	for _, localPath := range cfg.Paths() {
		m := cfg.Mirrors[localPath]
		hydrate, err := shouldHydrateSetupCache(ctx, git, m, inv.Setup.Force, cache)
		if err != nil {
			return err
		}
		if hydrate {
			if err := fetchCache(ctx, cache, m, inv.Global.Verbose, progress, stderr); err != nil {
				return err
			}
		}
		if err := setupOneWithProgress(ctx, git, m, inv.Setup.Force, cache, progress); err != nil {
			return err
		}
	}
	return nil
}

func shouldHydrateSetupCache(ctx context.Context, git RemoteGit, m mirror.Mirror, force bool, cache CacheConfig) (bool, error) {
	if !cache.Enabled {
		return false, nil
	}
	cacheURL := cache.RemoteURL(m)
	remoteURL, ok, err := git.RemoteURL(ctx, m.Remote())
	if err != nil {
		return false, err
	}
	if !ok || force {
		return true, nil
	}
	if remoteURL != cacheURL {
		return false, nil
	}
	if cache.Mode == CacheModeRepositoryLocal {
		ready, err := repositoryLocalSetupCacheReady(ctx, cache, m)
		if err != nil {
			return false, err
		}
		return !ready, nil
	}
	if _, err := os.Stat(cacheURL); err == nil {
		return false, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}

func repositoryLocalSetupCacheReady(ctx context.Context, cache CacheConfig, m mirror.Mirror) (bool, error) {
	cachePath := cache.RemoteURL(m)
	info, err := os.Stat(cachePath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}

	cacheGit := gitexec.New(cachePath, false, nil)
	if !repositoryLocalBareReady(ctx, cachePath) {
		return false, nil
	}
	if m.Revision != "" {
		recorded, ok := repositoryLocalCommit(ctx, cacheGit, cache.RecordedRef(m)+"^{commit}")
		if !ok {
			return false, nil
		}
		configured, ok := repositoryLocalCommit(ctx, cacheGit, m.Revision+"^{commit}")
		if !ok || configured != recorded {
			return false, nil
		}
	}
	switch {
	case m.Branch != "":
		if !repositoryLocalRefReady(ctx, cacheGit, "refs/heads/"+m.Branch+"^{commit}") {
			return false, nil
		}
	case m.Tag != "":
		if !repositoryLocalRefReady(ctx, cacheGit, "refs/tags/"+m.Tag+"^{commit}") {
			return false, nil
		}
	}
	return true, nil
}

func repositoryLocalBareReady(ctx context.Context, cachePath string) bool {
	bare, err := isBareRepository(ctx, cachePath, false, nil)
	return err == nil && bare
}

func repositoryLocalRefReady(ctx context.Context, git gitexec.Git, rev string) bool {
	_, ok := repositoryLocalCommit(ctx, git, rev)
	return ok
}

func repositoryLocalCommit(ctx context.Context, git gitexec.Git, rev string) (string, bool) {
	commit, err := git.RevParse(ctx, rev)
	if err != nil {
		return "", false
	}
	return commit, true
}

func (h SetupHandler) remoteGit(repo RepoContext, inv cli.Invocation, trace io.Writer) RemoteGit {
	if git, ok := h.Options.Git.(RemoteGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(RemoteGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func setupOne(ctx context.Context, git RemoteGit, m mirror.Mirror, force bool, cache CacheConfig) error {
	return setupOneWithProgress(ctx, git, m, force, cache, progressReporter{})
}

func setupOneWithProgress(ctx context.Context, git RemoteGit, m mirror.Mirror, force bool, cache CacheConfig, progress progressReporter) error {
	remote := m.Remote()
	if _, ok, err := git.RemoteURL(ctx, remote); err != nil {
		return err
	} else if ok {
		if !force {
			return nil
		}
		return runProgress(
			progress,
			"Braid: setting up mirror remote "+m.Path,
			"Braid: set up mirror remote "+m.Path,
			func() error {
				if err := git.RemoteRemove(ctx, remote); err != nil {
					return err
				}
				return setupMirrorRemote(ctx, git, m, cache)
			},
		)
	}

	return runProgress(
		progress,
		"Braid: setting up mirror remote "+m.Path,
		"Braid: set up mirror remote "+m.Path,
		func() error {
			return setupMirrorRemote(ctx, git, m, cache)
		},
	)
}

func setupMirrorRemote(ctx context.Context, git RemoteGit, m mirror.Mirror, cache CacheConfig) error {
	remote := m.Remote()
	return git.RemoteAdd(ctx, remote, cache.RemoteURL(m))
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
