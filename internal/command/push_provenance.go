package command

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/mirror"
)

const pushProvenanceCommitLimit = 25

type pushProvenanceTemplate struct {
	CommentChar string
	Content     string
}

type pushProvenance struct {
	Commits       []pushProvenanceCommit
	Omitted       int
	NoCleanAnchor bool
}

type pushProvenanceCommit struct {
	Hash    string
	Message string
}

type pushProvenanceWindow struct {
	Range         string
	NoCleanAnchor bool
}

func buildPushProvenanceTemplate(ctx context.Context, git PushGit, m mirror.Mirror) (pushProvenanceTemplate, bool, error) {
	provenance, ok, err := buildPushProvenance(ctx, git, m)
	if err != nil || !ok {
		return pushProvenanceTemplate{}, false, err
	}
	return buildPushProvenanceTemplateFromRaw(ctx, git, m, provenance)
}

func buildPushProvenance(ctx context.Context, git PushGit, m mirror.Mirror) (pushProvenance, bool, error) {
	window, err := findPushProvenanceWindow(ctx, git, m)
	if err != nil {
		return pushProvenance{}, false, err
	}
	commits, omitted, err := collectPushProvenanceCommits(ctx, git, m, window.Range)
	if err != nil {
		return pushProvenance{}, false, err
	}
	if len(commits) == 0 {
		return pushProvenance{}, false, nil
	}
	return pushProvenance{
		Commits:       commits,
		Omitted:       omitted,
		NoCleanAnchor: window.NoCleanAnchor,
	}, true, nil
}

func buildPushProvenanceTemplateFromRaw(ctx context.Context, git PushGit, m mirror.Mirror, provenance pushProvenance) (pushProvenanceTemplate, bool, error) {
	if len(provenance.Commits) == 0 {
		return pushProvenanceTemplate{}, false, nil
	}
	commentChar, err := pushProvenanceCommentChar(ctx, git)
	if err != nil {
		return pushProvenanceTemplate{}, false, err
	}
	return pushProvenanceTemplate{
		CommentChar: commentChar,
		Content:     formatPushProvenanceTemplate(m, provenance.Commits, provenance.Omitted, provenance.NoCleanAnchor, commentChar),
	}, true, nil
}

func pushProvenanceCommentChar(ctx context.Context, git PushGit) (string, error) {
	value, ok, err := git.CoreCommentChar(ctx)
	if err != nil {
		return "", err
	}
	if !ok || value == "" {
		return "#", nil
	}
	if value == "auto" {
		return "", fmt.Errorf("core.commentChar=auto is not supported for push provenance guidance")
	}
	if utf8.RuneCountInString(value) != 1 {
		return "", fmt.Errorf("core.commentChar=%q is not a single character", value)
	}
	return value, nil
}

func findPushProvenanceWindow(ctx context.Context, git PushGit, current mirror.Mirror) (pushProvenanceWindow, error) {
	commits, err := git.FirstParentCommits(ctx, "HEAD")
	if err != nil {
		return pushProvenanceWindow{}, err
	}
	for _, commit := range commits {
		historical, ok, err := mirrorAtCommit(ctx, git, commit, current.Path)
		if err != nil {
			return pushProvenanceWindow{}, err
		}
		if !ok || !samePushProvenanceIdentity(current, historical) {
			return pushProvenanceWindow{Range: commit + "..HEAD"}, nil
		}
		clean, err := mirrorCleanAtCommit(ctx, git, commit, historical)
		if err != nil {
			return pushProvenanceWindow{}, err
		}
		if clean {
			return pushProvenanceWindow{Range: commit + "..HEAD"}, nil
		}
	}
	return pushProvenanceWindow{Range: "HEAD", NoCleanAnchor: true}, nil
}

func mirrorCleanAtCommit(ctx context.Context, git PushGit, commit string, m mirror.Mirror) (bool, error) {
	downstreamItem, err := git.LsTreeItem(ctx, commit, m.Path)
	if err != nil {
		if !isMissingTreeItemError(err) {
			return false, err
		}
		return false, nil
	}
	upstreamItem, err := recordedMirrorItem(ctx, git, m)
	if err != nil {
		return false, err
	}
	return sameTreeItem(downstreamItem, upstreamItem), nil
}

