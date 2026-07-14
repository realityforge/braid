package command

import (
	"context"
	"fmt"
	"io"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/source"
)

type StatusHandler struct {
	Options Options
}

func (h StatusHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandStatus, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.statusGit(repo, inv, stderr)
	progress := newProgressReporter(stderr, inv.Global.Quiet)
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	cache, err := runtimeCacheForRepo(ctx, repo, inv.Global, inv.Global.Verbose, stderr)
	if err != nil {
		return err
	}

	if inv.Status.LocalPath != "" {
		selection, err := resolveSourceSelection(repo, cfg, inv.Status.LocalPath, true)
		if err != nil {
			return err
		}
		return h.statusSource(ctx, git, cache, selection, inv.Global.Verbose, progress, stdout, stderr)
	}

	for _, s := range cfg.SourcesSorted() {
		selection := source.SourceSelection{Source: s, Mirrors: s.SortedMirrors()}
		if err := h.statusSource(ctx, git, cache, selection, inv.Global.Verbose, progress, stdout, stderr); err != nil {
			return err
		}
	}
	return nil
}

func (h StatusHandler) statusGit(repo RepoContext, inv cli.Invocation, trace io.Writer) StatusGit {
	if git, ok := h.Options.Git.(StatusGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(StatusGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (h StatusHandler) statusSource(ctx context.Context, git StatusGit, cache CacheConfig, selection source.SourceSelection, verbose bool, progress progressReporter, stdout, trace io.Writer) (err error) {
	m := selection.Source.WithMirror(selection.Mirrors[0])
	if cache.Enabled {
		if err := fetchCache(ctx, cache, m, verbose, progress, trace); err != nil {
			return err
		}
	}
	if err := configureMirrorRemote(ctx, git, m, true, cache); err != nil {
		return err
	}
	defer func() {
		removeErr := git.RemoteRemove(ctx, m.Remote())
		if err == nil {
			err = removeErr
		}
	}()

	if err := fetchMirror(ctx, git, cache, m, progress); err != nil {
		return err
	}
	baseRevision, err := git.RevParse(ctx, m.Revision+"^{commit}")
	if err != nil {
		return err
	}
	newRevision, err := resolveAddRevision(ctx, git, m, "")
	if err != nil {
		return err
	}
	for _, mirror := range selection.Mirrors {
		if err := h.statusOne(ctx, git, selection.Source.WithMirror(mirror), baseRevision, newRevision, stdout); err != nil {
			return err
		}
	}
	return nil
}

func (h StatusHandler) statusOne(ctx context.Context, git StatusGit, m source.SourceMirror, baseRevision, newRevision string, stdout io.Writer) error {
	baseItem, basePresent, err := optionalItemAtRevision(ctx, git, m, baseRevision)
	if err != nil {
		return err
	}
	latestItem, latestPresent, err := optionalItemAtRevision(ctx, git, m, newRevision)
	if err != nil {
		return err
	}
	localItem, localErr := git.LsTreeItem(ctx, "HEAD", m.LocalPath)
	localPresent := true
	if gitexec.IsTreeItemNotFound(localErr) {
		localPresent = false
		localErr = nil
	}
	if localErr != nil {
		return localErr
	}
	modified, err := locallyModified(ctx, git, m)
	if err != nil {
		return err
	}
	contentState := mirrorContentState(baseItem, basePresent, localItem, localPresent, latestItem, latestPresent)
	if modified && localPresent {
		if contentState == "Modified Remotely" || contentState == "Removed Remotely" {
			contentState = "Modified Locally And Remotely"
		} else {
			contentState = "Modified Locally"
		}
	}
	sourceState := "Current"
	if m.Locked() {
		sourceState = "Locked"
	} else if newRevision != baseRevision {
		sourceState = "Behind"
	}

	if _, err := fmt.Fprintf(stdout, "%s (%s) [%s]", m.LocalPath, baseRevision, trackingLabel(m)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, " (%s, %s)\n", contentState, sourceState)
	return err
}

func mirrorContentState(base gitexec.TreeItem, basePresent bool, local gitexec.TreeItem, localPresent bool, latest gitexec.TreeItem, latestPresent bool) string {
	equal := func(a gitexec.TreeItem, ap bool, b gitexec.TreeItem, bp bool) bool {
		return ap == bp && (!ap || sameTreeItem(a, b))
	}
	if equal(local, localPresent, latest, latestPresent) {
		return "Up To Date"
	}
	if equal(local, localPresent, base, basePresent) {
		if !latestPresent {
			return "Removed Remotely"
		}
		return "Modified Remotely"
	}
	if equal(latest, latestPresent, base, basePresent) {
		if !localPresent {
			return "Removed Locally"
		}
		return "Modified Locally"
	}
	return "Modified Locally And Remotely"
}

func trackingLabel(m source.SourceMirror) string {
	switch {
	case m.Locked():
		return "REVISION LOCKED"
	case m.Tag() != "":
		return "TAG=" + m.Tag()
	default:
		return "BRANCH=" + m.Branch()
	}
}

func locallyModified(ctx context.Context, git StatusGit, m source.SourceMirror) (bool, error) {
	args, err := buildDiffArgs(ctx, git, m, cli.DiffOptions{})
	if err != nil {
		return false, err
	}
	out, err := git.Diff(ctx, args...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
