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

func (h AddHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	if err := Preflight(ctx, cli.CommandAdd, inv, h.Options, stderr); err != nil {
		return err
	}

	git := h.addGit(inv, stderr)
	return h.add(ctx, git, inv, stderr)
}

func (h AddHandler) addGit(inv cli.Invocation, trace io.Writer) AddGit {
	if git, ok := h.Options.Git.(AddGit); ok {
		return git
	}
	return gitexec.New(workDir(h.Options.WorkDir), inv.Global.Verbose, trace)
}

func (h AddHandler) add(ctx context.Context, git AddGit, inv cli.Invocation, trace io.Writer) error {
	cfg, err := config.Load(configRoot(h.Options))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}

	addOptions := inv.Add
	if addOptions.Branch == "" && addOptions.Tag == "" && addOptions.Revision == "" {
		branch, err := defaultBranch(ctx, git, addOptions.URL)
		if err != nil {
			return err
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
		return err
	}
	if err := validateNewMirror(cfg, m); err != nil {
		return err
	}
	if mirrorOverlapsConfig(m.Path) {
		return fmt.Errorf("mirror path %q overlaps %s", m.Path, config.FileName)
	}
	if err := ensureCommandScopesClean(ctx, git, configRoot(h.Options), false, m.Path); err != nil {
		return err
	}
	if err := ensureAddTargetAvailable(ctx, git, configRoot(h.Options), m.Path); err != nil {
		return err
	}

	cache, err := runtimeCache(inv.Global)
	if err != nil {
		return err
	}
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m.URL, inv.Global.Verbose, trace); err != nil {
			return err
		}
	}

	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return err
	}
	remote := m.Remote()
	cleanupRemote := func(cause error) error {
		if err := git.RemoteRemove(ctx, remote); err != nil {
			if cause != nil {
				return fmt.Errorf("%w; failed to remove temporary remote %q: %w", cause, remote, err)
			}
			return fmt.Errorf("add committed but failed to remove temporary remote %q: %w", remote, err)
		}
		return cause
	}

	if err := fetchMirror(ctx, git, m); err != nil {
		return cleanupRemote(err)
	}

	revision, err := resolveAddRevision(ctx, git, m, addOptions.Revision)
	if err != nil {
		return cleanupRemote(err)
	}
	item, err := itemAtRevision(ctx, git, m, revision)
	if err != nil {
		return cleanupRemote(err)
	}

	m.Revision = revision
	if err := cfg.Add(m); err != nil {
		return cleanupRemote(err)
	}
	configData, err := cfg.MarshalJSON()
	if err != nil {
		return cleanupRemote(err)
	}
	configItem, err := git.HashBytes(ctx, configData)
	if err != nil {
		return cleanupRemote(err)
	}

	mirrorTree, err := git.MakeTreeWithItemIn(ctx, "HEAD", m.Path, item)
	if err != nil {
		return cleanupRemote(err)
	}
	finalTree, err := git.MakeTreeWithItemIn(ctx, mirrorTree, config.FileName, configItem)
	if err != nil {
		return cleanupRemote(err)
	}
	committed, err := git.CommitTreeWithTemporaryIndex(ctx, finalTree, addCommitSubject(m))
	if err != nil {
		return cleanupRemote(err)
	}
	if !committed {
		return cleanupRemote(errors.New("add produced no commit"))
	}
	if err := git.RestorePathspecsFromHead(ctx, m.Path, config.FileName); err != nil {
		return cleanupRemote(err)
	}
	return cleanupRemote(nil)
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

func ensureAddTargetAvailable(ctx context.Context, git AddGit, root, target string) error {
	tracked, err := git.LsFiles(ctx, target)
	if err != nil {
		return err
	}
	if strings.TrimSpace(tracked) != "" {
		return fmt.Errorf("add target path %q already exists in git index", target)
	}

	for _, ancestor := range pathAncestors(target) {
		tracked, err := git.LsFiles(ctx, ancestor)
		if err != nil {
			return err
		}
		if lsFilesContainsExactPath(tracked, ancestor) {
			return fmt.Errorf("add target path %q is blocked by existing git index path %q", target, ancestor)
		}
	}

	for _, path := range append(pathAncestors(target), target) {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if path == target {
				return fmt.Errorf("add target path %q already exists in worktree", target)
			}
			return fmt.Errorf("add target path %q is blocked by worktree path %q", target, path)
		}
	}
	return nil
}

func pathAncestors(path string) []string {
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	ancestors := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		ancestors = append(ancestors, strings.Join(parts[:i], "/"))
	}
	return ancestors
}

func lsFilesContainsExactPath(output, path string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSuffix(line, "\r") == path {
			return true
		}
	}
	return false
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

type fetchGit interface {
	Fetch(context.Context, ...string) error
}

func fetchMirror(ctx context.Context, git fetchGit, m mirror.Mirror) error {
	if err := git.Fetch(ctx, "-n", m.Remote()); err != nil {
		return err
	}
	if m.Tag != "" {
		return git.Fetch(ctx, "-n", m.Remote(), "+refs/tags/"+m.Tag+":refs/tags/"+m.Tag)
	}
	return nil
}

type revParseGit interface {
	RevParse(context.Context, string) (string, error)
}

func resolveAddRevision(ctx context.Context, git revParseGit, m mirror.Mirror, requested string) (string, error) {
	if requested != "" {
		return git.RevParse(ctx, requested+"^{commit}")
	}
	return git.RevParse(ctx, m.LocalRef()+"^{commit}")
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
