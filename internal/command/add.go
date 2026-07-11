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
	repo, err := Preflight(ctx, cli.CommandAdd, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.addGit(repo, inv, stderr)
	progress := newProgressReporter(stderr, inv.Global.Quiet)
	return h.add(ctx, repo, git, inv, progress, stdout, stderr)
}

func (h AddHandler) addGit(repo RepoContext, inv cli.Invocation, trace io.Writer) AddGit {
	if git, ok := h.Options.Git.(AddGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(AddGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (h AddHandler) add(ctx context.Context, repo RepoContext, git AddGit, inv cli.Invocation, progress progressReporter, stdout, trace io.Writer) error {
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}

	addOptions := inv.Add
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
	m.Path, err = normalizeLocalPath(repo, m.Path)
	if err != nil {
		return err
	}
	if err := validateNewMirrorPath(cfg, m); err != nil {
		return err
	}
	if mirrorOverlapsConfig(m.Path) {
		return fmt.Errorf("mirror path %q overlaps %s", m.Path, config.FileName)
	}
	if err := ensureCommandScopesClean(ctx, git, configRoot(h.Options, repo), false, m.Path); err != nil {
		return err
	}
	if err := ensureAddTargetAvailable(ctx, git, configRoot(h.Options, repo), m.Path); err != nil {
		return err
	}

	if addOptions.Branch == "" && addOptions.Tag == "" && addOptions.Revision == "" {
		branch, err := defaultBranch(ctx, git, addOptions.URL, m.Path, progress)
		if err != nil {
			return err
		}
		addOptions.Branch = branch
		m.Branch = branch
	}
	if err := validateNewMirrorRemote(cfg, m); err != nil {
		return err
	}

	cache, err := runtimeCacheForRepo(ctx, repo, inv.Global, inv.Global.Verbose, trace)
	if err != nil {
		return err
	}
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m, inv.Global.Verbose, progress, trace); err != nil {
			return err
		}
	}

	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return err
	}
	remote := m.Remote()
	cleanupRemote := func(cause error, completed string) error {
		if err := git.RemoteRemove(ctx, remote); err != nil {
			if cause != nil {
				return fmt.Errorf("%w; failed to remove temporary remote %q: %w", cause, remote, err)
			}
			return fmt.Errorf("add %s but failed to remove temporary remote %q: %w", completed, remote, err)
		}
		return cause
	}

	if err := fetchMirror(ctx, git, cache, m, progress); err != nil {
		return cleanupRemote(err, "")
	}

	revision, err := resolveAddRevision(ctx, git, m, cacheResolveRecordedRevision(cache, m, addOptions.Revision))
	if err != nil {
		return cleanupRemote(err, "")
	}
	item, err := itemAtRevision(ctx, git, m, revision)
	if err != nil {
		return cleanupRemote(err, "")
	}

	m.Revision = revision
	if err := cfg.Add(m); err != nil {
		return cleanupRemote(err, "")
	}
	configData, err := cfg.MarshalJSON()
	if err != nil {
		return cleanupRemote(err, "")
	}
	configItem, err := git.HashBytes(ctx, configData)
	if err != nil {
		return cleanupRemote(err, "")
	}

	mirrorTree, err := git.MakeTreeWithItemIn(ctx, "HEAD", m.Path, item)
	if err != nil {
		return cleanupRemote(err, "")
	}
	finalTree, err := git.MakeTreeWithItemIn(ctx, mirrorTree, config.FileName, configItem)
	if err != nil {
		return cleanupRemote(err, "")
	}
	if addOptions.NoCommit {
		var warned bool
		if err := stageNoCommitResult(ctx, git, stdout, noCommitStageOptions{
			Tree:       finalTree,
			Action:     "add",
			MirrorPath: m.Path,
			Paths:      []string{m.Path, config.FileName},
			OwnedPaths: []string{m.Path},
			Quiet:      inv.Global.Quiet,
			Warned:     &warned,
		}); err != nil {
			return cleanupRemote(err, "")
		}
		return cleanupRemote(nil, "staged changes")
	}
	committed, err := git.CommitTreeWithTemporaryIndex(ctx, finalTree, addCommitSubject(m))
	if err != nil {
		return cleanupRemote(err, "")
	}
	if !committed {
		return cleanupRemote(errors.New("add produced no commit"), "")
	}
	if err := git.RestorePathspecsFromHead(ctx, m.Path, config.FileName); err != nil {
		return cleanupRemote(err, "")
	}
	return cleanupRemote(nil, "committed")
}

func defaultBranch(ctx context.Context, git AddGit, url, localPath string, progress progressReporter) (branch string, err error) {
	op, err := progress.Start(fmt.Sprintf("Braid: detecting default branch for mirror %s", localPath))
	if err != nil {
		return "", err
	}
	out, err := git.LsRemote(ctx, "--symref", url, "HEAD")
	if err != nil {
		_ = op.Abort()
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
		_ = op.Abort()
		return "", errors.New("failed to detect default branch; specify --branch")
	}
	if err := op.Complete(fmt.Sprintf("Braid: detected default branch for mirror %s", localPath)); err != nil {
		return "", err
	}
	return targets[0], nil
}

func validateNewMirrorPath(cfg config.Config, candidate mirror.Mirror) error {
	if err := pathcheck.ValidateLocal(candidate.Path, cfg.Paths()); err != nil {
		return err
	}
	if candidate.RemotePath != "" {
		if err := pathcheck.ValidateUpstream(candidate.RemotePath); err != nil {
			return err
		}
	}
	return nil
}

func validateNewMirrorRemote(cfg config.Config, candidate mirror.Mirror) error {
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
