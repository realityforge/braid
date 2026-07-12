package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/source"
)

type UpdateHandler struct {
	Options Options
}

type updateStatus int

const (
	updateStatusNoop updateStatus = iota
	updateStatusUpdated
	updateStatusConflict
)

type updateResult struct {
	Status updateStatus
}

type scopedCleanGit interface {
	StatusPorcelainPathspecs(context.Context, ...string) (string, error)
	BlockingOperation(context.Context) (string, bool, error)
}

type repoPathGit interface {
	RepoFilePath(context.Context, string) (string, error)
}

func (h UpdateHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandPull, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.updateGit(repo, inv, stderr)
	processGit := h.processRepoPathGit(repo, inv, stderr)
	progress := newProgressReporter(stderr, inv.Global.Quiet)
	cache, err := runtimeCacheForRepo(ctx, repo, inv.Global, inv.Global.Verbose, stderr)
	if err != nil {
		return err
	}

	if inv.Update.LocalPath != "" {
		cfg, loadErr := config.Load(configRoot(h.Options, repo))
		if loadErr != nil {
			return loadErr
		}
		selection, resolveErr := resolveSourceSelection(repo, cfg, inv.Update.LocalPath, false)
		if resolveErr != nil {
			return resolveErr
		}
		_, err = h.updateOne(ctx, repo, git, processGit, cache, selection.Mirrors[0].LocalPath, inv.Update, inv.Global.Verbose, inv.Global.Quiet, progress, stdout, stderr)
		return err
	}
	return h.updateAll(ctx, repo, git, processGit, cache, inv.Update, inv.Global.Verbose, inv.Global.Quiet, progress, stdout, stderr)
}

