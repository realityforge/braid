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

type mirrorItemStatus int

const (
	mirrorItemPresent mirrorItemStatus = iota
	mirrorItemAbsent
	mirrorItemError
)

type mirrorItemState struct {
	Status mirrorItemStatus
	Item   gitexec.TreeItem
	Err    error
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
	cache, err := runtimeCache(inv.Global)
	if err != nil {
		return err
	}

	if inv.Update.LocalPath != "" {
		localPath, err := normalizeLocalPath(repo, inv.Update.LocalPath)
		if err != nil {
			return err
		}
		_, err = h.updateOne(ctx, repo, git, processGit, cache, localPath, inv.Update, inv.Global.Verbose, progress, stdout, stderr)
		return err
	}
	return h.updateAll(ctx, repo, git, processGit, cache, inv.Update, inv.Global.Verbose, progress, stdout, stderr)
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

func (h UpdateHandler) updateAll(ctx context.Context, repo RepoContext, git UpdateGit, processGit repoPathGit, cache CacheConfig, options cli.UpdateOptions, verbose bool, progress progressReporter, stdout, trace io.Writer) error {
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}

	var targets []string
	var skippedLocked []string
	for _, localPath := range cfg.Paths() {
		m := cfg.Mirrors[localPath]
		if m.Locked() {
			skippedLocked = append(skippedLocked, localPath)
			continue
		}
		targets = append(targets, localPath)
	}
	if err := h.ensureUpdateTargetsClean(ctx, repo, git, cfg, targets); err != nil {
		return err
	}

	for _, localPath := range targets {
		if _, err := h.updateOne(ctx, repo, git, processGit, cache, localPath, options, verbose, progress, stdout, trace); err != nil {
			return fmt.Errorf("pull %s: %w", localPath, err)
		}
	}
	return writeSkippedLockedMirrors(stdout, skippedLocked)
}

func writeSkippedLockedMirrors(stdout io.Writer, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if _, err := io.WriteString(stdout, "Braid: skipped revision-locked mirrors:\n"); err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := fmt.Fprintf(stdout, "  %s\n", path); err != nil {
			return err
		}
	}
	return nil
}

