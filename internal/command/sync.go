package command

import (
	"context"
	"fmt"
	"io"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
)

type SyncHandler struct {
	Options Options
}

type syncTarget struct {
	LocalPath string
	Mirror    mirror.Mirror
}

type syncPushAction struct {
	Target       syncTarget
	BaseRevision string
}

type syncPushPlan struct {
	Actions []syncPushAction
}

type syncAutostashGit interface {
	PushGit
	StatusPorcelainPathspecsWithIgnored(context.Context, ...string) (string, error)
	StashPushAllPathspecs(context.Context, string, ...string) (gitexec.StashEntry, bool, error)
	syncAutostashRestoreGit
}

type syncAutostashRestoreGit interface {
	StashApply(context.Context, string) error
	RestoreStashIndexPathspecs(context.Context, string, ...string) error
	DropStashEntry(context.Context, gitexec.StashEntry) (string, error)
}

type syncAutostash struct {
	Entry gitexec.StashEntry
	Paths []string
}

const syncAutostashMessage = "braid sync autostash"

func (h SyncHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandSync, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.syncGit(repo, inv, stderr)
	processGit := UpdateHandler(h).processRepoPathGit(repo, inv, stderr)
	progress := newProgressReporter(stderr, inv.Global.Quiet)
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	targets, skippedLocked, err := h.syncTargets(repo, cfg, inv.Sync.LocalPaths)
	if err != nil {
		return err
	}
	cache, err := runtimeCache(inv.Global)
	if err != nil {
		return err
	}
	autostash, autostashOK, err := h.prepareSyncAutostash(ctx, repo, git, targets, inv.Sync.Autostash)
	if err != nil {
		return err
	}
	var runErr error
	var updateConflict bool
	if !inv.Sync.PullOnly {
		if err := h.hydrateMissingRecordedRevisions(ctx, git, cache, targets, inv.Sync.Keep, inv.Global.Verbose, progress, stderr); err != nil {
			runErr = err
		}
		if runErr == nil {
			plan, err := h.buildPushPlan(ctx, git, cache, targets, inv.Sync.Keep, inv.Global.Verbose, progress, stderr)
			if err != nil {
				runErr = err
			} else if err := h.runPushPlan(ctx, repo, git, plan, inv, stdout, stderr); err != nil {
				runErr = err
			}
		}
	}

	if runErr == nil {
		update := UpdateHandler(h)
		updateOptions := cli.UpdateOptions{Keep: inv.Sync.Keep}
		for _, target := range targets {
			result, err := update.updateOne(ctx, repo, git, processGit, cache, target.LocalPath, updateOptions, inv.Global.Verbose, inv.Global.Quiet, progress, stdout, stderr)
			if result.Status == updateStatusConflict && autostashOK {
				updateConflict = true
				if err != nil {
					runErr = fmt.Errorf("pull %s: %w", target.LocalPath, err)
				} else {
					runErr = fmt.Errorf("pull %s reached conflict state", target.LocalPath)
				}
				break
			}
			if err != nil {
				runErr = fmt.Errorf("pull %s: %w", target.LocalPath, err)
				break
			}
		}
	}

	if autostashOK {
		if updateConflict {
			return h.autostashConflictError(autostash, runErr)
		}
		if restoreErr := h.restoreSyncAutostash(ctx, git, autostash); restoreErr != nil {
			if runErr != nil {
				return fmt.Errorf("%w; additionally, %w", runErr, restoreErr)
			}
			return restoreErr
		}
	}
	if runErr != nil {
		return runErr
	}
	return writeSkippedLockedMirrors(stdout, skippedLocked)
}