func (h UpdateHandler) updateGit(repo RepoContext, inv cli.Invocation, trace io.Writer) UpdateGit {
	if git, ok := h.Options.Git.(UpdateGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(UpdateGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (h UpdateHandler) processRepoPathGit(repo RepoContext, inv cli.Invocation, trace io.Writer) repoPathGit {
	if git, ok := h.Options.Git.(repoPathGit); ok {
		return git
	}
	if git, ok := repo.processGit(inv, h.Options, trace).(repoPathGit); ok {
		return git
	}
	return gitexec.New(repo.ProcessWorkDir, inv.Global.Verbose, trace)
}

func (h UpdateHandler) updateAll(ctx context.Context, repo RepoContext, git UpdateGit, processGit repoPathGit, cache CacheConfig, options cli.UpdateOptions, verbose, quiet bool, progress progressReporter, stdout, trace io.Writer) error {
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}

	var targets []string
	var skippedLocked []string
	for _, s := range cfg.SourcesSorted() {
		if s.Locked() {
			skippedLocked = append(skippedLocked, ":"+s.Name)
			continue
		}
		targets = append(targets, s.SortedMirrors()[0].LocalPath)
	}
	if err := h.ensureUpdateTargetsClean(ctx, repo, git, cfg, targets); err != nil {
		return err
	}

	var warned bool
	for _, localPath := range targets {
		runOptions := updateOneRunOptions{}
		if options.NoCommit {
			runOptions.SkipCleanCheck = true
			runOptions.WarningPaths = targets
			runOptions.Warned = &warned
		}
		result, err := h.updateOneWithRunOptions(ctx, repo, git, processGit, cache, localPath, options, verbose, quiet, progress, stdout, trace, runOptions)
		if err != nil {
			return fmt.Errorf("pull %s: %w", localPath, err)
		}
		if options.NoCommit && result.Status == updateStatusConflict {
			break
		}
	}
	return writeSkippedLockedMirrors(stdout, skippedLocked)
}

func writeSkippedLockedMirrors(stdout io.Writer, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if _, err := io.WriteString(stdout, "Braid: skipped revision-locked sources:\n"); err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := fmt.Fprintf(stdout, "  %s\n", path); err != nil {
			return err
		}
	}
	return nil
}

type updateOneRunOptions struct {
	SkipCleanCheck bool
	WarningPaths   []string
	Warned         *bool
}

func (h UpdateHandler) updateOne(ctx context.Context, repo RepoContext, git UpdateGit, processGit repoPathGit, cache CacheConfig, localPath string, options cli.UpdateOptions, verbose, quiet bool, progress progressReporter, stdout, trace io.Writer) (updateResult, error) {
	return h.updateOneWithRunOptions(ctx, repo, git, processGit, cache, localPath, options, verbose, quiet, progress, stdout, trace, updateOneRunOptions{})
}

func (h UpdateHandler) updateOneWithRunOptions(ctx context.Context, repo RepoContext, git UpdateGit, processGit repoPathGit, cache CacheConfig, localPath string, options cli.UpdateOptions, verbose, quiet bool, progress progressReporter, stdout, trace io.Writer, runOptions updateOneRunOptions) (updateResult, error) {
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return updateResult{}, err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return updateResult{}, err
	}
	s, mirror, err := cfg.MirrorByLocalPathRequired(localPath)
	if err != nil {
		return updateResult{}, err
	}
	m := s.WithMirror(mirror)
	original := m
	applyUpdateStrategy(&m, options)
	for _, candidate := range m.Mirrors {
		if mirrorOverlapsConfig(candidate.LocalPath) {
			return updateResult{}, fmt.Errorf("mirror path %q overlaps %s", candidate.LocalPath, config.FileName)
		}
	}
	if !runOptions.SkipCleanCheck {
		if err := h.ensureUpdateTargetsClean(ctx, repo, git, cfg, m.LocalPaths()); err != nil {
			return updateResult{}, err
		}
	}

	if cache.Enabled {
		var requestedRevisions []string
		if options.Revision != "" {
			requestedRevisions = append(requestedRevisions, options.Revision)
		}
		if err := fetchCache(ctx, cache, m, verbose, progress, trace, requestedRevisions...); err != nil {
			return updateResult{}, err
		}
	}
	previousRemoteURL, previousRemoteExists, err := git.RemoteURL(ctx, m.Remote())
	if err != nil {
		return updateResult{}, err
	}
	var previousRemoteConfig gitexec.RemoteConfigSnapshot
	if previousRemoteExists {
		if exact, ok := git.(exactRemoteConfigGit); ok {
			previousRemoteConfig, err = exact.SnapshotRemoteConfig(ctx, m.Remote())
			if err != nil {
				return updateResult{}, err
			}
		}
	}
	if err := configureMirrorRemote(ctx, git, m, true, cache); err != nil {
		return updateResult{}, err
	}
	operationApplied := false
	cleanupRemote := func() error {
		if operationApplied && options.Keep {
			return nil
		}
		if _, ok, inspectErr := git.RemoteURL(ctx, m.Remote()); inspectErr != nil {
			return inspectErr
		} else if ok {
			if removeErr := git.RemoteRemove(ctx, m.Remote()); removeErr != nil {
				return removeErr
			}
		}
		if !operationApplied && previousRemoteExists {
			if exact, ok := git.(exactRemoteConfigGit); ok && previousRemoteConfig != nil {
				return exact.RestoreRemoteConfig(ctx, m.Remote(), previousRemoteConfig)
			}
			return git.RemoteAdd(ctx, m.Remote(), previousRemoteURL)
		}
		return nil
	}

	if err := fetchMirror(ctx, git, cache, m, progress); err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}

	checkProgress, err := progress.Start(fmt.Sprintf("Braid: checking source :%s", m.Name))
	if err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}
	baseRevision, err := git.RevParse(ctx, original.Revision+"^{commit}")
	if err != nil {
		_ = checkProgress.Abort()
		_ = cleanupRemote()
		return updateResult{}, err
	}
	newRevision, err := resolveUpdateRevision(ctx, git, cache, m, options.Revision)
	if err != nil {
		_ = checkProgress.Abort()
		_ = cleanupRemote()
		return updateResult{}, err
	}
	if !updateSwitchesTracking(original, m, options, newRevision) && newRevision == baseRevision {
		if err := checkProgress.Complete(fmt.Sprintf("Braid: checked source :%s", m.Name)); err != nil {
			_ = cleanupRemote()
			return updateResult{}, err
		}
		if _, err := fmt.Fprintf(stdout, "Braid: source :%s is already up to date\n", m.Name); err != nil {
			_ = cleanupRemote()
			return updateResult{}, err
		}
		operationApplied = true
		return updateResult{Status: updateStatusNoop}, cleanupRemote()
	}
	if err := checkProgress.Complete(fmt.Sprintf("Braid: checked source :%s", m.Name)); err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}

	localHash, err := git.RevParse(ctx, "HEAD")
	if err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}
	updateProgress, err := progress.Start(fmt.Sprintf("Braid: updating source :%s", m.Name))
	if err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}
	failUpdate := func(result updateResult, cause error) (updateResult, error) {
		_ = updateProgress.Abort()
		return result, cause
	}

	baseTree, remoteTree := localHash, localHash
	for _, mirror := range m.SortedMirrors() {
		oldView := original.WithMirror(mirror)
		newView := m.WithMirror(mirror)
		baseItem, basePresent, itemErr := optionalItemAtRevision(ctx, git, oldView, baseRevision)
		if itemErr != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, itemErr)
		}
		remoteItem, remotePresent, itemErr := optionalItemAtRevision(ctx, git, newView, newRevision)
		if itemErr != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, itemErr)
		}
		baseTree, itemErr = replaceTreeItem(ctx, git, baseTree, mirror.LocalPath, baseItem, basePresent)
		if itemErr != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, itemErr)
		}
		remoteTree, itemErr = replaceTreeItem(ctx, git, remoteTree, mirror.LocalPath, remoteItem, remotePresent)
		if itemErr != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, itemErr)
		}
	}
	mergedTree, mergeErr := git.MergeTreeWrite(ctx, baseTree, localHash, remoteTree)
	if mergeErr != nil && !gitexec.IsMergeTreeConflict(mergeErr) {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, mergeErr)
	}
	contentTree := mergedTree.Tree

	m.Revision = newRevision
	if err := cfg.UpdateSource(m.Source); err != nil {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, err)
	}
	configData, err := cfg.MarshalJSON()
	if err != nil {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, err)
	}

	subject := updateCommitSubject(m)
	if mergeErr != nil {
		result := updateResult{Status: updateStatusConflict}
		mergeMsgPath, pathErr := processGit.RepoFilePath(ctx, "MERGE_MSG")
		if pathErr != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, pathErr)
		}
		mergeMsgPath, pathErr = gitRepoOSPath(mergeMsgPath, repo.ProcessWorkDir)
		if pathErr != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, pathErr)
		}
		previousMergeMsg, readErr := os.ReadFile(mergeMsgPath)
		mergeMsgExisted := readErr == nil
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, readErr)
		}
		rollbackFailure := func(cause error) (updateResult, error) {
			var rollbackErrors []error
			operationApplied = false
			ownedPaths := append(m.LocalPaths(), config.FileName)
			if restoreErr := git.RestorePathspecsFromTree(ctx, "HEAD", true, false, ownedPaths...); restoreErr != nil {
				rollbackErrors = append(rollbackErrors, restoreErr)
			} else if restoreErr := git.RestorePathspecsFromTree(ctx, "HEAD", false, true, ownedPaths...); restoreErr != nil {
				rollbackErrors = append(rollbackErrors, restoreErr)
			}
			if mergeMsgExisted {
				if restoreErr := os.WriteFile(mergeMsgPath, previousMergeMsg, 0o644); restoreErr != nil {
					rollbackErrors = append(rollbackErrors, restoreErr)
				}
			} else if removeErr := os.Remove(mergeMsgPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				rollbackErrors = append(rollbackErrors, removeErr)
			}
			if cleanupErr := cleanupRemote(); cleanupErr != nil {
				rollbackErrors = append(rollbackErrors, cleanupErr)
			}
			if len(rollbackErrors) != 0 {
				cause = fmt.Errorf("%w; rollback failed: %w", cause, errors.Join(rollbackErrors...))
			}
			return failUpdate(updateResult{}, cause)
		}
		if err := cfg.WriteFile(filepath.Join(configRoot(h.Options, repo), config.FileName)); err != nil {
			return rollbackFailure(err)
		}
		if mergedTree.Tree != "" {
			if err := git.RestorePathspecsFromTree(ctx, mergedTree.Tree, false, true, m.LocalPaths()...); err != nil {
				return rollbackFailure(err)
			}
		}
		if applier, ok := git.(interface {
			ApplyConflictIndex(context.Context, string, string, []string, ...string) error
		}); ok {
			if err := applier.ApplyConflictIndex(ctx, mergedTree.Tree, mergedTree.ConflictIndex, mergedTree.ConflictPaths, m.LocalPaths()...); err != nil {
				return rollbackFailure(err)
			}
		} else {
			return rollbackFailure(errors.New("git implementation cannot materialize conflict index"))
		}
		if err := git.Add(ctx, config.FileName); err != nil {
			return rollbackFailure(err)
		}
		if err := h.writeConflictInstructions(ctx, git, processGit, stdout, m, mergedTree.ConflictPaths); err != nil {
			return rollbackFailure(err)
		}
		if err := h.writeMergeMessage(ctx, repo, processGit, subject); err != nil {
			return rollbackFailure(err)
		}
		operationApplied = true
		if cleanupErr := cleanupRemote(); cleanupErr != nil {
			return rollbackFailure(cleanupErr)
		}
		_ = updateProgress.Abort()
		return result, nil
	}

	configItem, err := git.HashBytes(ctx, configData)
	if err != nil {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, err)
	}
	finalTree, err := git.MakeTreeWithItemIn(ctx, contentTree, config.FileName, configItem)
	if err != nil {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, err)
	}
	if options.NoCommit {
		warningPaths := runOptions.WarningPaths
		if len(warningPaths) == 0 {
			warningPaths = m.LocalPaths()
		}
		if err := stageNoCommitResult(ctx, git, stdout, noCommitStageOptions{
			Tree:       finalTree,
			Action:     "update",
			MirrorPath: ":" + m.Name,
			Paths:      append(append([]string{}, m.LocalPaths()...), config.FileName),
			OwnedPaths: warningPaths,
			Quiet:      quiet,
			Warned:     runOptions.Warned,
		}); err != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, err)
		}
		if err := updateProgress.Complete(fmt.Sprintf("Braid: updated source :%s to %s", m.Name, shortRevision(newRevision))); err != nil {
			_ = cleanupRemote()
			return updateResult{}, err
		}
		operationApplied = true
		return updateResult{Status: updateStatusUpdated}, cleanupRemote()
	}
	committed, err := git.CommitTreeWithTemporaryIndex(ctx, finalTree, subject)
	if err != nil {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, err)
	}
	if !committed {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, errors.New("pull produced no commit"))
	}
	if err := git.RestorePathspecsFromHead(ctx, append(m.LocalPaths(), config.FileName)...); err != nil {
		cause := err
		if updater, ok := git.(interface {
			UpdateRef(context.Context, ...string) error
		}); ok {
			if newHead, headErr := git.RevParse(ctx, "HEAD"); headErr == nil {
				if rollbackErr := updater.UpdateRef(ctx, "HEAD", localHash, newHead); rollbackErr != nil {
					cause = fmt.Errorf("%w; rollback failed: %w", cause, rollbackErr)
				} else if rollbackErr := git.RestorePathspecsFromHead(ctx, append(m.LocalPaths(), config.FileName)...); rollbackErr != nil {
					cause = fmt.Errorf("%w; rollback restore failed: %w", cause, rollbackErr)
				}
			} else {
				cause = fmt.Errorf("%w; rollback could not resolve advanced HEAD: %w", cause, headErr)
			}
		} else {
			cause = fmt.Errorf("%w; git implementation cannot roll back advanced HEAD", cause)
		}
		_ = cleanupRemote()
		return failUpdate(updateResult{}, cause)
	}
	if err := updateProgress.Complete(fmt.Sprintf("Braid: updated source :%s to %s", m.Name, shortRevision(newRevision))); err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}
	operationApplied = true
	return updateResult{Status: updateStatusUpdated}, cleanupRemote()
}

