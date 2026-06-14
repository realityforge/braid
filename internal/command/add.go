package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
	"braid/internal/pathcheck"
)

type AddHandler struct {
	Options Options
}

type indexItem struct {
	treeish string
	blob    *gitexec.TreeItem
}

func (h AddHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	if err := Preflight(ctx, cli.CommandAdd, inv, h.Options, stderr); err != nil {
		return err
	}

	git := h.addGit(inv, stderr)
	head, err := git.Head(ctx)
	if err != nil {
		return err
	}

	remoteAdded, err := h.add(ctx, git, inv, stderr)
	if err != nil {
		if remoteAdded != "" {
			_ = git.RemoteRemove(ctx, remoteAdded)
		}
		return resetOnError(ctx, git, head, err)
	}
	if remoteAdded != "" {
		return git.RemoteRemove(ctx, remoteAdded)
	}
	return nil
}

func (h AddHandler) addGit(inv cli.Invocation, trace io.Writer) AddGit {
	if git, ok := h.Options.Git.(AddGit); ok {
		return git
	}
	return gitexec.New(workDir(h.Options.WorkDir), verbose(inv), trace)
}

func (h AddHandler) add(ctx context.Context, git AddGit, inv cli.Invocation, trace io.Writer) (string, error) {
	cfg, err := config.Load(configRoot(h.Options))
	if err != nil {
		return "", err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return "", err
	}

	addOptions := inv.Add
	if addOptions.Branch == "" && addOptions.Tag == "" && addOptions.Revision == "" {
		branch, err := defaultBranch(ctx, git, addOptions.URL)
		if err != nil {
			return "", err
		}
		addOptions.Branch = branch
	}
	if addOptions.Revision != "" {
		addOptions.Branch = ""
	}

	m, err := mirror.NewFromOptions(addOptions.URL, mirror.Options{
		LocalPath:  addOptions.LocalPath,
		Branch:     addOptions.Branch,
		Tag:        addOptions.Tag,
		Revision:   addOptions.Revision,
		RemotePath: addOptions.RemotePath,
	})
	if err != nil {
		return "", err
	}
	if err := validateNewMirror(cfg, m); err != nil {
		return "", err
	}

	cache, err := runtimeCache(inv.Global)
	if err != nil {
		return "", err
	}
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m.URL, verbose(inv), trace); err != nil {
			return "", err
		}
	}

	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return "", err
	}
	remote := m.Remote()
	if err := fetchMirror(ctx, git, m); err != nil {
		return remote, err
	}

	revision, err := resolveAddRevision(ctx, git, m, addOptions.Revision)
	if err != nil {
		return remote, err
	}
	item, err := upstreamIndexItem(ctx, git, m, revision)
	if err != nil {
		return remote, err
	}
	if err := addItemToIndex(ctx, git, item, m.Path); err != nil {
		return remote, err
	}

	m.Revision = revision
	if err := cfg.Add(m); err != nil {
		return remote, err
	}
	if err := cfg.WriteFile(filepath.Join(configRoot(h.Options), config.FileName)); err != nil {
		return remote, err
	}
	if err := git.Add(ctx, config.FileName); err != nil {
		return remote, err
	}
	committed, err := git.CommitMessage(ctx, addCommitSubject(m))
	if err != nil {
		return remote, err
	}
	if !committed {
		return remote, errors.New("add produced no commit")
	}
	return remote, nil
}

func resetOnError(ctx context.Context, git AddGit, head string, cause error) error {
	if resetErr := git.ResetHard(ctx, head); resetErr != nil {
		return fmt.Errorf("%w; failed to reset to %s: %w", cause, shortRevision(head), resetErr)
	}
	return cause
}

func defaultBranch(ctx context.Context, git AddGit, url string) (string, error) {
	out, err := git.LsRemote(ctx, "--symref", url, "HEAD")
	if err != nil {
		return "", err
	}
	var targets []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "ref: ") || !strings.HasSuffix(line, "\tHEAD") {
			continue
		}
		target := strings.TrimSuffix(strings.TrimPrefix(line, "ref: "), "\tHEAD")
		if strings.HasPrefix(target, "refs/heads/") {
			targets = append(targets, strings.TrimPrefix(target, "refs/heads/"))
		}
	}
	if len(targets) != 1 || targets[0] == "" {
		return "", errors.New("failed to detect default branch; specify --branch")
	}
	return targets[0], nil
}

func validateNewMirror(cfg config.Config, candidate mirror.Mirror) error {
	if err := pathcheck.ValidateLocal(candidate.Path, cfg.Paths()); err != nil {
		return err
	}
	if candidate.RemotePath != "" {
		if err := pathcheck.ValidateUpstream(candidate.RemotePath); err != nil {
			return err
		}
	}
	existing := make([]mirror.Mirror, 0, len(cfg.Mirrors))
	for _, localPath := range cfg.Paths() {
		existing = append(existing, cfg.Mirrors[localPath])
	}
	return pathcheck.CheckRemoteCollision(candidate, existing)
}

func fetchCache(ctx context.Context, cache CacheConfig, url string, verbose bool, trace io.Writer) error {
	cachePath := CachePath(cache.Dir, url)
	if _, err := os.Stat(filepath.Join(cachePath, ".git")); err == nil {
		if err := os.RemoveAll(cachePath); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if _, err := os.Stat(cachePath); err == nil {
		return gitexec.New(cachePath, verbose, trace).Fetch(ctx)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(cache.Dir, 0o755); err != nil {
		return err
	}
	return gitexec.New(".", verbose, trace).CloneMirror(ctx, url, cachePath)
}

func fetchMirror(ctx context.Context, git AddGit, m mirror.Mirror) error {
	if err := git.Fetch(ctx, "-n", m.Remote()); err != nil {
		return err
	}
	if m.Tag != "" {
		return git.Fetch(ctx, "-n", m.Remote(), "+refs/tags/"+m.Tag+":refs/tags/"+m.Tag)
	}
	return nil
}

func resolveAddRevision(ctx context.Context, git AddGit, m mirror.Mirror, requested string) (string, error) {
	if requested != "" {
		return git.RevParse(ctx, requested+"^{commit}")
	}
	return git.RevParse(ctx, m.LocalRef()+"^{commit}")
}

func upstreamIndexItem(ctx context.Context, git AddGit, m mirror.Mirror, revision string) (indexItem, error) {
	if m.RemotePath == "" {
		return indexItem{treeish: revision}, nil
	}
	item, err := git.LsTreeItem(ctx, revision, m.RemotePath)
	if err != nil {
		return indexItem{}, err
	}
	if item.Type == "tree" {
		return indexItem{treeish: item.Hash}, nil
	}
	return indexItem{blob: &item}, nil
}

func addItemToIndex(ctx context.Context, git AddGit, item indexItem, path string) error {
	if item.blob != nil {
		if err := git.UpdateIndexCacheInfo(ctx, item.blob.Mode, item.blob.Hash, path); err != nil {
			return err
		}
		return git.CheckoutIndex(ctx, path)
	}
	return git.ReadTreePrefix(ctx, path, item.treeish, true)
}

func addCommitSubject(m mirror.Mirror) string {
	return fmt.Sprintf("Braid: Add mirror '%s' at '%s'", m.Path, shortRevision(m.Revision))
}

func shortRevision(revision string) string {
	if len(revision) < 7 {
		return revision
	}
	return revision[:7]
}
