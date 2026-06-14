package command

import (
	"errors"
	"path/filepath"
	"strings"
)

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