func applyUpdateStrategy(m *source.SourceMirror, options cli.UpdateOptions) {
	switch {
	case options.Tag != "":
		m.Tracking = source.TagTracking{Tag: options.Tag}
	case options.Branch != "":
		m.Tracking = source.BranchTracking{Branch: options.Branch}
	case options.Revision != "":
		m.Tracking = source.RevisionTracking{}
	}
}

func resolveUpdateRevision(ctx context.Context, git UpdateGit, cache CacheConfig, m source.SourceMirror, requested string) (string, error) {
	if requested != "" {
		return git.RevParse(ctx, cacheResolveRequestedRevision(cache, m, requested)+"^{commit}")
	}
	return resolveAddRevision(ctx, git, m, "")
}

func updateSwitchesTracking(original, next source.SourceMirror, options cli.UpdateOptions, newRevision string) bool {
	if original.Branch() != next.Branch() || original.Tag() != next.Tag() {
		return true
	}
	return options.Revision != "" && original.Revision != newRevision
}

type treeItemGit interface {
	LsTreeItem(context.Context, string, string) (gitexec.TreeItem, error)
	TreeContainsGitlink(context.Context, string, string) (bool, error)
}

type revisionItemGit interface {
	treeItemGit
	RevParse(context.Context, string) (string, error)
}

