package command

import (
	"context"
	"fmt"
	"io"
	"path"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/source"
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
		selection, err := resolveSourceSelection(repo, cfg, inv.Diff.LocalPath, true)
		if err != nil {
			return err
		}
		for _, mirror := range selection.Mirrors {
			if len(selection.Mirrors) > 1 {
				if _, err := fmt.Fprintf(stdout, "=======================================================\nBraid: Diffing mirror %s\n=======================================================\n", mirror.LocalPath); err != nil {
					return err
				}
			}
			if err := h.diffOne(ctx, git, processGit, cache, selection.Source.WithMirror(mirror), inv.Diff, inv.Global.Verbose, progress, stdout, stderr); err != nil {
				return err
			}
		}
		return nil
	}

	for _, m := range cfg.MirrorsSorted() {
		localPath := m.LocalPath
		if _, err := fmt.Fprintf(stdout, "=======================================================\nBraid: Diffing mirror %s\n=======================================================\n", localPath); err != nil {
			return err
		}
		if err := h.diffOne(ctx, git, processGit, cache, m, inv.Diff, inv.Global.Verbose, progress, stdout, stderr); err != nil {
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

func (h DiffHandler) diffOne(ctx context.Context, git, processGit DiffGit, cache CacheConfig, m source.SourceMirror, options cli.DiffOptions, verbose bool, progress progressReporter, stdout, trace io.Writer) (err error) {
	if err := configureMirrorRemote(ctx, git, m, true, cache); err != nil {
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

func fetchBaseRevisionIfMissing(ctx context.Context, git DiffGit, cache CacheConfig, m source.SourceMirror, verbose bool, progress progressReporter, trace io.Writer) error {
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

func buildDiffArgs(ctx context.Context, git DiffGit, m source.SourceMirror, userArgs []string) ([]string, error) {
	item, err := baseDiffItem(ctx, git, m)
	if err != nil {
		return nil, err
	}
	baseTree, err := git.MakeTreeWithItem(ctx, m.LocalPath, item)
	if err != nil {
		return nil, err
	}

	if item.Type == "blob" {
		args := []string{
			"--relative=" + m.LocalPath,
			"--src-prefix=a/" + path.Base(m.UpstreamPath),
			"--dst-prefix=b/" + path.Base(m.LocalPath),
			baseTree,
		}
		args = append(args, userArgs...)
		return append(args, ":(top)"+m.LocalPath), nil
	}

	args := []string{"--relative=" + m.LocalPath + "/", baseTree}
	return append(args, userArgs...), nil
}

func baseDiffItem(ctx context.Context, git DiffGit, m source.SourceMirror) (gitexec.TreeItem, error) {
	if m.UpstreamPath == "" {
		return gitexec.TreeItem{Type: "tree", Hash: m.Revision}, nil
	}
	return git.LsTreeItem(ctx, m.Revision, m.UpstreamPath)
}