func (h UpdateHandler) updateOne(ctx context.Context, repo RepoContext, git UpdateGit, processGit repoPathGit, cache CacheConfig, localPath string, options cli.UpdateOptions, verbose bool, progress progressReporter, stdout, trace io.Writer) (updateResult, error) {
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return updateResult{}, err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return updateResult{}, err
	}
	m, err := cfg.GetRequired(localPath)
	if err != nil {
		return updateResult{}, err
	}
	original := m
	applyUpdateStrategy(&m, options)
	if err := h.ensureUpdateTargetsClean(ctx, repo, git, cfg, []string{localPath}); err != nil {
		return updateResult{}, err
	}

	if cache.Enabled {
		if err := fetchCache(ctx, cache, m, verbose, progress, trace); err != nil {
			return updateResult{}, err
		}
	}
	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return updateResult{}, err
	}
	cleanupRemote := func() error {
		if options.Keep {
			return nil
		}
		return git.RemoteRemove(ctx, m.Remote())
	}

	if err := fetchMirror(ctx, git, m, progress); err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}

	checkProgress, err := progress.Start(fmt.Sprintf("Braid: checking mirror %s", m.Path))
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
	newRevision, err := resolveUpdateRevision(ctx, git, m, options.Revision)
	if err != nil {
		_ = checkProgress.Abort()
		_ = cleanupRemote()
		return updateResult{}, err
	}
	if !updateSwitchesTracking(original, m, options, newRevision) && newRevision == baseRevision {
		if err := checkProgress.Complete(fmt.Sprintf("Braid: mirror %s already up to date", m.Path)); err != nil {
			_ = cleanupRemote()
			return updateResult{}, err
		}
		return updateResult{Status: updateStatusNoop}, cleanupRemote()
	}
	if err := checkProgress.Complete(fmt.Sprintf("Braid: checked mirror %s", m.Path)); err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}

	baseItem, err := baseDiffItem(ctx, git, original)
	if err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}
	remoteItem, err := itemAtRevision(ctx, git, m, newRevision)
	if err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}

	localHash, err := git.RevParse(ctx, "HEAD")
	if err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}
	localItem := currentMirrorItem(ctx, git, m.Path)
	if localItem.Status == mirrorItemError {
		_ = cleanupRemote()
		return updateResult{}, localItem.Err
	}
	baseCompareItem, err := comparableItemAtRevision(ctx, git, original, baseRevision)
	if err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}
	remoteCompareItem, err := comparableItemAtRevision(ctx, git, m, newRevision)
	if err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}

	updateProgress, err := progress.Start(fmt.Sprintf("Braid: updating mirror %s", m.Path))
	if err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}
	failUpdate := func(result updateResult, cause error) (updateResult, error) {
		_ = updateProgress.Abort()
		return result, cause
	}

	var contentTree string
	mergedTree := gitexec.MergeTreeResult{Tree: localHash}
	var mergeErr error
	switch {
	case localItem.Status == mirrorItemPresent && sameTreeItem(localItem.Item, remoteCompareItem):
		contentTree = localHash
	case localItem.Status == mirrorItemPresent && sameTreeItem(localItem.Item, baseCompareItem):
		contentTree, err = git.MakeTreeWithItemIn(ctx, localHash, m.Path, remoteItem)
		if err != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, err)
		}
	default:
		baseTree, err := git.MakeTreeWithItemIn(ctx, localHash, m.Path, baseItem)
		if err != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, err)
		}
		remoteTree, err := git.MakeTreeWithItemIn(ctx, localHash, m.Path, remoteItem)
		if err != nil {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, err)
		}
		mergedTree, mergeErr = git.MergeTreeWrite(ctx, baseTree, localHash, remoteTree)
		if mergeErr != nil && !gitexec.IsMergeTreeConflict(mergeErr) {
			_ = cleanupRemote()
			return failUpdate(updateResult{}, mergeErr)
		}
		contentTree = mergedTree.Tree
	}

	m.Revision = newRevision
	if err := cfg.Update(m); err != nil {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, err)
	}
	if err := cfg.WriteFile(filepath.Join(configRoot(h.Options, repo), config.FileName)); err != nil {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, err)
	}
	configItem, err := git.HashFile(ctx, config.FileName)
	if err != nil {
		_ = cleanupRemote()
		if mergeErr != nil {
			return failUpdate(updateResult{Status: updateStatusConflict}, err)
		}
		return failUpdate(updateResult{}, err)
	}

	subject := updateCommitSubject(m)
	if mergeErr != nil {
		result := updateResult{Status: updateStatusConflict}
		if err := writeConflictSummary(stdout, mergedTree.ConflictPaths); err != nil {
			return failUpdate(result, err)
		}
		if _, err := io.WriteString(stdout, mergedTree.Details); err != nil {
			return failUpdate(result, err)
		}
		if mergedTree.Tree != "" {
			if err := git.RestorePathspecsFromTree(ctx, mergedTree.Tree, false, true, m.Path); err != nil {
				_ = cleanupRemote()
				return failUpdate(result, err)
			}
		}
		if err := git.Add(ctx, config.FileName); err != nil {
			_ = cleanupRemote()
			return failUpdate(result, err)
		}
		if err := h.writeConflictInstructions(ctx, git, processGit, stdout, m); err != nil {
			_ = cleanupRemote()
			return failUpdate(result, err)
		}
		if err := h.writeMergeMessage(ctx, repo, processGit, subject); err != nil {
			return failUpdate(result, err)
		}
		_ = updateProgress.Abort()
		return result, nil
	}

	finalTree, err := git.MakeTreeWithItemIn(ctx, contentTree, config.FileName, configItem)
	if err != nil {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, err)
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
	if err := git.RestorePathspecsFromHead(ctx, m.Path, config.FileName); err != nil {
		_ = cleanupRemote()
		return failUpdate(updateResult{}, err)
	}
	if err := updateProgress.Complete(fmt.Sprintf("Braid: updated mirror %s to %s", m.Path, shortRevision(newRevision))); err != nil {
		_ = cleanupRemote()
		return updateResult{}, err
	}
	return updateResult{Status: updateStatusUpdated}, cleanupRemote()
}

func applyUpdateStrategy(m *mirror.Mirror, options cli.UpdateOptions) {
	switch {
	case options.Tag != "":
		m.Tag = options.Tag
		m.Branch = ""
	case options.Branch != "":
		m.Branch = options.Branch
		m.Tag = ""
	case options.Revision != "":
		m.Branch = ""
		m.Tag = ""
	}
}

