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

type scopedCleanGit interface {
	StatusPorcelainPathspecs(context.Context, ...string) (string, error)
	BlockingOperation(context.Context) (string, bool, error)
}

func (h UpdateHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	if err := Preflight(ctx, cli.CommandUpdate, inv, h.Options, stderr); err != nil {
		return err
	}

	git := h.updateGit(inv, stderr)
	cache, err := runtimeCache(inv.Global)
	if err != nil {
		return err
	}

	if inv.Update.LocalPath != "" {
		return h.updateOne(ctx, git, cache, inv.Update.LocalPath, inv.Update, inv.Global.Verbose, stdout, stderr)
	}
	return h.updateAll(ctx, git, cache, inv.Update, inv.Global.Verbose, stdout, stderr)
}

func (h UpdateHandler) updateGit(inv cli.Invocation, trace io.Writer) UpdateGit {
	if git, ok := h.Options.Git.(UpdateGit); ok {
		return git
	}
	return gitexec.New(workDir(h.Options.WorkDir), inv.Global.Verbose, trace)
}

func (h UpdateHandler) updateAll(ctx context.Context, git UpdateGit, cache CacheConfig, options cli.UpdateOptions, verbose bool, stdout, trace io.Writer) error {
	cfg, err := config.Load(configRoot(h.Options))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}

	var targets []string
	for _, localPath := range cfg.Paths() {
		m := cfg.Mirrors[localPath]
		if m.Locked() {
			continue
		}
		targets = append(targets, localPath)
	}
	if err := h.ensureUpdateTargetsClean(ctx, git, cfg, targets); err != nil {
		return err
	}

	for _, localPath := range targets {
		if err := h.updateOne(ctx, git, cache, localPath, options, verbose, stdout, trace); err != nil {
			return fmt.Errorf("update %s: %w", localPath, err)
		}
	}
	return nil
}