func itemAtRevision(ctx context.Context, git treeItemGit, m source.SourceMirror, revision string) (gitexec.TreeItem, error) {
	if contains, err := git.TreeContainsGitlink(ctx, revision, m.UpstreamPath); err != nil {
		return gitexec.TreeItem{}, err
	} else if contains {
		return gitexec.TreeItem{}, fmt.Errorf("mirror %s contains an unsupported gitlink", m.LocalPath)
	}
	if m.UpstreamPath == "" {
		return gitexec.TreeItem{Type: "tree", Hash: revision}, nil
	}
	return git.LsTreeItem(ctx, revision, m.UpstreamPath)
}

func comparableItemAtRevision(ctx context.Context, git revisionItemGit, m source.SourceMirror, revision string) (gitexec.TreeItem, error) {
	if contains, err := git.TreeContainsGitlink(ctx, revision, m.UpstreamPath); err != nil {
		return gitexec.TreeItem{}, err
	} else if contains {
		return gitexec.TreeItem{}, fmt.Errorf("mirror %s contains an unsupported gitlink", m.LocalPath)
	}
	if m.UpstreamPath == "" {
		tree, err := git.RevParse(ctx, revision+"^{tree}")
		if err != nil {
			return gitexec.TreeItem{}, err
		}
		return gitexec.TreeItem{Mode: "040000", Type: "tree", Hash: tree}, nil
	}
	return git.LsTreeItem(ctx, revision, m.UpstreamPath)
}

