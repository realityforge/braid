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

func (h SyncHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandSync, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.syncGit(repo, inv, stderr)
	processGit := UpdateHandler{Options: h.Options}.processRepoPathGit(repo, inv, stderr)
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
	if err := h.ensureSyncTargetsClean(ctx, repo, git, targets); err != nil {
		return err
	}

	cache, err := runtimeCache(inv.Global)
	if err != nil {
		return err
	}
	if !inv.Sync.PullOnly {
		if err := h.hydrateMissingRecordedRevisions(ctx, git, cache, targets, inv.Sync.Keep, inv.Global.Verbose, stderr); err != nil {
			return err
		}
		plan, err := h.buildPushPlan(ctx, git, cache, targets, inv.Sync.Keep, inv.Global.Verbose, stderr)
		if err != nil {
			return err
		}
		if err := h.runPushPlan(ctx, repo, git, plan, inv, stdout, stderr); err != nil {
			return err
		}
	}

	update := UpdateHandler{Options: h.Options}
	updateOptions := cli.UpdateOptions{Keep: inv.Sync.Keep}
	for _, target := range targets {
		if err := update.updateOne(ctx, repo, git, processGit, cache, target.LocalPath, updateOptions, inv.Global.Verbose, stdout, stderr); err != nil {
			return fmt.Errorf("update %s: %w", target.LocalPath, err)
		}
	}
	return writeSkippedLockedMirrors(stdout, skippedLocked)
}

func (h SyncHandler) syncGit(repo RepoContext, inv cli.Invocation, trace io.Writer) PushGit {
	if git, ok := h.Options.Git.(PushGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(PushGit); ok {
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

func (h SyncHandler) ensureSyncTargetsClean(ctx context.Context, repo RepoContext, git scopedCleanGit, targets []syncTarget) error {
	paths := make([]string, 0, len(targets))
	for _, target := range targets {
		if mirrorOverlapsConfig(target.Mirror.Path) {
			return fmt.Errorf("mirror path %q overlaps %s", target.Mirror.Path, config.FileName)
		}
		paths = append(paths, target.Mirror.Path)
	}
	return ensureCommandScopesClean(ctx, git, configRoot(h.Options, repo), true, paths...)
}

func (h SyncHandler) hydrateMissingRecordedRevisions(ctx context.Context, git PushGit, cache CacheConfig, targets []syncTarget, keep bool, verbose bool, trace io.Writer) error {
	for _, target := range targets {
		if _, err := git.RevParse(ctx, target.Mirror.Revision+"^{commit}"); err == nil {
			continue
		}
		err := h.withFetchedMirrorForPlanning(ctx, git, cache, target.Mirror, keep, verbose, trace, func() error {
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

func (h SyncHandler) buildPushPlan(ctx context.Context, git PushGit, cache CacheConfig, targets []syncTarget, keep bool, verbose bool, trace io.Writer) (syncPushPlan, error) {
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
		err := h.withFetchedMirrorForPlanning(ctx, git, cache, action.Target.Mirror, keep, verbose, trace, func() error {
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

func (h SyncHandler) withFetchedMirrorForPlanning(ctx context.Context, git PushGit, cache CacheConfig, m mirror.Mirror, keep bool, verbose bool, trace io.Writer, fn func() error) (err error) {
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m.URL, verbose, trace); err != nil {
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
	if err := fetchMirror(ctx, git, m); err != nil {
		return err
	}
	return fn()
}

func (h SyncHandler) runPushPlan(ctx context.Context, repo RepoContext, git PushGit, plan syncPushPlan, inv cli.Invocation, stdout, stderr io.Writer) error {
	push := PushHandler{Options: h.Options}
	for _, action := range plan.Actions {
		result, err := push.push(ctx, repo, git, action.Target.Mirror, action.Target.Mirror.Branch, inv.Sync.Keep, inv.Global, stdout, stderr)
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
	return fmt.Errorf("sync cannot push %s because the upstream branch is not up to date; run braid update %s, resolve conflicts if needed, commit, then rerun braid sync", localPath, localPath)
}

func syncNonBranchLocalChangeError(localPath string) error {
	return fmt.Errorf("sync cannot push committed local changes for non-branch mirror %s; run braid push %s --branch <branch> or rerun braid sync --pull-only %s if you only intended to update", localPath, localPath, localPath)
}

func syncMirrorPathDeletedError(localPath string) error {
	return fmt.Errorf("sync cannot push deletion of mirror path %s; restore the mirror path, commit, then rerun braid sync", localPath)
}