func resolveUpdateRevision(ctx context.Context, git UpdateGit, m mirror.Mirror, requested string) (string, error) {
	if requested != "" {
		return git.RevParse(ctx, requested+"^{commit}")
	}
	return resolveAddRevision(ctx, git, m, "")
}

func updateSwitchesTracking(original, next mirror.Mirror, options cli.UpdateOptions, newRevision string) bool {
	if original.Branch != next.Branch || original.Tag != next.Tag {
		return true
	}
	return options.Revision != "" && original.Revision != newRevision
}

type treeItemGit interface {
	LsTreeItem(context.Context, string, string) (gitexec.TreeItem, error)
}

type revisionItemGit interface {
	RevParse(context.Context, string) (string, error)
	LsTreeItem(context.Context, string, string) (gitexec.TreeItem, error)
}

func itemAtRevision(ctx context.Context, git treeItemGit, m mirror.Mirror, revision string) (gitexec.TreeItem, error) {
	if m.RemotePath == "" {
		return gitexec.TreeItem{Type: "tree", Hash: revision}, nil
	}
	return git.LsTreeItem(ctx, revision, m.RemotePath)
}

func comparableItemAtRevision(ctx context.Context, git revisionItemGit, m mirror.Mirror, revision string) (gitexec.TreeItem, error) {
	if m.RemotePath == "" {
		tree, err := git.RevParse(ctx, revision+"^{tree}")
		if err != nil {
			return gitexec.TreeItem{}, err
		}
		return gitexec.TreeItem{Mode: "040000", Type: "tree", Hash: tree}, nil
	}
	return git.LsTreeItem(ctx, revision, m.RemotePath)
}

func currentMirrorItem(ctx context.Context, git treeItemGit, path string) mirrorItemState {
	item, err := git.LsTreeItem(ctx, "HEAD", path)
	if err == nil {
		return mirrorItemState{Status: mirrorItemPresent, Item: item}
	}
	if gitexec.IsTreeItemNotFound(err) {
		return mirrorItemState{Status: mirrorItemAbsent}
	}
	return mirrorItemState{Status: mirrorItemError, Err: err}
}

func writeConflictSummary(stdout io.Writer, paths []string) error {
	if len(paths) == 0 {
		paths = []string{"(unknown path)"}
	}
	for _, path := range paths {
		if _, err := fmt.Fprintf(stdout, "CONFLICT: %s\n", path); err != nil {
			return err
		}
	}
	return nil
}

func updateCommitSubject(m mirror.Mirror) string {
	return fmt.Sprintf("Braid: Update mirror '%s' to '%s'", m.Path, shortRevision(m.Revision))
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

func (h UpdateHandler) writeConflictInstructions(ctx context.Context, git UpdateGit, processGit repoPathGit, stdout io.Writer, m mirror.Mirror) error {
	staged, err := hasUnrelatedStagedEntries(ctx, git, m.Path)
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
	_, err = fmt.Fprintf(stdout, "Braid: conflicts written to %s. Resolve them, then run:\n  git add -- %s %s\n  git commit -F %s\n", m.Path, shellQuote(":(top)"+m.Path), shellQuote(":(top)"+config.FileName), shellQuote(mergeMsgPath))
	return err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func hasUnrelatedStagedEntries(ctx context.Context, git UpdateGit, mirrorPath string) (bool, error) {
	out, err := git.Diff(ctx, "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	for _, path := range strings.Split(out, "\n") {
		path = strings.TrimSpace(path)
		if path == "" || path == config.FileName || pathWithin(path, mirrorPath) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func pathWithin(path, scope string) bool {
	cleanPath := strings.TrimRight(path, "/")
	cleanScope := strings.TrimRight(scope, "/")
	return cleanPath == cleanScope || strings.HasPrefix(cleanPath, cleanScope+"/")
}

func (h UpdateHandler) ensureUpdateTargetsClean(ctx context.Context, repo RepoContext, git UpdateGit, cfg config.Config, localPaths []string) error {
	var paths []string
	for _, localPath := range localPaths {
		m, err := cfg.GetRequired(localPath)
		if err != nil {
			return err
		}
		if mirrorOverlapsConfig(m.Path) {
			return fmt.Errorf("mirror path %q overlaps %s", m.Path, config.FileName)
		}
		paths = append(paths, m.Path)
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
	status, err := git.StatusPorcelainPathspecs(ctx, path)
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
