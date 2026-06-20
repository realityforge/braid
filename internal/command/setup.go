package command

import (
	"context"
	"io"

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
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	cache, err := runtimeCache(inv.Global)
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
		return setupOne(ctx, git, m, inv.Setup.Force, cache)
	}

	for _, localPath := range cfg.Paths() {
		if err := setupOne(ctx, git, cfg.Mirrors[localPath], inv.Setup.Force, cache); err != nil {
			return err
		}
	}
	return nil
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
	remote := m.Remote()
	if _, ok, err := git.RemoteURL(ctx, remote); err != nil {
		return err
	} else if ok {
		if !force {
			return nil
		}
		if err := git.RemoteRemove(ctx, remote); err != nil {
			return err
		}
	}

	url := m.URL
	if cache.Enabled {
		url = CachePath(cache.Dir, m.URL)
	}
	return git.RemoteAdd(ctx, remote, url)
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
