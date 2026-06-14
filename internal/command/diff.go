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
	if err := Preflight(ctx, cli.CommandDiff, inv, h.Options, stderr); err != nil {
		return err
	}

	git := h.diffGit(inv, stderr)
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

	if inv.Diff.LocalPath != "" {
		m, err := cfg.GetRequired(inv.Diff.LocalPath)
		if err != nil {
			return err
		}
		return h.diffOne(ctx, git, cache, m, inv.Diff, stdout, stderr)
	}

	for _, localPath := range cfg.Paths() {
		fmt.Fprintf(stdout, "=======================================================\nBraid: Diffing %s\n=======================================================\n", localPath)
		if err := h.diffOne(ctx, git, cache, cfg.Mirrors[localPath], inv.Diff, stdout, stderr); err != nil {
			return err
		}
	}
	return nil
}

func (h DiffHandler) diffGit(inv cli.Invocation, trace io.Writer) DiffGit {
	if git, ok := h.Options.Git.(DiffGit); ok {
		return git
	}
	return gitexec.New(workDir(h.Options.WorkDir), verbose(inv), trace)
}

func (h DiffHandler) diffOne(ctx context.Context, git DiffGit, cache CacheConfig, m mirror.Mirror, options cli.DiffOptions, stdout, trace io.Writer) (err error) {
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

	if err := fetchBaseRevisionIfMissing(ctx, git, cache, m, options.Verbose, trace); err != nil {
		return err
	}
	args, err := buildDiffArgs(ctx, git, m, options.GitDiffArgs)
	if err != nil {
		return err
	}
	out, err := git.Diff(ctx, args...)
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, out)
	return err
}

func fetchBaseRevisionIfMissing(ctx context.Context, git DiffGit, cache CacheConfig, m mirror.Mirror, verbose bool, trace io.Writer) error {
	if _, err := git.RevParse(ctx, m.Revision+"^{commit}"); err == nil {
		return nil
	}
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m.URL, verbose, trace); err != nil {
			return err
		}
	}
	return fetchMirror(ctx, git, m)
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
		return append(args, m.Path), nil
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
