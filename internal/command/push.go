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
	"braid/internal/source"
)

type PushHandler struct {
	Options Options
}

type pushStatus int

const (
	pushStatusPushed pushStatus = iota
	pushStatusNoLocalChanges
	pushStatusNotUpToDate
)

type pushRepresentation struct {
	Mirror  source.Mirror
	Item    gitexec.TreeItem
	Present bool
}

func validatePushRepresentations(ctx context.Context, git PushGit, items []pushRepresentation) error {
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			a, b := items[i], items[j]
			switch {
			case a.Mirror.UpstreamPath == b.Mirror.UpstreamPath:
				if a.Present != b.Present || (a.Present && !sameTreeItem(a.Item, b.Item)) {
					return inconsistentPushMirrors(a, b)
				}
			case upstreamContains(a.Mirror.UpstreamPath, b.Mirror.UpstreamPath):
				if ok, err := pushDescendantMatches(ctx, git, a, b); err != nil {
					return err
				} else if !ok {
					return inconsistentPushMirrors(a, b)
				}
			case upstreamContains(b.Mirror.UpstreamPath, a.Mirror.UpstreamPath):
				if ok, err := pushDescendantMatches(ctx, git, b, a); err != nil {
					return err
				} else if !ok {
					return inconsistentPushMirrors(a, b)
				}
			}
		}
	}
	return nil
}
func upstreamContains(ancestor, descendant string) bool {
	if ancestor == "" {
		return descendant != ""
	}
	return strings.HasPrefix(descendant, ancestor+"/")
}
func pushDescendantMatches(ctx context.Context, git PushGit, ancestor, descendant pushRepresentation) (bool, error) {
	if !ancestor.Present {
		return !descendant.Present, nil
	}
	if ancestor.Item.Type != "tree" {
		return false, nil
	}
	relative := descendant.Mirror.UpstreamPath
	if ancestor.Mirror.UpstreamPath != "" {
		relative = strings.TrimPrefix(relative, ancestor.Mirror.UpstreamPath+"/")
	}
	nested, err := git.LsTreeItem(ctx, ancestor.Item.Hash, relative)
	nestedPresent := true
	if gitexec.IsTreeItemNotFound(err) {
		nestedPresent = false
		err = nil
	}
	if err != nil {
		return false, err
	}
	return nestedPresent == descendant.Present && (!nestedPresent || sameTreeItem(nested, descendant.Item)), nil
}
func inconsistentPushMirrors(a, b pushRepresentation) error {
	return fmt.Errorf("source mirrors %s -> %q and %s -> %q represent inconsistent upstream content", a.Mirror.LocalPath, a.Mirror.UpstreamPath, b.Mirror.LocalPath, b.Mirror.UpstreamPath)
}
func outermostPushRepresentation(items []pushRepresentation, index int) bool {
	candidate := items[index]
	for i, other := range items {
		if i == index {
			continue
		}
		if other.Mirror.UpstreamPath == candidate.Mirror.UpstreamPath && other.Mirror.LocalPath < candidate.Mirror.LocalPath {
			return false
		}
		if upstreamContains(other.Mirror.UpstreamPath, candidate.Mirror.UpstreamPath) {
			return false
		}
	}
	return true
}

type pushResult struct {
	Status pushStatus
}

func (h PushHandler) Run(inv cli.Invocation, stdout, stderr io.Writer) error {
	ctx := context.Background()
	repo, err := Preflight(ctx, cli.CommandPush, inv, h.Options, stderr)
	if err != nil {
		return err
	}

	git := h.pushGit(repo, inv, stderr)
	cfg, err := config.Load(configRoot(h.Options, repo))
	if err != nil {
		return err
	}
	if err := validateConfigPaths(cfg); err != nil {
		return err
	}
	selection, err := resolveSourceSelection(repo, cfg, inv.Push.LocalPath, false)
	if err != nil {
		return err
	}
	result, err := h.push(ctx, repo, git, selection.Source.WithMirror(selection.Mirrors[0]), inv.Push.Branch, inv.Push.Keep, inv.Push.Message, inv.Global, stdout, stderr)
	if err != nil {
		return err
	}
	switch result.Status {
	case pushStatusNotUpToDate:
		_, err = fmt.Fprintln(stdout, "Braid: Source is not up to date. Stopping.")
	case pushStatusNoLocalChanges:
		_, err = fmt.Fprintln(stdout, "Braid: No local changes found in downstream HEAD. Stopping.")
	}
	return err
}

