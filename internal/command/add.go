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
	"braid/internal/pathcheck"
	"braid/internal/source"
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
	var s source.Source
	addingExisting := addOptions.ExistingSource != ""
	if addingExisting {
		s, err = cfg.SourceByNameRequired(addOptions.ExistingSource)
		if err != nil {
			return err
		}
	} else {
		name := addOptions.SourceName
		if name == "" {
			name = source.DerivedName(addOptions.URL)
		}
		if !source.ValidName(name) {
			return fmt.Errorf("invalid source name %q; specify --name", name)
		}
		if _, exists := cfg.SourceByName(name); exists {
			return fmt.Errorf("source name already exists: %s", name)
		}
		tracking := source.Tracking(source.RevisionTracking{})
		if addOptions.Branch != "" {
			tracking = source.BranchTracking{Branch: addOptions.Branch}
		} else if addOptions.Tag != "" {
			tracking = source.TagTracking{Tag: addOptions.Tag}
		}
		s = source.Source{Name: name, URL: source.CleanURL(addOptions.URL), Tracking: tracking, Revision: addOptions.Revision, PartialClone: addOptions.PartialClone, SyncPush: addOptions.SyncPush}
	}
	requested := addOptions.Mirrors
	if len(requested) == 0 {
		requested = []cli.MirrorMapping{{LocalPath: s.Name}}
	}
	newMirrors := make([]source.Mirror, 0, len(requested))
	existingPaths := cfg.LocalPaths()
	for _, mapping := range requested {
		localPath, normalizeErr := normalizeLocalPath(repo, mapping.LocalPath)
		if normalizeErr != nil {
			return normalizeErr
		}
		sm := source.Mirror{LocalPath: localPath, UpstreamPath: mapping.UpstreamPath}
		candidate := s.WithMirror(sm)
		if err := pathcheck.ValidateLocal(localPath, existingPaths); err != nil {
			return err
		}
		if candidate.UpstreamPath != "" {
			if err := pathcheck.ValidateUpstream(candidate.UpstreamPath); err != nil {
				return err
			}
		}
		if err := validateNewMirrorPath(cfg, candidate); err != nil {
			return err
		}
		if mirrorOverlapsConfig(localPath) {
			return fmt.Errorf("mirror path %q overlaps %s", localPath, config.FileName)
		}
		if err := ensureAddTargetAvailable(ctx, git, configRoot(h.Options, repo), localPath); err != nil {
			return err
		}
		newMirrors = append(newMirrors, sm)
		existingPaths = append(existingPaths, localPath)
		s.Mirrors = append(s.Mirrors, sm)
	}
	paths := make([]string, 0, len(newMirrors))
	for _, mirror := range newMirrors {
		paths = append(paths, mirror.LocalPath)
	}
	if err := ensureCommandScopesClean(ctx, git, configRoot(h.Options, repo), false, paths...); err != nil {
		return err
	}
	if !addingExisting && addOptions.Branch == "" && addOptions.Tag == "" && addOptions.Revision == "" {
		branch, branchErr := defaultBranch(ctx, git, s.URL, ":"+s.Name, progress)
		if branchErr != nil {
			return branchErr
		}
		s.Tracking = source.BranchTracking{Branch: branch}
	}
	primary := s.WithMirror(newMirrors[0])
	if !addingExisting {
		if err := validateNewMirrorRemote(cfg, primary); err != nil {
			return err
		}
	}

	cache, err := runtimeCacheForRepo(ctx, repo, inv.Global, inv.Global.Verbose, trace)
	if err != nil {
		return err
	}
	if cache.Enabled {
		if err := fetchCache(ctx, cache, primary, inv.Global.Verbose, progress, trace); err != nil {
			return err
		}
	}

	if err := configureMirrorRemote(ctx, git, primary, true, cache); err != nil {
		return err
	}
	remote := primary.Remote()
	cleanupRemote := func(cause error, completed string) error {
		if err := git.RemoteRemove(ctx, remote); err != nil {
			if cause != nil {
				return fmt.Errorf("%w; failed to remove temporary remote %q: %w", cause, remote, err)
			}
			return fmt.Errorf("add %s but failed to remove temporary remote %q: %w", completed, remote, err)
		}
		return cause
	}

	if err := fetchMirror(ctx, git, cache, primary, progress); err != nil {
		return cleanupRemote(err, "")
	}

	var revision string
	if addingExisting {
		revision, err = git.RevParse(ctx, s.Revision+"^{commit}")
	} else {
		revision, err = resolveAddRevision(ctx, git, primary, cacheResolveRecordedRevision(cache, primary, addOptions.Revision))
	}
	if err != nil {
		return cleanupRemote(err, "")
	}
	s.Revision = revision
	if addingExisting {
		err = cfg.UpdateSource(s)
	} else {
		err = cfg.AddSource(s)
	}
	if err != nil {
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

	mirrorTree := "HEAD"
	for _, mirror := range newMirrors {
		item, itemErr := itemAtRevision(ctx, git, s.WithMirror(mirror), revision)
		if itemErr != nil {
			return cleanupRemote(itemErr, "")
		}
		mirrorTree, err = git.MakeTreeWithItemIn(ctx, mirrorTree, mirror.LocalPath, item)
		if err != nil {
			return cleanupRemote(err, "")
		}
	}
	finalTree, err := git.MakeTreeWithItemIn(ctx, mirrorTree, config.FileName, configItem)
	if err != nil {
		return cleanupRemote(err, "")
	}
	if addOptions.NoCommit {
		var warned bool
		description := ""
		if addingExisting {
			description = "addition of mirrors to source ':" + s.Name + "'"
		}
		if err := stageNoCommitResult(ctx, git, stdout, noCommitStageOptions{
			Tree:        finalTree,
			Action:      "add",
			MirrorPath:  ":" + s.Name,
			Description: description,
			Paths:       append(append([]string{}, paths...), config.FileName),
			OwnedPaths:  paths,
			Quiet:       inv.Global.Quiet,
			Warned:      &warned,
		}); err != nil {
			return cleanupRemote(err, "")
		}
		return cleanupRemote(nil, "staged changes")
	}
	subject := fmt.Sprintf("Braid: Add source '%s' at '%s'", s.Name, shortRevision(s.Revision))
	if addingExisting {
		subject = fmt.Sprintf("Braid: Add mirrors to source '%s'", s.Name)
	}
	committed, err := git.CommitTreeWithTemporaryIndex(ctx, finalTree, subject)
	if err != nil {
		return cleanupRemote(err, "")
	}
	if !committed {
		return cleanupRemote(errors.New("add produced no commit"), "")
	}
	if err := git.RestorePathspecsFromHead(ctx, append(paths, config.FileName)...); err != nil {
		return cleanupRemote(err, "")
	}
	return cleanupRemote(nil, "committed")
}