func recordedMirrorItem(ctx context.Context, git PushGit, m mirror.Mirror) (gitexec.TreeItem, error) {
	if m.RemotePath == "" {
		return git.TreeItem(ctx, m.Revision)
	}
	return git.LsTreeItem(ctx, m.Revision, m.RemotePath)
}

func collectPushProvenanceCommits(ctx context.Context, git PushGit, current mirror.Mirror, revisionRange string) ([]pushProvenanceCommit, int, error) {
	candidates, err := git.LogCommitsTouchingPath(ctx, revisionRange, current.Path)
	if err != nil {
		return nil, 0, err
	}
	eligible := make([]pushProvenanceCommit, 0, len(candidates))
	for _, candidate := range candidates {
		historical, ok, err := mirrorAtCommit(ctx, git, candidate.Hash, current.Path)
		if err != nil {
			return nil, 0, err
		}
		if !ok || !samePushProvenanceIdentity(current, historical) || isBraidAutomaticMirrorCommit(candidate.Subject) {
			continue
		}
		eligible = append(eligible, pushProvenanceCommit{
			Hash:    candidate.Hash,
			Message: candidate.Message,
		})
	}
	omitted := 0
	if len(eligible) > pushProvenanceCommitLimit {
		omitted = len(eligible) - pushProvenanceCommitLimit
		eligible = eligible[omitted:]
	}
	return eligible, omitted, nil
}

func mirrorAtCommit(ctx context.Context, git PushGit, commit, localPath string) (mirror.Mirror, bool, error) {
	data, ok, err := git.ShowFile(ctx, commit, config.FileName)
	if err != nil {
		return mirror.Mirror{}, false, err
	}
	if !ok {
		return mirror.Mirror{}, false, nil
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return mirror.Mirror{}, false, fmt.Errorf("parse %s at %s: %w", config.FileName, commit, err)
	}
	m, ok := cfg.Get(localPath)
	return m, ok, nil
}

func samePushProvenanceIdentity(current, historical mirror.Mirror) bool {
	return current.Path == historical.Path &&
		current.URL == historical.URL &&
		current.RemotePath == historical.RemotePath
}

func sameTreeItem(a, b gitexec.TreeItem) bool {
	return a.Mode == b.Mode && a.Type == b.Type && a.Hash == b.Hash
}

func isBraidAutomaticMirrorCommit(subject string) bool {
	for _, prefix := range []string{
		"Braid: Add mirror ",
		"Braid: Update mirror ",
		"Braid: Remove mirror ",
	} {
		if strings.HasPrefix(subject, prefix) {
			return true
		}
	}
	return false
}

func formatPushProvenanceTemplate(m mirror.Mirror, commits []pushProvenanceCommit, omitted int, noCleanAnchor bool, commentChar string) string {
	lines := []string{
		fmt.Sprintf("Braid downstream mirror commit guidance for %s", m.Path),
		"",
	}
	if noCleanAnchor {
		lines = append(lines,
			"No clean mirror anchor was found; showing all reachable commits for the current mirror identity, subject to the normal cap.",
			"",
		)
	}
	if omitted > 0 {
		lines = append(lines, omittedPushProvenanceMessage(omitted), "")
	}
	for _, commit := range commits {
		lines = append(lines, "Commit "+commit.Hash)
		lines = append(lines, strings.Split(commit.Message, "\n")...)
		lines = append(lines, "")
	}

	var b strings.Builder
	for _, line := range lines {
		b.WriteString(commentChar)
		b.WriteByte(' ')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func omittedPushProvenanceMessage(omitted int) string {
	if omitted == 1 {
		return "1 older eligible downstream commit omitted."
	}
	return fmt.Sprintf("%d older eligible downstream commits omitted.", omitted)
}