func optionalItemAtRevision(ctx context.Context, git revisionItemGit, m source.SourceMirror, revision string) (gitexec.TreeItem, bool, error) {
	item, err := comparableItemAtRevision(ctx, git, m, revision)
	if gitexec.IsTreeItemNotFound(err) {
		return gitexec.TreeItem{}, false, nil
	}
	if err != nil {
		return gitexec.TreeItem{}, false, err
	}
	if item.Type == "commit" || item.Mode == "160000" {
		return gitexec.TreeItem{}, false, fmt.Errorf("mirror %s points to unsupported gitlink %s", m.LocalPath, m.UpstreamPath)
	}
	return item, true, nil
}

func replaceTreeItem(ctx context.Context, git UpdateGit, tree, localPath string, item gitexec.TreeItem, present bool) (string, error) {
	if present {
		return git.MakeTreeWithItemIn(ctx, tree, localPath, item)
	}
	remover, ok := git.(interface {
		MakeTreeWithoutPath(context.Context, string, string) (string, error)
	})
	if !ok {
		return "", errors.New("git implementation cannot remove mirror paths")
	}
	return remover.MakeTreeWithoutPath(ctx, tree, localPath)
}

func updateCommitSubject(m source.SourceMirror) string {
	return fmt.Sprintf("Braid: Update source '%s' to '%s'", m.Name, shortRevision(m.Revision))
}

func (h UpdateHandler) writeMergeMessage(ctx context.Context, repo RepoContext, git repoPathGit, subject string) error {
	mergeMsgPath, err := git.RepoFilePath(ctx, "MERGE_MSG")
	if err != nil {
		return err
	}
	mergeMsgPath, err = gitRepoOSPath(mergeMsgPath, repo.ProcessWorkDir)
	if err != nil {
		return err
	}
	return os.WriteFile(mergeMsgPath, []byte(subject+"\n"), 0o644)
}