func defaultBranch(ctx context.Context, git AddGit, url, localPath string, progress progressReporter) (branch string, err error) {
	op, err := progress.Start(fmt.Sprintf("Braid: detecting default branch for source %s", localPath))
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
	if err := op.Complete(fmt.Sprintf("Braid: detected default branch for source %s", localPath)); err != nil {
		return "", err
	}
	return targets[0], nil
}

func validateNewMirrorPath(cfg config.Config, candidate source.SourceMirror) error {
	if err := pathcheck.ValidateLocal(candidate.LocalPath, cfg.LocalPaths()); err != nil {
		return err
	}
	if candidate.UpstreamPath != "" {
		if err := pathcheck.ValidateUpstream(candidate.UpstreamPath); err != nil {
			return err
		}
	}
	return nil
}

func validateNewMirrorRemote(cfg config.Config, candidate source.SourceMirror) error {
	return pathcheck.CheckRemoteCollision(candidate.Source, cfg.SourcesSorted())
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

func resolveAddRevision(ctx context.Context, git revParseGit, m source.SourceMirror, requested string) (string, error) {
	if requested != "" {
		return git.RevParse(ctx, requested+"^{commit}")
	}
	return git.RevParse(ctx, m.LocalRef()+"^{commit}")
}

func shortRevision(revision string) string {
	if len(revision) < 7 {
		return revision
	}
	return revision[:7]
}
