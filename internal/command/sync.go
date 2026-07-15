package command

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/source"
)

type SyncHandler struct {
	Options Options
}

type syncTarget struct {
	LocalPath string
	Mirror    source.SourceMirror
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
	cache, err := runtimeCacheForRepo(ctx, repo, inv.Global, inv.Global.Verbose, stderr)
	if err != nil {
		return err
	}
	autostash, autostashOK, err := h.prepareSyncAutostash(ctx, repo, git, targets, inv.Sync.Autostash)
	if err != nil {
		return err
	}
	var runErr error
	var updateConflict bool
	var pushedSources []string
	if !inv.Sync.PullOnly {
		pushTargets := make([]syncTarget, 0, len(targets))
		for _, target := range targets {
			if target.Mirror.SyncPush {
				pushTargets = append(pushTargets, target)
			}
		}
		if err := h.hydrateMissingRecordedRevisions(ctx, git, cache, pushTargets, inv.Sync.Keep, inv.Global.Verbose, progress, stderr); err != nil {
			runErr = err
		}
		if runErr == nil {
			plan, err := h.buildPushPlan(ctx, git, cache, pushTargets, inv.Sync.Keep, inv.Global.Verbose, progress, stderr)
			if err != nil {
				runErr = err
			} else {
				pushedSources, err = h.runPushPlan(ctx, repo, git, plan, inv, stdout, stderr)
				if err != nil {
					runErr = err
				}
			}
		}
	}

	if runErr == nil {
		update := UpdateHandler(h)
		updateOptions := cli.UpdateOptions{Keep: inv.Sync.Keep}
		for _, target := range targets {
			result, err := update.updateOne(ctx, repo, git, processGit, cache, target.LocalPath, updateOptions, inv.Global.Verbose, inv.Global.Quiet, progress, stdout, stderr)
			if result.Status == updateStatusConflict {
				updateConflict = autostashOK
				if err != nil {
					runErr = fmt.Errorf("pull %s: %w", target.LocalPath, err)
				} else {
					runErr = fmt.Errorf("pull %s reached conflict state", target.LocalPath)
				}
				if len(pushedSources) != 0 {
					runErr = syncPostPushPullFailure(runErr, pushedSources)
				}
				break
			}
			if err != nil {
				runErr = fmt.Errorf("pull %s: %w", target.LocalPath, err)
				if len(pushedSources) != 0 {
					runErr = syncPostPushPullFailure(runErr, pushedSources)
				}
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
			if len(pushedSources) != 0 {
				return syncPostPushPullFailure(restoreErr, pushedSources)
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
		targets := make([]syncTarget, 0, len(cfg.Sources))
		var skippedLocked []string
		for _, s := range cfg.SourcesSorted() {
			if s.Locked() {
				skippedLocked = append(skippedLocked, ":"+s.Name)
				continue
			}
			mirror := s.SortedMirrors()[0]
			targets = append(targets, syncTarget{LocalPath: mirror.LocalPath, Mirror: s.WithMirror(mirror)})
		}
		return targets, skippedLocked, nil
	}

	targets := make([]syncTarget, 0, len(localPaths))
	seen := map[string]struct{}{}
	for _, rawPath := range localPaths {
		selection, err := resolveSourceSelection(repo, cfg, rawPath, false)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := seen[selection.Source.Name]; ok {
			continue
		}
		seen[selection.Source.Name] = struct{}{}
		mirror := selection.Mirrors[0]
		targets = append(targets, syncTarget{LocalPath: mirror.LocalPath, Mirror: selection.Source.WithMirror(mirror)})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Mirror.Name < targets[j].Mirror.Name })
	return targets, nil, nil
}

func (h SyncHandler) prepareSyncAutostash(ctx context.Context, repo RepoContext, git syncAutostashGit, targets []syncTarget, autostashEnabled bool) (syncAutostash, bool, error) {
	paths := make([]string, 0, len(targets))
	for _, target := range targets {
		for _, path := range target.Mirror.LocalPaths() {
			if mirrorOverlapsConfig(path) {
				return syncAutostash{}, false, fmt.Errorf("mirror path %q overlaps %s", path, config.FileName)
			}
			paths = append(paths, path)
		}
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
			if err := ensureScopedCleanAllowIgnored(ctx, git, path); err != nil {
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

func (h SyncHandler) withFetchedMirrorForPlanning(ctx context.Context, git PushGit, cache CacheConfig, m source.SourceMirror, keep bool, verbose bool, progress progressReporter, trace io.Writer, fn func() error) (err error) {
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m, verbose, progress, trace); err != nil {
			return err
		}
	}
	previousURL, previousExists, err := git.RemoteURL(ctx, m.Remote())
	if err != nil {
		return err
	}
	var previousConfig gitexec.RemoteConfigSnapshot
	if previousExists {
		if exact, ok := git.(exactRemoteConfigGit); ok {
			previousConfig, err = exact.SnapshotRemoteConfig(ctx, m.Remote())
			if err != nil {
				return err
			}
		}
	}
	if err := configureMirrorRemote(ctx, git, m, true, cache); err != nil {
		return err
	}
	defer func() {
		if keep && err == nil {
			return
		}
		if _, ok, inspectErr := git.RemoteURL(ctx, m.Remote()); inspectErr == nil && ok {
			if removeErr := git.RemoteRemove(ctx, m.Remote()); removeErr != nil && err == nil {
				err = removeErr
			}
		} else if inspectErr != nil && err == nil {
			err = inspectErr
		}
		if err != nil && previousExists {
			if exact, ok := git.(exactRemoteConfigGit); ok && previousConfig != nil {
				if restoreErr := exact.RestoreRemoteConfig(ctx, m.Remote(), previousConfig); restoreErr != nil {
					err = fmt.Errorf("%w; failed to restore existing remote: %w", err, restoreErr)
				}
			} else if restoreErr := git.RemoteAdd(ctx, m.Remote(), previousURL); restoreErr != nil {
				err = fmt.Errorf("%w; failed to restore existing remote: %w", err, restoreErr)
			}
		}
	}()
	if err := fetchMirror(ctx, git, cache, m, progress); err != nil {
		return err
	}
	return fn()
}

func (h SyncHandler) runPushPlan(ctx context.Context, repo RepoContext, git PushGit, plan syncPushPlan, inv cli.Invocation, stdout, stderr io.Writer) ([]string, error) {
	push := PushHandler(h)
	var completed []string
	messageGeneration := configuredPushMessageGeneration()
	var err error
	if len(plan.Actions) > 0 {
		messageGeneration, err = resolvePushMessageGeneration(ctx, git, messageGeneration)
		if err != nil {
			return nil, err
		}
	}
	for _, action := range plan.Actions {
		result, err := push.push(ctx, repo, git, action.Target.Mirror, action.Target.Mirror.Branch(), inv.Sync.Keep, "", messageGeneration, inv.Global, stdout, stderr)
		if err != nil {
			if result.Status == pushStatusPushed {
				completed = append(completed, ":"+action.Target.Mirror.Name)
			}
			return completed, syncPushFailure(err, completed)
		}
		if result.Status == pushStatusNotUpToDate {
			return completed, syncPushFailure(syncNotUpToDateError(action.Target.LocalPath), completed)
		}
		completed = append(completed, ":"+action.Target.Mirror.Name)
	}
	return completed, nil
}

func syncPushFailure(cause error, completed []string) error {
	if len(completed) == 0 {
		return cause
	}
	return fmt.Errorf("%w; sync already pushed %s and cannot roll those upstream updates back; after resolving the failure, run braid sync %s", cause, strings.Join(completed, ", "), strings.Join(completed, " "))
}

func syncPostPushPullFailure(cause error, completed []string) error {
	commands := make([]string, 0, len(completed))
	for _, name := range completed {
		commands = append(commands, "braid pull "+name)
	}
	return fmt.Errorf("%w; sync already pushed %s and cannot roll those upstream updates back; after resolving the failure, run %s", cause, strings.Join(completed, ", "), strings.Join(commands, " and "))
}

func committedLocalMirrorChange(ctx context.Context, git PushGit, m source.SourceMirror) (bool, string, error) {
	baseRevision, err := git.RevParse(ctx, m.Revision+"^{commit}")
	if err != nil {
		return false, "", err
	}
	for _, mirror := range m.Mirrors {
		if _, err := git.LsTreeItem(ctx, "HEAD", mirror.LocalPath); err != nil {
			return false, "", err
		}
	}
	newTree, err := reconstructUpstreamTree(ctx, git, m)
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

func syncMirrorPathDeletedError(localPath string) error {
	return fmt.Errorf("sync cannot push deletion of mirror path %s; restore the mirror path, commit, then rerun braid sync", localPath)
}
