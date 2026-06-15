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
	if err := Preflight(context.Background(), cli.CommandSetup, inv, h.Options, stderr); err != nil {
		return err
	}

	git := h.remoteGit(inv, stderr)
	cfg, err := config.Load(configRoot(h.Options))
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
		m, err := cfg.GetRequired(inv.Setup.LocalPath)
		if err != nil {
			return err
		}
		return setupOne(context.Background(), git, m, inv.Setup.Force, cache)
	}

	for _, localPath := range cfg.Paths() {
		if err := setupOne(context.Background(), git, cfg.Mirrors[localPath], inv.Setup.Force, cache); err != nil {
			return err
		}
	}
	return nil
}

func (h SetupHandler) remoteGit(inv cli.Invocation, trace io.Writer) RemoteGit {
	if git, ok := h.Options.Git.(RemoteGit); ok {
		return git
	}
	return gitexec.New(workDir(h.Options.WorkDir), inv.Global.Verbose, trace)
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
