package command

import (
	"context"
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

	for _, localPath := range cfg.Paths() {
		m := cfg.Mirrors[localPath]
		if m.Locked() {
			continue
		}
		if err := ensureClean(ctx, git); err != nil {
			return err
		}
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

	mergeOut, mergeErr := git.MergeTrees(ctx, map[string]string{
		"GITHEAD_" + localHash:  "HEAD",
		"GITHEAD_" + remoteTree: newRevision,
	}, baseTree, localHash, remoteTree)

	m.Revision = newRevision
	if err := cfg.Update(m); err != nil {
		_ = cleanupRemote()
		return err
	}
	if err := cfg.WriteFile(filepath.Join(configRoot(h.Options), config.FileName)); err != nil {
		_ = cleanupRemote()
		return err
	}
	if err := git.Add(ctx, config.FileName); err != nil {
		_ = cleanupRemote()
		return err
	}

	subject := updateCommitSubject(m)
	if mergeErr != nil {
		if _, err := io.WriteString(stdout, mergeOut); err != nil {
			return err
		}
		return h.writeMergeMessage(ctx, git, subject)
	}

	if _, err := git.CommitMessage(ctx, subject); err != nil {
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

func itemAtRevision(ctx context.Context, git DiffGit, m mirror.Mirror, revision string) (gitexec.TreeItem, error) {
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
