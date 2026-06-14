package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
)

type PushHandler struct {
	Options Options
}

func (h PushHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	if err := Preflight(ctx, cli.CommandPush, inv, h.Options, stderr); err != nil {
		return err
	}

	git := h.pushGit(inv, stderr)
	cfg, err := config.Load(configRoot(h.Options))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	m, err := cfg.GetRequired(inv.Push.LocalPath)
	if err != nil {
		return err
	}
	return h.push(ctx, git, m, inv, stdout, stderr)
}

func (h PushHandler) pushGit(inv cli.Invocation, trace io.Writer) PushGit {
	if git, ok := h.Options.Git.(PushGit); ok {
		return git
	}
	return gitexec.New(workDir(h.Options.WorkDir), verbose(inv), trace)
}

func (h PushHandler) push(ctx context.Context, git PushGit, m mirror.Mirror, inv cli.Invocation, stdout, trace io.Writer) (err error) {
	branch := inv.Push.Branch
	if branch == "" {
		branch = m.Branch
	}
	if branch == "" {
		return fmt.Errorf("mirror has no tracked branch; specify --branch to push %s", m.Path)
	}

	cache, err := runtimeCache(inv.Global)
	if err != nil {
		return err
	}
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m.URL, inv.Push.Verbose, trace); err != nil {
			return err
		}
	}
	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return err
	}
	if !inv.Push.Keep {
		defer func() {
			removeErr := git.RemoteRemove(ctx, m.Remote())
			if err == nil {
				err = removeErr
			}
		}()
	}
	if err := fetchMirror(ctx, git, m); err != nil {
		return err
	}

	upstreamRevision, err := resolveAddRevision(ctx, git, m, "")
	if err != nil {
		return err
	}
	baseRevision, err := git.RevParse(ctx, m.Revision+"^{commit}")
	if err != nil {
		return err
	}
	if upstreamRevision != baseRevision {
		if _, err := fmt.Fprintln(stdout, "Braid: Mirror is not up to date. Stopping."); err != nil {
			return err
		}
		return nil
	}

	diffArgs, err := buildDiffArgs(ctx, git, m, nil)
	if err != nil {
		return err
	}
	diff, err := git.Diff(ctx, diffArgs...)
	if err != nil {
		return err
	}
	if strings.TrimSpace(diff) == "" {
		if _, err := fmt.Fprintln(stdout, "Braid: No local changes found. Stopping."); err != nil {
			return err
		}
		return nil
	}

	localItem, err := git.LsTreeItem(ctx, "HEAD", m.Path)
	if err != nil {
		return err
	}
	return h.pushViaTempRepo(ctx, git, m, branch, baseRevision, localItem, inv.Push.Verbose, trace)
}

func (h PushHandler) pushViaTempRepo(ctx context.Context, source PushGit, m mirror.Mirror, branch, baseRevision string, localItem gitexec.TreeItem, verbose bool, trace io.Writer) error {
	tempDir, err := os.MkdirTemp("", "braid-push")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	tempGit := gitexec.New(tempDir, verbose, trace)
	if err := tempGit.Init(ctx); err != nil {
		return err
	}
	// Push is assembled in an isolated repository so the user's worktree and index stay untouched.
	if err := copyLocalGitConfig(ctx, source, tempGit); err != nil {
		return err
	}
	// Alternates let the temporary repository reuse already-fetched objects without copying packs.
	if err := writeAlternates(ctx, source, tempDir, workDir(h.Options.WorkDir)); err != nil {
		return err
	}
	if err := tempGit.UpdateRef(ctx, "--no-deref", "HEAD", baseRevision); err != nil {
		return err
	}
	newTree, err := tempGit.MakeTreeWithItemIn(ctx, baseRevision, m.RemotePath, localItem)
	if err != nil {
		return err
	}
	if err := tempGit.ConfigSet(ctx, "--local", "core.sparsecheckout", "true"); err != nil {
		return err
	}
	// An empty sparse-checkout avoids checking out files outside the mirror and running their filters.
	if err := os.WriteFile(filepath.Join(tempDir, ".git", "info", "sparse-checkout"), nil, 0o644); err != nil {
		return err
	}
	if err := tempGit.ReadTreeUpdateMerge(ctx, newTree); err != nil {
		return err
	}
	if err := tempGit.CommitVerbose(ctx); err != nil {
		return err
	}
	return tempGit.Push(ctx, m.URL, "HEAD:refs/heads/"+branch)
}

func copyLocalGitConfig(ctx context.Context, source PushGit, target gitexec.Git) error {
	for _, key := range []string{"user.name", "user.email", "commit.gpgsign"} {
		value, ok, err := source.ConfigGet(ctx, "--local", "--get", key)
		if err != nil {
			return err
		}
		if ok {
			if err := target.ConfigSet(ctx, "--local", key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeAlternates(ctx context.Context, source PushGit, tempDir, sourceWorkDir string) error {
	objectsPath, err := source.RepoFilePath(ctx, "objects")
	if err != nil {
		return err
	}
	objectsPath, err = alternateObjectPath(objectsPath, sourceWorkDir)
	if err != nil {
		return err
	}
	alternates := filepath.Join(tempDir, ".git", "objects", "info", "alternates")
	return os.WriteFile(alternates, []byte(objectsPath+"\n"), 0o644)
}

func alternateObjectPath(objectsPath, sourceWorkDir string) (string, error) {
	absolutePath, err := gitRepoOSPath(objectsPath, sourceWorkDir)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(absolutePath), nil
}