func (h PushHandler) pushGit(repo RepoContext, inv cli.Invocation, trace io.Writer) PushGit {
	if git, ok := h.Options.Git.(PushGit); ok {
		return git
	}
	if git, ok := repo.rootGit(inv, h.Options, trace).(PushGit); ok {
		return git
	}
	return gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (h PushHandler) push(ctx context.Context, repo RepoContext, git PushGit, m source.SourceMirror, branch string, keep bool, commitMessage string, global cli.GlobalOptions, stdout, stderr io.Writer) (result pushResult, err error) {
	if branch == "" {
		branch = m.Branch()
	}
	if branch == "" {
		return pushResult{}, fmt.Errorf("mirror has no tracked branch; specify --branch to push %s", m.LocalPath)
	}
	ls, ok := git.(interface {
		LsRemote(context.Context, ...string) (string, error)
	})
	if !ok {
		return pushResult{}, errors.New("git implementation cannot inspect push destination")
	}
	out, lsErr := ls.LsRemote(ctx, m.URL, "refs/heads/"+branch)
	if lsErr != nil {
		return pushResult{}, lsErr
	}
	expectedOld := ""
	fields := strings.Fields(out)
	if len(fields) > 0 {
		expectedOld = fields[0]
	}
	progress := newProgressReporter(stderr, global.Quiet)

	cache, err := runtimeCacheForRepo(ctx, repo, global, global.Verbose, stderr)
	if err != nil {
		return pushResult{}, err
	}
	if cache.Enabled {
		cacheMirror := m
		if expectedOld == "" {
			cacheMirror.Tracking = source.RevisionTracking{}
		}
		if err := fetchCache(ctx, cache, cacheMirror, global.Verbose, progress, stderr); err != nil {
			return pushResult{}, err
		}
	}
	previousRemoteURL, previousRemoteExists, err := git.RemoteURL(ctx, m.Remote())
	if err != nil {
		return pushResult{}, err
	}
	var previousRemoteConfig gitexec.RemoteConfigSnapshot
	if previousRemoteExists {
		if exact, ok := git.(exactRemoteConfigGit); ok {
			previousRemoteConfig, err = exact.SnapshotRemoteConfig(ctx, m.Remote())
			if err != nil {
				return pushResult{}, err
			}
		}
	}
	if err := configureMirrorRemote(ctx, git, m, true, cache); err != nil {
		return pushResult{}, err
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
		if err != nil && previousRemoteExists {
			var restoreErr error
			if exact, ok := git.(exactRemoteConfigGit); ok && previousRemoteConfig != nil {
				restoreErr = exact.RestoreRemoteConfig(ctx, m.Remote(), previousRemoteConfig)
			} else {
				restoreErr = git.RemoteAdd(ctx, m.Remote(), previousRemoteURL)
			}
			if restoreErr != nil {
				err = fmt.Errorf("%w; failed to restore existing remote: %w", err, restoreErr)
			}
		}
	}()
	if err := fetchMirror(ctx, git, cache, m, progress); err != nil {
		return pushResult{}, err
	}

	baseRevision, err := git.RevParse(ctx, m.Revision+"^{commit}")
	if err != nil {
		return pushResult{}, err
	}
	if expectedOld != "" && expectedOld != baseRevision {
		return pushResult{Status: pushStatusNotUpToDate}, nil
	}

	newTree, err := reconstructUpstreamTree(ctx, git, m)
	if err != nil {
		return pushResult{}, err
	}
	baseTree, err := git.RevParse(ctx, baseRevision+"^{tree}")
	if err != nil {
		return pushResult{}, err
	}
	if newTree == baseTree {
		return pushResult{Status: pushStatusNoLocalChanges}, nil
	}

	var provenance pushProvenance
	var provenanceOK bool
	var provenanceErr error
	messageGeneration := pushMessageGeneration{}
	if commitMessage == "" {
		provenance, provenanceOK, provenanceErr = buildPushProvenance(ctx, git, m)
		if provenanceErr != nil {
			warnPushProvenance(stderr, provenanceErr)
		}
		messageGeneration = configuredPushMessageGeneration()
	}

	pushCompleted := false
	if err := runProgressWithOperation(
		progress,
		fmt.Sprintf("Braid: pushing source :%s", m.Name),
		fmt.Sprintf("Braid: pushed source :%s", m.Name),
		func(pushProgress *progressOperation) error {
			info := stderr
			commitStdout := stdout
			if global.Quiet {
				info = io.Discard
				commitStdout = io.Discard
			}
			pushErr := h.pushViaTempRepo(ctx, repo, git, m, branch, baseRevision, expectedOld, newTree, commitMessage, global.Verbose, h.stdin(), commitStdout, stderr, info, pushProgress, provenance, provenanceOK, provenanceErr, messageGeneration)
			pushCompleted = pushErr == nil
			return pushErr
		},
	); err != nil {
		if pushCompleted {
			return pushResult{Status: pushStatusPushed}, err
		}
		return pushResult{}, err
	}
	return pushResult{Status: pushStatusPushed}, nil
}

func reconstructUpstreamTree(ctx context.Context, git PushGit, m source.SourceMirror) (string, error) {
	representations := make([]pushRepresentation, 0, len(m.Mirrors))
	for _, mirror := range m.SortedMirrors() {
		if contains, containsErr := git.TreeContainsGitlink(ctx, "HEAD", mirror.LocalPath); containsErr != nil {
			return "", containsErr
		} else if contains {
			return "", fmt.Errorf("mirror %s contains an unsupported gitlink", mirror.LocalPath)
		}
		item, itemErr := git.LsTreeItem(ctx, "HEAD", mirror.LocalPath)
		present := true
		if gitexec.IsTreeItemNotFound(itemErr) {
			present = false
			itemErr = nil
		}
		if itemErr != nil {
			return "", itemErr
		}
		if present && (item.Type == "commit" || item.Mode == "160000") {
			return "", fmt.Errorf("mirror %s contains an unsupported gitlink", mirror.LocalPath)
		}
		representations = append(representations, pushRepresentation{Mirror: mirror, Item: item, Present: present})
	}
	if err := validatePushRepresentations(ctx, git, representations); err != nil {
		return "", err
	}
	newTree := m.Revision
	var err error
	for i, representation := range representations {
		if !outermostPushRepresentation(representations, i) {
			continue
		}
		upstream := representation.Mirror.UpstreamPath
		if upstream == "" {
			if representation.Present {
				if representation.Item.Type != "tree" {
					return "", fmt.Errorf("root mirror %s is not a tree", representation.Mirror.LocalPath)
				}
				newTree = representation.Item.Hash
			} else {
				empty, ok := git.(interface {
					EmptyTree(context.Context) (string, error)
				})
				if !ok {
					return "", errors.New("git implementation cannot create empty tree")
				}
				newTree, err = empty.EmptyTree(ctx)
			}
		} else if representation.Present {
			newTree, err = git.MakeTreeWithItemIn(ctx, newTree, upstream, representation.Item)
		} else {
			remover, ok := git.(interface {
				MakeTreeWithoutPath(context.Context, string, string) (string, error)
			})
			if !ok {
				return "", errors.New("git implementation cannot remove upstream paths")
			}
			newTree, err = remover.MakeTreeWithoutPath(ctx, newTree, upstream)
		}
		if err != nil {
			return "", err
		}
	}
	return newTree, nil
}

func (h PushHandler) pushViaTempRepo(ctx context.Context, repo RepoContext, source PushGit, m source.SourceMirror, branch, baseRevision, expectedOld, newTree, commitMessage string, verbose bool, stdin io.Reader, stdout, stderr, info io.Writer, pushProgress *progressOperation, provenance pushProvenance, provenanceOK bool, provenanceErr error, messageGeneration pushMessageGeneration) error {
	workspaceDir, err := os.MkdirTemp("", "braid-push")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(workspaceDir)
	}()

	pushRepoDir := filepath.Join(workspaceDir, "push-repo")
	contextDir := filepath.Join(workspaceDir, "context")
	if err := os.Mkdir(pushRepoDir, 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(contextDir, 0o755); err != nil {
		return err
	}

	tempGit := gitexec.New(pushRepoDir, verbose, info)
	if err := tempGit.Init(ctx); err != nil {
		return err
	}
	// Push is assembled in an isolated repository so the user's worktree and index stay untouched.
	if err := copyLocalGitConfig(ctx, source, tempGit); err != nil {
		return err
	}
	// Alternates let the temporary repository reuse already-fetched objects without copying packs.
	if err := writeAlternates(ctx, source, pushRepoDir, repo.GitWorkTreeRoot); err != nil {
		return err
	}
	if err := tempGit.UpdateRef(ctx, "--no-deref", "HEAD", baseRevision); err != nil {
		return err
	}
	if err := tempGit.ConfigSet(ctx, "--local", "core.sparsecheckout", "true"); err != nil {
		return err
	}
	// An empty sparse-checkout avoids checking out files outside the mirror and running their filters.
	if err := os.WriteFile(filepath.Join(pushRepoDir, ".git", "info", "sparse-checkout"), nil, 0o644); err != nil {
		return err
	}
	if err := tempGit.ReadTreeUpdateMerge(ctx, newTree); err != nil {
		return err
	}

	if commitMessage != "" {
		if err := verifyTempPushIndex(ctx, tempGit, newTree); err != nil {
			return err
		}
		committed, err := tempGit.CommitMessage(ctx, commitMessage)
		if err != nil {
			return err
		}
		if !committed {
			return fmt.Errorf("temporary push commit did not create a commit")
		}
	} else if messageGeneration.Enabled {
		messageInfo := newProgressSeparatedWriter(pushProgress, info)
		seedPath, err := preparePushMessageSeed(ctx, repo, source, tempGit, m, branch, baseRevision, newTree, contextDir, messageGeneration, verbose, messageInfo, provenance, provenanceOK, provenanceErr)
		if err != nil {
			return err
		}
		if err := verifyTempPushIndex(ctx, tempGit, newTree); err != nil {
			return err
		}
		pushProgress.pause()
		if err := tempGit.CommitVerboseMessageFile(ctx, seedPath, stdin, stdout, stderr); err != nil {
			return err
		}
		pushProgress.resume()
	} else {
		var provenanceTemplate pushProvenanceTemplate
		if provenanceOK {
			if template, ok, err := buildPushProvenanceTemplateFromRaw(ctx, source, m, provenance); err != nil {
				warnPushProvenance(stderr, err)
			} else if ok {
				provenanceTemplate = template
			}
		}
		templatePath := preparePushProvenanceTemplate(ctx, tempGit, contextDir, provenanceTemplate, stderr)
		if err := verifyTempPushIndex(ctx, tempGit, newTree); err != nil {
			return err
		}
		if templatePath != "" {
			pushProgress.pause()
			if err := tempGit.CommitVerboseMessageFile(ctx, templatePath, stdin, stdout, stderr); err != nil {
				return err
			}
			pushProgress.resume()
		} else {
			pushProgress.pause()
			if err := tempGit.CommitVerbose(ctx, stdin, stdout, stderr); err != nil {
				return err
			}
			pushProgress.resume()
		}
	}
	lease := "--force-with-lease=refs/heads/" + branch + ":" + expectedOld
	return tempGit.Push(ctx, lease, m.URL, "HEAD:refs/heads/"+branch)
}

func (h PushHandler) stdin() io.Reader {
	if h.Options.Stdin != nil {
		return h.Options.Stdin
	}
	return os.Stdin
}

func copyLocalGitConfig(ctx context.Context, source PushGit, target gitexec.Git) error {
	for _, key := range []string{"user.name", "user.email", "commit.gpgsign"} {
		value, ok, err := source.ConfigGet(ctx, "--local", "--get", key)
		if err != nil {
			return err
		}
		if ok {
			if err := target.ConfigSet(ctx, "--local", key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func preparePushProvenanceTemplate(ctx context.Context, tempGit gitexec.Git, tempDir string, template pushProvenanceTemplate, stderr io.Writer) string {
	if template.Content == "" {
		return ""
	}
	if err := tempGit.ConfigSet(ctx, "--local", "core.commentChar", template.CommentChar); err != nil {
		warnPushProvenance(stderr, fmt.Errorf("set temporary core.commentChar: %w", err))
		return ""
	}
	templatePath := filepath.Join(tempDir, "BRAID_COMMIT_TEMPLATE")
	if err := os.WriteFile(templatePath, []byte(template.Content), 0o644); err != nil {
		warnPushProvenance(stderr, fmt.Errorf("write temporary commit template: %w", err))
		return ""
	}
	return templatePath
}

func warnPushProvenance(stderr io.Writer, err error) {
	_, _ = fmt.Fprintf(stderr, "Braid: warning: push provenance guidance skipped: %v\n", err)
}

func writeAlternates(ctx context.Context, source PushGit, tempDir, sourceWorkDir string) error {
	objectsPath, err := source.RepoFilePath(ctx, "objects")
	if err != nil {
		return err
	}
	objectsPath, err = alternateObjectPath(objectsPath, sourceWorkDir)
	if err != nil {
		return err
	}
	alternates := filepath.Join(tempDir, ".git", "objects", "info", "alternates")
	return os.WriteFile(alternates, []byte(objectsPath+"\n"), 0o644)
}

func alternateObjectPath(objectsPath, sourceWorkDir string) (string, error) {
	absolutePath, err := gitRepoOSPath(objectsPath, sourceWorkDir)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(absolutePath), nil
}
