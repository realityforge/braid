package source

import (
	"errors"
	"path"
	"regexp"
	"sort"
	"strings"
)

var remoteUnsafeChars = regexp.MustCompile(`[^-A-Za-z0-9]`)
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Tracking interface {
	isTracking()
	Name() string
}

type BranchTracking struct{ Branch string }

func (BranchTracking) isTracking()    {}
func (t BranchTracking) Name() string { return t.Branch }

type TagTracking struct{ Tag string }

func (TagTracking) isTracking()    {}
func (t TagTracking) Name() string { return t.Tag }

type RevisionTracking struct{}

func (RevisionTracking) isTracking()  {}
func (RevisionTracking) Name() string { return "revision" }

type Mirror struct {
	LocalPath    string
	UpstreamPath string
}

type Source struct {
	Name         string
	URL          string
	Tracking     Tracking
	Revision     string
	PartialClone bool
	SyncPush     bool
	Mirrors      []Mirror
}

type SourceMirror struct {
	Source
	Mirror
}

type SourceSelection struct {
	Source  Source
	Mirrors []Mirror
}

func (s Source) Locked() bool { _, ok := s.Tracking.(RevisionTracking); return ok }
func (s Source) TrackingName() string {
	if s.Tracking == nil {
		return "revision"
	}
	return s.Tracking.Name()
}
func (s Source) TrackingIdentity() string {
	switch t := s.Tracking.(type) {
	case BranchTracking:
		return "branch:" + t.Branch
	case TagTracking:
		return "tag:" + t.Tag
	default:
		return "revision"
	}
}
func (s Source) Branch() string {
	if t, ok := s.Tracking.(BranchTracking); ok {
		return t.Branch
	}
	return ""
}
func (s Source) Tag() string {
	if t, ok := s.Tracking.(TagTracking); ok {
		return t.Tag
	}
	return ""
}
func (s Source) Remote() string {
	return remoteUnsafeChars.ReplaceAllString(s.TrackingName()+"_braid_"+s.Name, "_")
}
func (s Source) LocalRef() string {
	switch t := s.Tracking.(type) {
	case BranchTracking:
		return s.Remote() + "/" + t.Branch
	case TagTracking:
		return "refs/remotes/" + s.Remote() + "/tags/" + t.Tag
	default:
		return s.Revision
	}
}
func (s Source) RemoteRef() (string, error) {
	switch t := s.Tracking.(type) {
	case BranchTracking:
		return "+refs/heads/" + t.Branch, nil
	case TagTracking:
		return "+refs/tags/" + t.Tag, nil
	default:
		return "", errors.New("revision-locked source has no remote ref")
	}
}
func (s Source) SortedMirrors() []Mirror {
	result := append([]Mirror(nil), s.Mirrors...)
	sort.Slice(result, func(i, j int) bool { return result[i].LocalPath < result[j].LocalPath })
	return result
}
func (s Source) MirrorByLocalPath(localPath string) (Mirror, bool) {
	for _, m := range s.Mirrors {
		if m.LocalPath == localPath {
			return m, true
		}
	}
	return Mirror{}, false
}
func (s Source) LocalPaths() []string {
	result := make([]string, 0, len(s.Mirrors))
	for _, m := range s.SortedMirrors() {
		result = append(result, m.LocalPath)
	}
	return result
}
func (s Source) WithMirror(m Mirror) SourceMirror { return SourceMirror{Source: s, Mirror: m} }
func (m SourceMirror) Remote() string             { return m.Source.Remote() }
func (m SourceMirror) LocalRef() string           { return m.Source.LocalRef() }
func (m SourceMirror) RemoteRef() (string, error) { return m.Source.RemoteRef() }
func (m SourceMirror) Locked() bool               { return m.Source.Locked() }
func (m SourceMirror) TrackingName() string       { return m.Source.TrackingName() }
func (m SourceMirror) TrackingIdentity() string   { return m.Source.TrackingIdentity() }

func CleanURL(value string) string {
	if value == "" || value == "/" || value == `\` || isWindowsRoot(value) || strings.EqualFold(value, "file:///") {
		return value
	}
	trimmed := strings.TrimRight(value, `/\`)
	if trimmed == "" {
		return value
	}
	return trimmed
}
func URLIdentity(value string) string { return CleanURL(value) }
func DerivedName(url string) string {
	cleaned := CleanURL(url)
	name := path.Base(strings.ReplaceAll(cleaned, `\`, "/"))
	return strings.TrimSuffix(name, ".git")
}
func ValidName(name string) bool { return name != "." && name != ".." && namePattern.MatchString(name) }
func isWindowsRoot(value string) bool {
	if len(value) < 3 {
		return false
	}
	if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", rune(value[0])) || value[1] != ':' {
		return false
	}
	return strings.Trim(value[2:], `/\`) == ""
}
