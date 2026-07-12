package command

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"braid/internal/config"
	"braid/internal/source"
)

func resolveSourceSelection(repo RepoContext, cfg config.Config, raw string, pathScoped bool) (source.SourceSelection, error) {
	if strings.HasPrefix(raw, ":") {
		s, err := cfg.SourceByNameRequired(strings.TrimPrefix(raw, ":"))
		if err != nil {
			return source.SourceSelection{}, err
		}
		return source.SourceSelection{Source: s, Mirrors: s.SortedMirrors()}, nil
	}
	localPath, err := normalizeLocalPath(repo, raw)
	if err != nil {
		return source.SourceSelection{}, err
	}
	s, m, err := cfg.MirrorByLocalPathRequired(localPath)
	if err != nil {
		return source.SourceSelection{}, err
	}
	if pathScoped {
		return source.SourceSelection{Source: s, Mirrors: []source.Mirror{m}}, nil
	}
	return source.SourceSelection{Source: s, Mirrors: s.SortedMirrors()}, nil
}

func normalizeLocalPath(repo RepoContext, value string) (string, error) {
	value = strings.ReplaceAll(value, `\`, "/")
	if value == "" {
		return "", errors.New("local path cannot be empty")
	}
	if localPathIsAbs(value) {
		return normalizeAbsoluteLocalPath(repo, value)
	}
	return normalizeRelativeLocalPath(repo, value)
}

func normalizeRelativeLocalPath(repo RepoContext, value string) (string, error) {
	normalized := path.Clean(path.Join(repo.WorkTreePrefix, value))
	if normalized == "." || normalized == "" {
		return "", fmt.Errorf("local path %q resolves to the git worktree root", value)
	}
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("local path %q escapes the git worktree", value)
	}
	return normalized, nil
}

func normalizeAbsoluteLocalPath(repo RepoContext, value string) (string, error) {
	nativePath := filepath.Clean(filepath.FromSlash(value))
	for _, root := range []string{repo.LogicalWorkTreeRoot, repo.GitWorkTreeRoot} {
		if root == "" {
			continue
		}
		relative, ok := relativePathUnder(root, nativePath)
		if !ok {
			continue
		}
		normalized := path.Clean(filepath.ToSlash(relative))
		if normalized == "." || normalized == "" {
			return "", fmt.Errorf("local path %q resolves to the git worktree root", value)
		}
		return normalized, nil
	}
	return "", fmt.Errorf("local path %q is outside the git worktree", value)
}

func localPathIsAbs(value string) bool {
	return path.IsAbs(value) || filepath.IsAbs(filepath.FromSlash(value))
}

func relativePathUnder(root, target string) (string, bool) {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return "", false
	}
	if relative == "." {
		return relative, true
	}
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}

func gitRepoOSPath(gitPath, repoWorkDir string) (string, error) {
	if gitPath == "" {
		return "", errors.New("could not resolve git repository path")
	}
	nativePath := filepath.FromSlash(strings.ReplaceAll(gitPath, `\`, "/"))
	if !filepath.IsAbs(nativePath) {
		nativePath = filepath.Join(repoWorkDir, nativePath)
	}
	return filepath.Abs(nativePath)
}