func (h SyncHandler) syncGit(repo RepoContext, inv cli.Invocation, trace io.Writer) syncAutostashGit {
	if git, ok := h.Options.Git.(syncAutostashGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(syncAutostashGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (h SyncHandler) syncTargets(repo RepoContext, cfg config.Config, localPaths []string) ([]syncTarget, []string, error) {
	if len(localPaths) == 0 {
		targets := make([]syncTarget, 0, len(cfg.Mirrors))
		var skippedLocked []string
		for _, localPath := range cfg.Paths() {
			m := cfg.Mirrors[localPath]
			if m.Locked() {
				skippedLocked = append(skippedLocked, localPath)
				continue
			}
			targets = append(targets, syncTarget{LocalPath: localPath, Mirror: m})
		}
		return targets, skippedLocked, nil
	}

	targets := make([]syncTarget, 0, len(localPaths))
	seen := map[string]struct{}{}
	for _, rawPath := range localPaths {
		localPath, err := normalizeLocalPath(repo, rawPath)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := seen[localPath]; ok {
			return nil, nil, fmt.Errorf("duplicate sync path: %s", localPath)
		}
		seen[localPath] = struct{}{}
		m, err := cfg.GetRequired(localPath)
		if err != nil {
			return nil, nil, err
		}
		targets = append(targets, syncTarget{LocalPath: localPath, Mirror: m})
	}
	return targets, nil, nil
}

func (h SyncHandler) prepareSyncAutostash(ctx context.Context, repo RepoContext, git syncAutostashGit, targets []syncTarget, autostashEnabled bool) (syncAutostash, bool, error) {
	paths := make([]string, 0, len(targets))
	for _, target := range targets {
		if mirrorOverlapsConfig(target.Mirror.Path) {
			return syncAutostash{}, false, fmt.Errorf("mirror path %q overlaps %s", target.Mirror.Path, config.FileName)
		}
		paths = append(paths, target.Mirror.Path)
	}
	if state, blocked, err := git.BlockingOperation(ctx); err != nil {
		return syncAutostash{}, false, err
	} else if blocked {
		return syncAutostash{}, false, fmt.Errorf("unresolved git operation state is present: %s", state)
	}
	if err := ensureConfigClean(ctx, git, configRoot(h.Options, repo), true); err != nil {
		return syncAutostash{}, false, err
	}
	if !autostashEnabled {
		for _, path := range paths {
			if err := ensureScopedClean(ctx, git, path); err != nil {
				return syncAutostash{}, false, err
			}
		}
		return syncAutostash{}, false, nil
	}
	if len(paths) == 0 {
		return syncAutostash{}, false, nil
	}
	status, err := git.StatusPorcelainPathspecsWithIgnored(ctx, paths...)
	if err != nil {
		return syncAutostash{}, false, err
	}
	if strings.TrimSpace(status) == "" {
		return syncAutostash{}, false, nil
	}
	entry, saved, err := git.StashPushAllPathspecs(ctx, syncAutostashMessage, paths...)
	if err != nil {
		return syncAutostash{}, false, err
	}
	if !saved {
		return syncAutostash{}, false, nil
	}
	return syncAutostash{Entry: entry, Paths: paths}, true, nil
}

func (h SyncHandler) restoreSyncAutostash(ctx context.Context, git syncAutostashRestoreGit, saved syncAutostash) error {
	if err := git.StashApply(ctx, saved.Entry.OID); err != nil {
		return fmt.Errorf("failed to restore braid sync autostash %s: %w. %s", saved.Entry.OID, err, manualAutostashRestoreInstructions(saved))
	}
	if err := git.RestoreStashIndexPathspecs(ctx, saved.Entry.OID, saved.Paths...); err != nil {
		return fmt.Errorf("failed to restore selected-path index state from braid sync autostash %s: %w. %s", saved.Entry.OID, err, manualAutostashRestoreInstructions(saved))
	}
	if _, err := git.DropStashEntry(ctx, saved.Entry); err != nil {
		return fmt.Errorf("restored saved work from braid sync autostash %s, but could not remove the stash entry: %w. %s", saved.Entry.OID, err, manualAutostashCleanupInstructions(saved))
	}
	return nil
}

func (h SyncHandler) autostashConflictError(saved syncAutostash, err error) error {
	if err == nil {
		return fmt.Errorf("sync reached pull conflict state. %s", manualAutostashConflictInstructions(saved))
	}
	return fmt.Errorf("%w. %s", err, manualAutostashConflictInstructions(saved))
}

func manualAutostashConflictInstructions(saved syncAutostash) string {
	return fmt.Sprintf("Braid preserved autostash %s. Resolve the Braid pull conflict first, then restore your saved work manually: %s", saved.Entry.OID, manualAutostashRestoreCommands(saved))
}

func manualAutostashRestoreInstructions(saved syncAutostash) string {
	return fmt.Sprintf("The saved work remains in stash %s. To restore it manually, run: %s", saved.Entry.OID, manualAutostashRestoreCommands(saved))
}

func manualAutostashCleanupInstructions(saved syncAutostash) string {
	return fmt.Sprintf("The saved stash %s remains recoverable. Inspect `git stash list` and drop the Braid autostash entry manually after verifying your work is restored.", saved.Entry.OID)
}

func manualAutostashRestoreCommands(saved syncAutostash) string {
	quotedPaths := make([]string, 0, len(saved.Paths))
	for _, path := range saved.Paths {
		quotedPaths = append(quotedPaths, shellQuote(":(top)"+path))
	}
	return fmt.Sprintf("git stash apply %s && git restore --source=%s^2 --staged -- %s", saved.Entry.OID, saved.Entry.OID, strings.Join(quotedPaths, " "))
}

func (h SyncHandler) hydrateMissingRecordedRevisions(ctx context.Context, git PushGit, cache CacheConfig, targets []syncTarget, keep bool, verbose bool, progress progressReporter, trace io.Writer) error {
	for _, target := range targets {
		if _, err := git.RevParse(ctx, target.Mirror.Revision+"^{commit}"); err == nil {
			continue
		}
		err := h.withFetchedMirrorForPlanning(ctx, git, cache, target.Mirror, keep, verbose, progress, trace, func() error {
			if _, err := git.RevParse(ctx, target.Mirror.Revision+"^{commit}"); err != nil {
				return fmt.Errorf("recorded revision %s for %s is unavailable after hydration", target.Mirror.Revision, target.LocalPath)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("hydrate %s: %w", target.LocalPath, err)
		}
	}
	return nil
}

func (h SyncHandler) buildPushPlan(ctx context.Context, git PushGit, cache CacheConfig, targets []syncTarget, keep bool, verbose bool, progress progressReporter, trace io.Writer) (syncPushPlan, error) {
	actions := make([]syncPushAction, 0, len(targets))
	for _, target := range targets {
		changed, baseRevision, err := committedLocalMirrorChange(ctx, git, target.Mirror)
		if err != nil {
			if isMissingTreeItemError(err) {
				return syncPushPlan{}, syncMirrorPathDeletedError(target.LocalPath)
			}
			return syncPushPlan{}, err
		}
		if !changed {
			continue
		}
		if target.Mirror.Branch == "" {
			return syncPushPlan{}, syncNonBranchLocalChangeError(target.LocalPath)
		}
		actions = append(actions, syncPushAction{Target: target, BaseRevision: baseRevision})
	}

	for _, action := range actions {
		var upstreamRevision string
		err := h.withFetchedMirrorForPlanning(ctx, git, cache, action.Target.Mirror, keep, verbose, progress, trace, func() error {
			var err error
			upstreamRevision, err = resolveAddRevision(ctx, git, action.Target.Mirror, "")
			return err
		})
		if err != nil {
			return syncPushPlan{}, fmt.Errorf("check upstream freshness for %s: %w", action.Target.LocalPath, err)
		}
		if upstreamRevision != action.BaseRevision {
			return syncPushPlan{}, syncNotUpToDateError(action.Target.LocalPath)
		}
	}
	return syncPushPlan{Actions: actions}, nil
}

func (h SyncHandler) withFetchedMirrorForPlanning(ctx context.Context, git PushGit, cache CacheConfig, m mirror.Mirror, keep bool, verbose bool, progress progressReporter, trace io.Writer, fn func() error) (err error) {
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m, verbose, progress, trace); err != nil {
			return err
		}
	}
	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return err
	}
	if !keep {
		defer func() {
			removeErr := git.RemoteRemove(ctx, m.Remote())
			if err == nil {
				err = removeErr
			}
		}()
	}
	if err := fetchMirror(ctx, git, m, progress); err != nil {
		return err
	}
	return fn()
}

func (h SyncHandler) runPushPlan(ctx context.Context, repo RepoContext, git PushGit, plan syncPushPlan, inv cli.Invocation, stdout, stderr io.Writer) error {
	push := PushHandler(h)
	for _, action := range plan.Actions {
		result, err := push.push(ctx, repo, git, action.Target.Mirror, action.Target.Mirror.Branch, inv.Sync.Keep, "", inv.Global, stdout, stderr)
		if err != nil {
			return err
		}
		if result.Status == pushStatusNotUpToDate {
			return syncNotUpToDateError(action.Target.LocalPath)
		}
	}
	return nil
}

func committedLocalMirrorChange(ctx context.Context, git PushGit, m mirror.Mirror) (bool, string, error) {
	localItem, err := git.LsTreeItem(ctx, "HEAD", m.Path)
	if err != nil {
		return false, "", err
	}
	baseRevision, err := git.RevParse(ctx, m.Revision+"^{commit}")
	if err != nil {
		return false, "", err
	}
	newTree, err := git.MakeTreeWithItemIn(ctx, baseRevision, m.RemotePath, localItem)
	if err != nil {
		return false, "", err
	}
	baseTree, err := git.RevParse(ctx, baseRevision+"^{tree}")
	if err != nil {
		return false, "", err
	}
	return newTree != baseTree, baseRevision, nil
}

func isMissingTreeItemError(err error) bool {
	return strings.Contains(err.Error(), "no tree item exists at")
}

func syncNotUpToDateError(localPath string) error {
	return fmt.Errorf("sync cannot push %s because the upstream branch is not up to date; run braid pull %s, resolve conflicts if needed, commit, then rerun braid sync", localPath, localPath)
}

func syncNonBranchLocalChangeError(localPath string) error {
	return fmt.Errorf("sync cannot push committed local changes for non-branch mirror %s; run braid push %s --branch <branch> or rerun braid sync --pull-only %s if you only intended to pull", localPath, localPath, localPath)
}

func syncMirrorPathDeletedError(localPath string) error {
	return fmt.Errorf("sync cannot push deletion of mirror path %s; restore the mirror path, commit, then rerun braid sync", localPath)
}