func (h UpdateHandler) writeConflictInstructions(ctx context.Context, git UpdateGit, processGit repoPathGit, stdout io.Writer, m source.SourceMirror, conflictPaths []string) error {
	conflictPaths = append([]string(nil), conflictPaths...)
	sort.Strings(conflictPaths)
	if _, err := fmt.Fprintf(stdout, "Braid: conflicts while updating source :%s:\n", m.Name); err != nil {
		return err
	}
	for _, conflictPath := range conflictPaths {
		if _, err := fmt.Fprintf(stdout, "  %s\n", conflictPath); err != nil {
			return err
		}
	}
	staged, err := hasUnrelatedStagedEntries(ctx, git, m.LocalPaths()...)
	if err != nil {
		return err
	}
	if staged {
		if _, err := io.WriteString(stdout, "Braid: warning: unrelated staged changes are present; unstage them before the resolution commit if they should not be included.\n"); err != nil {
			return err
		}
	}
	mergeMsgPath, err := processGit.RepoFilePath(ctx, "MERGE_MSG")
	if err != nil {
		return err
	}
	quoted := make([]string, 0, len(m.Mirrors)+1)
	for _, path := range m.LocalPaths() {
		quoted = append(quoted, shellQuote(":(top)"+path))
	}
	quoted = append(quoted, shellQuote(":(top)"+config.FileName))
	_, err = fmt.Fprintf(stdout, "Resolve them, then run:\n  git add -- %s\n  git commit -F %s\n", strings.Join(quoted, " "), shellQuote(mergeMsgPath))
	return err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func pathWithin(path, scope string) bool {
	cleanPath := strings.TrimRight(path, "/")
	cleanScope := strings.TrimRight(scope, "/")
	return cleanPath == cleanScope || strings.HasPrefix(cleanPath, cleanScope+"/")
}

func (h UpdateHandler) ensureUpdateTargetsClean(ctx context.Context, repo RepoContext, git UpdateGit, cfg config.Config, localPaths []string) error {
	var paths []string
	seen := map[string]bool{}
	for _, localPath := range localPaths {
		s, mirror, err := cfg.MirrorByLocalPathRequired(localPath)
		if err != nil {
			return err
		}
		m := s.WithMirror(mirror)
		for _, path := range m.LocalPaths() {
			if mirrorOverlapsConfig(path) {
				return fmt.Errorf("mirror path %q overlaps %s", path, config.FileName)
			}
			if !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
		}
	}
	return ensureCommandScopesClean(ctx, git, configRoot(h.Options, repo), true, paths...)
}

func ensureCommandScopesClean(ctx context.Context, git scopedCleanGit, root string, requireConfig bool, paths ...string) error {
	if state, blocked, err := git.BlockingOperation(ctx); err != nil {
		return err
	} else if blocked {
		return fmt.Errorf("unresolved git operation state is present: %s", state)
	}
	if err := ensureConfigClean(ctx, git, root, requireConfig); err != nil {
		return err
	}
	for _, path := range paths {
		if err := ensureScopedClean(ctx, git, path); err != nil {
			return err
		}
	}
	return nil
}

func ensureConfigClean(ctx context.Context, git scopedCleanGit, root string, required bool) error {
	if !required {
		if _, err := os.Stat(filepath.Join(root, config.FileName)); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
	}
	return ensureScopedClean(ctx, git, config.FileName)
}

func ensureScopedClean(ctx context.Context, git scopedCleanGit, path string) error {
	var status string
	var err error
	if ignoredGit, ok := git.(interface {
		StatusPorcelainPathspecsWithIgnored(context.Context, ...string) (string, error)
	}); ok {
		status, err = ignoredGit.StatusPorcelainPathspecsWithIgnored(ctx, path)
	} else {
		status, err = git.StatusPorcelainPathspecs(ctx, path)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("local changes are present in %s", path)
	}
	return nil
}

func mirrorOverlapsConfig(path string) bool {
	clean := strings.TrimRight(path, "/")
	return clean == config.FileName || strings.HasPrefix(clean, config.FileName+"/")
}
