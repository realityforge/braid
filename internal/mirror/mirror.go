package mirror

import (
	"errors"
	"path"
	"regexp"
	"strings"
)

var remoteUnsafeChars = regexp.MustCompile(`[^-A-Za-z0-9]`)

type Options struct {
	LocalPath    string
	Branch       string
	Tag          string
	Revision     string
	RemotePath   string
	PartialClone bool
}

type Mirror struct {
	Path         string
	URL          string
	Branch       string
	RemotePath   string
	Tag          string
	Revision     string
	PartialClone bool
}

func NewFromOptions(url string, options Options) (Mirror, error) {
	if options.Tag != "" && options.Branch != "" {
		return Mirror{}, errors.New("cannot specify both tag and branch")
	}
	if options.Tag != "" && options.Revision != "" {
		return Mirror{}, errors.New("cannot specify both tag and revision")
	}
	if options.PartialClone && options.RemotePath == "" {
		return Mirror{}, errors.New("partial clone requires path")
	}

	cleanURL := strings.TrimRight(url, "/")
	localPath := cleanMirrorPath(options.LocalPath)
	if localPath == "" {
		localPath = defaultLocalPath(cleanURL, options.RemotePath)
	}

	return Mirror{
		Path:         localPath,
		URL:          cleanURL,
		Branch:       options.Branch,
		RemotePath:   strings.TrimRight(options.RemotePath, "/"),
		Tag:          options.Tag,
		Revision:     options.Revision,
		PartialClone: options.PartialClone,
	}, nil
}

func (m Mirror) Locked() bool {
	return m.Branch == "" && m.Tag == ""
}

func (m Mirror) TrackingName() string {
	switch {
	case m.Branch != "":
		return m.Branch
	case m.Tag != "":
		return m.Tag
	default:
		return "revision"
	}
}

func (m Mirror) Remote() string {
	return remoteUnsafeChars.ReplaceAllString(m.TrackingName()+"_braid_"+m.Path, "_")
}

func (m Mirror) LocalRef() string {
	switch {
	case m.Branch != "":
		return m.Remote() + "/" + m.Branch
	case m.Tag != "":
		return "refs/remotes/" + m.Remote() + "/tags/" + m.Tag
	default:
		return m.Revision
	}
}

func (m Mirror) RemoteRef() (string, error) {
	switch {
	case m.Branch != "":
		return "+refs/heads/" + m.Branch, nil
	case m.Tag != "":
		return "+refs/tags/" + m.Tag, nil
	default:
		return "", errors.New("revision-locked mirror has no remote ref")
	}
}

func defaultLocalPath(url, remotePath string) string {
	if remotePath != "" {
		return path.Base(strings.TrimRight(remotePath, "/"))
	}
	trimmed := strings.TrimRight(url, `/\`)
	name := path.Base(strings.ReplaceAll(trimmed, `\`, "/"))
	return strings.TrimSuffix(name, ".git")
}

func cleanMirrorPath(value string) string {
	return strings.TrimRight(strings.ReplaceAll(value, `\`, "/"), "/")
}
