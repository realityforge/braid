package command

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"braid/internal/config"
	"braid/internal/gitexec"
	"braid/internal/source"
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

func buildPushProvenance(ctx context.Context, git PushGit, m source.SourceMirror) (pushProvenance, bool, error) {
	byHash := map[string]pushProvenanceCommit{}
	noAnchor := false
	for _, mirror := range m.SortedMirrors() {
		one, ok, err := buildPushProvenanceOne(ctx, git, m.WithMirror(mirror))
		if err != nil {
			return pushProvenance{}, false, err
		}
		if !ok {
			continue
		}
		noAnchor = noAnchor || one.NoCleanAnchor
		for _, commit := range one.Commits {
			byHash[commit.Hash] = commit
		}
	}
	if len(byHash) == 0 {
		return pushProvenance{}, false, nil
	}
	order, err := git.FirstParentCommits(ctx, "HEAD")
	if err != nil {
		return pushProvenance{}, false, err
	}
	rank := map[string]int{}
	for i, hash := range order {
		rank[hash] = i
	}
	commits := make([]pushProvenanceCommit, 0, len(byHash))
	for _, commit := range byHash {
		commits = append(commits, commit)
	}
	sort.Slice(commits, func(i, j int) bool { return rank[commits[i].Hash] > rank[commits[j].Hash] })
	omitted := 0
	if len(commits) > pushProvenanceCommitLimit {
		omitted = len(commits) - pushProvenanceCommitLimit
		commits = commits[omitted:]
	}
	return pushProvenance{Commits: commits, Omitted: omitted, NoCleanAnchor: noAnchor}, true, nil
}

func buildPushProvenanceOne(ctx context.Context, git PushGit, m source.SourceMirror) (pushProvenance, bool, error) {
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

func buildPushProvenanceTemplateFromRaw(ctx context.Context, git PushGit, m source.SourceMirror, provenance pushProvenance) (pushProvenanceTemplate, bool, error) {
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

func findPushProvenanceWindow(ctx context.Context, git PushGit, current source.SourceMirror) (pushProvenanceWindow, error) {
	commits, err := git.FirstParentCommits(ctx, "HEAD")
	if err != nil {
		return pushProvenanceWindow{}, err
	}
	for _, commit := range commits {
		historical, ok, err := mirrorAtCommit(ctx, git, commit, current.LocalPath)
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

func mirrorCleanAtCommit(ctx context.Context, git PushGit, commit string, m source.SourceMirror) (bool, error) {
	downstreamItem, err := git.LsTreeItem(ctx, commit, m.LocalPath)
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

func recordedMirrorItem(ctx context.Context, git PushGit, m source.SourceMirror) (gitexec.TreeItem, error) {
	if m.UpstreamPath == "" {
		return git.TreeItem(ctx, m.Revision)
	}
	return git.LsTreeItem(ctx, m.Revision, m.UpstreamPath)
}

func collectPushProvenanceCommits(ctx context.Context, git PushGit, current source.SourceMirror, revisionRange string) ([]pushProvenanceCommit, int, error) {
	candidates, err := git.LogCommitsTouchingPath(ctx, revisionRange, current.LocalPath)
	if err != nil {
		return nil, 0, err
	}
	eligible := make([]pushProvenanceCommit, 0, len(candidates))
	for _, candidate := range candidates {
		historical, ok, err := mirrorAtCommit(ctx, git, candidate.Hash, current.LocalPath)
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
	return eligible, 0, nil
}

func mirrorAtCommit(ctx context.Context, git PushGit, commit, localPath string) (source.SourceMirror, bool, error) {
	data, ok, err := git.ShowFile(ctx, commit, config.FileName)
	if err != nil {
		return source.SourceMirror{}, false, err
	}
	if !ok {
		return source.SourceMirror{}, false, nil
	}
	cfg, err := config.Parse(data)
	if err != nil {
		if strings.Contains(err.Error(), "requires upgrade") {
			return source.SourceMirror{}, false, nil
		}
		return source.SourceMirror{}, false, fmt.Errorf("parse %s at %s: %w", config.FileName, commit, err)
	}
	s, m, ok := cfg.MirrorByLocalPath(localPath)
	if !ok {
		return source.SourceMirror{}, false, nil
	}
	return s.WithMirror(m), true, nil
}

func samePushProvenanceIdentity(current, historical source.SourceMirror) bool {
	return current.LocalPath == historical.LocalPath &&
		current.Name == historical.Name && source.URLIdentity(current.URL) == source.URLIdentity(historical.URL) && current.TrackingIdentity() == historical.TrackingIdentity() && current.UpstreamPath == historical.UpstreamPath
}

func sameTreeItem(a, b gitexec.TreeItem) bool {
	return a.Mode == b.Mode && a.Type == b.Type && a.Hash == b.Hash
}

func isBraidAutomaticMirrorCommit(subject string) bool {
	for _, prefix := range []string{
		"Braid: Add mirror ",
		"Braid: Update mirror ",
		"Braid: Remove mirror ",
		"Braid: Add source ",
		"Braid: Update source ",
		"Braid: Remove source ",
		"Braid: Add mirrors to source ",
	} {
		if strings.HasPrefix(subject, prefix) {
			return true
		}
	}
	return false
}

func formatPushProvenanceTemplate(m source.SourceMirror, commits []pushProvenanceCommit, omitted int, noCleanAnchor bool, commentChar string) string {
	lines := []string{
		fmt.Sprintf("Braid downstream mirror commit guidance for %s", m.LocalPath),
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
	for i, commit := range commits {
		lines = append(lines, "Commit "+commit.Hash)
		lines = append(lines, strings.Split(commit.Message, "\n")...)
		if i < len(commits)-1 {
			lines = append(lines, "")
		}
	}

	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(commentChar)
		b.WriteByte(' ')
		b.WriteString(line)
	}
	return b.String()
}

func omittedPushProvenanceMessage(omitted int) string {
	if omitted == 1 {
		return "1 older eligible downstream commit omitted."
	}
	return fmt.Sprintf("%d older eligible downstream commits omitted.", omitted)
}