func (h UpdateHandler) updateOne(ctx context.Context, git UpdateGit, cache CacheConfig, localPath string, options cli.UpdateOptions, verbose bool, stdout, trace io.Writer) error {
	cfg, err := config.Load(configRoot(h.Options))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	m, err := cfg.GetRequired(localPath)
	if err != nil {
		return err
	}
	original := m
	applyUpdateStrategy(&m, options)
	if err := h.ensureUpdateTargetsClean(ctx, git, cfg, []string{localPath}); err != nil {
		return err
	}

	if cache.Enabled {
		if err := fetchCache(ctx, cache, m.URL, verbose, trace); err != nil {
			return err
		}
	}
	if err := setupOne(ctx, git, m, true, cache); err != nil {
		return err
	}
	cleanupRemote := func() error {
		if options.Keep {
			return nil
		}
		return git.RemoteRemove(ctx, m.Remote())
	}

	if err := fetchMirror(ctx, git, m); err != nil {
		_ = cleanupRemote()
		return err
	}

	baseRevision, err := git.RevParse(ctx, original.Revision+"^{commit}")
	if err != nil {
		_ = cleanupRemote()
		return err
	}
	newRevision, err := resolveUpdateRevision(ctx, git, m, options.Revision)
	if err != nil {
		_ = cleanupRemote()
		return err
	}
	if !updateSwitchesTracking(original, m, options, newRevision) && newRevision == baseRevision {
		return cleanupRemote()
	}

	baseItem, err := baseDiffItem(ctx, git, original)
	if err != nil {
		_ = cleanupRemote()
		return err
	}
	remoteItem, err := itemAtRevision(ctx, git, m, newRevision)
	if err != nil {
		_ = cleanupRemote()
		return err
	}

	localHash, err := git.RevParse(ctx, "HEAD")
	if err != nil {
		_ = cleanupRemote()
		return err
	}
	baseTree, err := git.MakeTreeWithItemIn(ctx, "HEAD", m.Path, baseItem)
	if err != nil {
		_ = cleanupRemote()
		return err
	}
	remoteTree, err := git.MakeTreeWithItemIn(ctx, "HEAD", m.Path, remoteItem)
	if err != nil {
		_ = cleanupRemote()
		return err
	}

	mergedTree, mergeErr := git.MergeTreeWrite(ctx, baseTree, localHash, remoteTree)

	m.Revision = newRevision
	if err := cfg.Update(m); err != nil {
		_ = cleanupRemote()
		return err
	}
	if err := cfg.WriteFile(filepath.Join(configRoot(h.Options), config.FileName)); err != nil {
		_ = cleanupRemote()
		return err
	}
	configItem, err := git.HashFile(ctx, config.FileName)
	if err != nil {
		_ = cleanupRemote()
		return err
	}

	subject := updateCommitSubject(m)
	if mergeErr != nil {
		if _, err := io.WriteString(stdout, mergedTree.Details); err != nil {
			return err
		}
		if mergedTree.Tree != "" {
			if err := git.RestorePathspecsFromTree(ctx, mergedTree.Tree, false, true, m.Path); err != nil {
				_ = cleanupRemote()
				return err
			}
		}
		if err := git.Add(ctx, config.FileName); err != nil {
			_ = cleanupRemote()
			return err
		}
		if err := h.writeConflictInstructions(ctx, git, stdout, m); err != nil {
			_ = cleanupRemote()
			return err
		}
		return h.writeMergeMessage(ctx, git, subject)
	}

	finalTree, err := git.MakeTreeWithItemIn(ctx, mergedTree.Tree, config.FileName, configItem)
	if err != nil {
		_ = cleanupRemote()
		return err
	}
	committed, err := git.CommitTreeWithTemporaryIndex(ctx, finalTree, subject)
	if err != nil {
		_ = cleanupRemote()
		return err
	}
	if !committed {
		_ = cleanupRemote()
		return errors.New("update produced no commit")
	}
	if err := git.RestorePathspecsFromHead(ctx, m.Path, config.FileName); err != nil {
		_ = cleanupRemote()
		return err
	}
	return cleanupRemote()
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

func itemAtRevision(ctx context.Context, git treeItemGit, m mirror.Mirror, revision string) (gitexec.TreeItem, error) {
	if m.RemotePath == "" {
		return gitexec.TreeItem{Type: "tree", Hash: revision}, nil
	}
	return git.LsTreeItem(ctx, revision, m.RemotePath)
}

func updateCommitSubject(m mirror.Mirror) string {
	return fmt.Sprintf("Braid: Update mirror '%s' to '%s'", m.Path, shortRevision(m.Revision))
}

func (h UpdateHandler) writeMergeMessage(ctx context.Context, git UpdateGit, subject string) error {
	mergeMsgPath, err := git.RepoFilePath(ctx, "MERGE_MSG")
	if err != nil {
		return err
	}
	mergeMsgPath, err = gitRepoOSPath(mergeMsgPath, workDir(h.Options.WorkDir))
	if err != nil {
		return err
	}
	return os.WriteFile(mergeMsgPath, []byte(subject+"\n"), 0o644)
}

func (h UpdateHandler) writeConflictInstructions(ctx context.Context, git UpdateGit, stdout io.Writer, m mirror.Mirror) error {
	staged, err := hasUnrelatedStagedEntries(ctx, git, m.Path)
	if err != nil {
		return err
	}
	if staged {
		if _, err := io.WriteString(stdout, "Braid: warning: unrelated staged changes are present; unstage them before the resolution commit if they should not be included.\n"); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stdout, "Braid: conflicts written to %s. Resolve them, then run:\n  git add -- %s %s\n  git commit -F .git/MERGE_MSG\n", m.Path, m.Path, config.FileName)
	return err
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

func (h UpdateHandler) ensureUpdateTargetsClean(ctx context.Context, git UpdateGit, cfg config.Config, localPaths []string) error {
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
	return ensureCommandScopesClean(ctx, git, configRoot(h.Options), true, paths...)
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

func ensureClean(ctx context.Context, git Git) error {
	status, err := git.StatusPorcelain(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("local changes are present")
	}
	return nil
}
