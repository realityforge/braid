package command

import (
	"context"
	"fmt"
	"io"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
)

type StatusHandler struct {
	Options Options
}

func (h StatusHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandStatus, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.statusGit(repo, inv, stderr)
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

	if inv.Status.LocalPath != "" {
		localPath, err := normalizeLocalPath(repo, inv.Status.LocalPath)
		if err != nil {
			return err
		}
		m, err := cfg.GetRequired(localPath)
		if err != nil {
			return err
		}
		return h.statusOne(ctx, git, cache, m, inv.Global.Verbose, stdout, stderr)
	}

	for _, localPath := range cfg.Paths() {
		if err := h.statusOne(ctx, git, cache, cfg.Mirrors[localPath], inv.Global.Verbose, stdout, stderr); err != nil {
			return err
		}
	}
	return nil
}

func (h StatusHandler) statusGit(repo RepoContext, inv cli.Invocation, trace io.Writer) StatusGit {
	if git, ok := h.Options.Git.(StatusGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(StatusGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (h StatusHandler) statusOne(ctx context.Context, git StatusGit, cache CacheConfig, m mirror.Mirror, verbose bool, stdout, trace io.Writer) (err error) {
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m.URL, verbose, trace); err != nil {
			return err
		}
	}
	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return err
	}
	defer func() {
		removeErr := git.RemoteRemove(ctx, m.Remote())
		if err == nil {
			err = removeErr
		}
	}()

	if err := fetchMirror(ctx, git, m); err != nil {
		return err
	}
	baseRevision, err := git.RevParse(ctx, m.Revision+"^{commit}")
	if err != nil {
		return err
	}
	newRevision, err := resolveAddRevision(ctx, git, m, "")
	if err != nil {
		return err
	}

	states := []string{}
	if newRevision != baseRevision {
		states = append(states, "Remote Modified")
	}

	files, err := git.LsFiles(ctx, m.Path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(files) == "" {
		states = append(states, "Removed Locally")
	} else {
		modified, err := locallyModified(ctx, git, m)
		if err != nil {
			return err
		}
		if modified {
			states = append(states, "Locally Modified")
		}
	}

	if _, err := fmt.Fprintf(stdout, "%s (%s) [%s]", m.Path, baseRevision, trackingLabel(m)); err != nil {
		return err
	}
	for _, state := range states {
		if _, err := fmt.Fprintf(stdout, " (%s)", state); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(stdout)
	return err
}

func trackingLabel(m mirror.Mirror) string {
	switch {
	case m.Locked():
		return "REVISION LOCKED"
	case m.Tag != "":
		return "TAG=" + m.Tag
	default:
		return "BRANCH=" + m.Branch
	}
}

func locallyModified(ctx context.Context, git StatusGit, m mirror.Mirror) (bool, error) {
	args, err := buildDiffArgs(ctx, git, m, nil)
	if err != nil {
		return false, err
	}
	out, err := git.Diff(ctx, args...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
