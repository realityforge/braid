package command

import (
	"context"
	"fmt"
	"io"
	"path"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
)

type DiffHandler struct {
	Options Options
}

func (h DiffHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandDiff, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.diffGit(repo, inv, stderr)
	processGit := h.processDiffGit(repo, inv, stderr)
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

	if inv.Diff.LocalPath != "" {
		localPath, err := normalizeLocalPath(repo, inv.Diff.LocalPath)
		if err != nil {
			return err
		}
		m, err := cfg.GetRequired(localPath)
		if err != nil {
			return err
		}
		return h.diffOne(ctx, git, processGit, cache, m, inv.Diff, inv.Global.Verbose, progress, stdout, stderr)
	}

	for _, localPath := range cfg.Paths() {
		if _, err := fmt.Fprintf(stdout, "=======================================================\nBraid: Diffing %s\n=======================================================\n", localPath); err != nil {
			return err
		}
		if err := h.diffOne(ctx, git, processGit, cache, cfg.Mirrors[localPath], inv.Diff, inv.Global.Verbose, progress, stdout, stderr); err != nil {
			return err
		}
	}
	return nil
}

func (h DiffHandler) diffGit(repo RepoContext, inv cli.Invocation, trace io.Writer) DiffGit {
	if git, ok := h.Options.Git.(DiffGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(DiffGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (h DiffHandler) processDiffGit(repo RepoContext, inv cli.Invocation, trace io.Writer) DiffGit {
	if git, ok := h.Options.Git.(DiffGit); ok {
		return git
	}
	if git, ok := repo.processGit(inv, h.Options, trace).(DiffGit); ok {
		return git
	}
	return gitexec.New(repo.ProcessWorkDir, inv.Global.Verbose, trace)
}

func (h DiffHandler) diffOne(ctx context.Context, git, processGit DiffGit, cache CacheConfig, m mirror.Mirror, options cli.DiffOptions, verbose bool, progress progressReporter, stdout, trace io.Writer) (err error) {
	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return err
	}
	if !options.Keep {
		defer func() {
			removeErr := git.RemoteRemove(ctx, m.Remote())
			if err == nil {
				err = removeErr
			}
		}()
	}

	if err := fetchBaseRevisionIfMissing(ctx, git, cache, m, verbose, progress, trace); err != nil {
		return err
	}
	args, err := buildDiffArgs(ctx, git, m, options.GitDiffArgs)
	if err != nil {
		return err
	}
	out, err := processGit.Diff(ctx, args...)
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, out)
	return err
}

func fetchBaseRevisionIfMissing(ctx context.Context, git DiffGit, cache CacheConfig, m mirror.Mirror, verbose bool, progress progressReporter, trace io.Writer) error {
	if _, err := git.RevParse(ctx, m.Revision+"^{commit}"); err == nil {
		return nil
	}
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m, verbose, progress, trace); err != nil {
			return err
		}
	}
	return fetchMirror(ctx, git, cache, m, progress)
}

func buildDiffArgs(ctx context.Context, git DiffGit, m mirror.Mirror, userArgs []string) ([]string, error) {
	item, err := baseDiffItem(ctx, git, m)
	if err != nil {
		return nil, err
	}
	baseTree, err := git.MakeTreeWithItem(ctx, m.Path, item)
	if err != nil {
		return nil, err
	}

	if item.Type == "blob" {
		args := []string{
			"--relative=" + m.Path,
			"--src-prefix=a/" + path.Base(m.RemotePath),
			"--dst-prefix=b/" + path.Base(m.Path),
			baseTree,
		}
		args = append(args, userArgs...)
		return append(args, ":(top)"+m.Path), nil
	}

	args := []string{"--relative=" + m.Path + "/", baseTree}
	return append(args, userArgs...), nil
}

func baseDiffItem(ctx context.Context, git DiffGit, m mirror.Mirror) (gitexec.TreeItem, error) {
	if m.RemotePath == "" {
		return gitexec.TreeItem{Type: "tree", Hash: m.Revision}, nil
	}
	return git.LsTreeItem(ctx, m.Revision, m.RemotePath)
}
