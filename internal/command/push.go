package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
)

type PushHandler struct {
	Options Options
}

type pushStatus int

const (
	pushStatusPushed pushStatus = iota
	pushStatusNoLocalChanges
	pushStatusNotUpToDate
)

type pushResult struct {
	Status pushStatus
}

func (h PushHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandPush, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.pushGit(repo, inv, stderr)
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	localPath, err := normalizeLocalPath(repo, inv.Push.LocalPath)
	if err != nil {
		return err
	}
	m, err := cfg.GetRequired(localPath)
	if err != nil {
		return err
	}
	result, err := h.push(ctx, repo, git, m, inv.Push.Branch, inv.Push.Keep, inv.Global, stdout, stderr)
	if err != nil {
		return err
	}
	switch result.Status {
	case pushStatusNotUpToDate:
		_, err = fmt.Fprintln(stdout, "Braid: Mirror is not up to date. Stopping.")
	case pushStatusNoLocalChanges:
		_, err = fmt.Fprintln(stdout, "Braid: No local changes found in downstream HEAD. Stopping.")
	}
	return err
}

func (h PushHandler) pushGit(repo RepoContext, inv cli.Invocation, trace io.Writer) PushGit {
	if git, ok := h.Options.Git.(PushGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(PushGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (h PushHandler) push(ctx context.Context, repo RepoContext, git PushGit, m mirror.Mirror, branch string, keep bool, global cli.GlobalOptions, stdout, stderr io.Writer) (result pushResult, err error) {
	if branch == "" {
		branch = m.Branch
	}
	if branch == "" {
		return pushResult{}, fmt.Errorf("mirror has no tracked branch; specify --branch to push %s", m.Path)
	}

	cache, err := runtimeCache(global)
	if err != nil {
		return pushResult{}, err
	}
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m.URL, global.Verbose, stderr); err != nil {
			return pushResult{}, err
		}
	}
	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return pushResult{}, err
	}
	if !keep {
		defer func() {
			removeErr := git.RemoteRemove(ctx, m.Remote())
			if err == nil {
				err = removeErr
			}
		}()
	}
	if err := fetchMirror(ctx, git, m); err != nil {
		return pushResult{}, err
	}

	upstreamRevision, err := resolveAddRevision(ctx, git, m, "")
	if err != nil {
		return pushResult{}, err
	}
	baseRevision, err := git.RevParse(ctx, m.Revision+"^{commit}")
	if err != nil {
		return pushResult{}, err
	}
	if upstreamRevision != baseRevision {
		return pushResult{Status: pushStatusNotUpToDate}, nil
	}

	localItem, err := git.LsTreeItem(ctx, "HEAD", m.Path)
	if err != nil {
		return pushResult{}, err
	}
	newTree, err := git.MakeTreeWithItemIn(ctx, baseRevision, m.RemotePath, localItem)
	if err != nil {
		return pushResult{}, err
	}
	baseTree, err := git.RevParse(ctx, baseRevision+"^{tree}")
	if err != nil {
		return pushResult{}, err
	}
	if newTree == baseTree {
		return pushResult{Status: pushStatusNoLocalChanges}, nil
	}

	if err := h.pushViaTempRepo(ctx, repo, git, m, branch, baseRevision, newTree, global.Verbose, h.stdin(), stdout, stderr); err != nil {
		return pushResult{}, err
	}
	return pushResult{Status: pushStatusPushed}, nil
}

func (h PushHandler) pushViaTempRepo(ctx context.Context, repo RepoContext, source PushGit, m mirror.Mirror, branch, baseRevision, newTree string, verbose bool, stdin io.Reader, stdout, stderr io.Writer) error {
	tempDir, err := os.MkdirTemp("", "braid-push")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	tempGit := gitexec.New(tempDir, verbose, stderr)
	if err := tempGit.Init(ctx); err != nil {
		return err
	}
	// Push is assembled in an isolated repository so the user's worktree and index stay untouched.
	if err := copyLocalGitConfig(ctx, source, tempGit); err != nil {
		return err
	}
	// Alternates let the temporary repository reuse already-fetched objects without copying packs.
	if err := writeAlternates(ctx, source, tempDir, repo.GitWorkTreeRoot); err != nil {
		return err
	}
	if err := tempGit.UpdateRef(ctx, "--no-deref", "HEAD", baseRevision); err != nil {
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
	if err := tempGit.CommitVerbose(ctx, stdin, stdout, stderr); err != nil {
		return err
	}
	return tempGit.Push(ctx, m.URL, "HEAD:refs/heads/"+branch)
}

func (h PushHandler) stdin() io.Reader {
	if h.Options.Stdin != nil {
		return h.Options.Stdin
	}
	return os.Stdin
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
