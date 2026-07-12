package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"braid/internal/pathcheck"
	"braid/internal/source"
)

const (
	FileName       = ".braids.json"
	CurrentVersion = 2
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Config struct{ Sources map[string]source.Source }
type SourceDoesNotExistError struct{ Name string }

func (e *SourceDoesNotExistError) Error() string { return "source does not exist: " + e.Name }

type MirrorDoesNotExistError struct{ Path string }

func (e *MirrorDoesNotExistError) Error() string { return "mirror does not exist: " + e.Path }

type PathAlreadyInUseError struct{ Path string }

func (e *PathAlreadyInUseError) Error() string { return "path already in use: " + e.Path }

func Empty() Config                    { return Config{Sources: map[string]source.Source{}} }
func Load(root string) (Config, error) { return LoadFile(filepath.Join(root, FileName)) }
func LoadFile(file string) (Config, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

type rawConfig struct {
	ConfigVersion int                        `json:"config_version"`
	Sources       map[string]json.RawMessage `json:"sources"`
	Mirrors       map[string]json.RawMessage `json:"mirrors"`
}
type readSource struct {
	URL          string                     `json:"url"`
	Branch       string                     `json:"branch"`
	Tag          string                     `json:"tag"`
	Revision     string                     `json:"revision"`
	PartialClone bool                       `json:"partial_clone"`
	Mirrors      map[string]json.RawMessage `json:"mirrors"`
}
type readMirrorV1 struct {
	URL      string `json:"url"`
	Branch   string `json:"branch"`
	Path     string `json:"path"`
	Tag      string `json:"tag"`
	Revision string `json:"revision"`
}
type writeConfig struct {
	ConfigVersion int                    `json:"config_version"`
	Sources       map[string]writeSource `json:"sources"`
}
type writeSource struct {
	URL          string            `json:"url"`
	Branch       string            `json:"branch,omitempty"`
	Tag          string            `json:"tag,omitempty"`
	Revision     string            `json:"revision"`
	PartialClone bool              `json:"partial_clone,omitempty"`
	Mirrors      map[string]string `json:"mirrors"`
}

func Parse(data []byte) (Config, error) {
	var raw rawConfig
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&raw); err != nil {
		return Config{}, err
	}
	if err := requireJSONEOF(d); err != nil {
		return Config{}, err
	}
	if raw.ConfigVersion == 0 {
		return Config{}, errors.New("missing config_version")
	}
	if raw.ConfigVersion > CurrentVersion {
		return Config{}, fmt.Errorf("config version %d is newer than supported version %d", raw.ConfigVersion, CurrentVersion)
	}
	if raw.ConfigVersion < CurrentVersion {
		return Config{}, fmt.Errorf("config version %d requires upgrade; run %q", raw.ConfigVersion, "braid upgrade-config")
	}
	if raw.Mirrors != nil {
		return Config{}, errors.New("config version 2 uses obsolete unreleased mirrors schema; replace it with sources")
	}
	if raw.Sources == nil {
		return Config{}, errors.New("missing sources")
	}
	cfg := Empty()
	for name, encoded := range raw.Sources {
		var rs readSource
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&rs); err != nil {
			return Config{}, fmt.Errorf("source %q: %w", name, err)
		}
		s, err := decodeSource(name, rs)
		if err != nil {
			return Config{}, fmt.Errorf("source %q: %w", name, err)
		}
		if err := cfg.AddSource(s); err != nil {
			return Config{}, fmt.Errorf("source %q: %w", name, err)
		}
	}
	return cfg, nil
}
func decodeSource(name string, rs readSource) (source.Source, error) {
	var tracking source.Tracking = source.RevisionTracking{}
	if rs.Branch != "" && rs.Tag != "" {
		return source.Source{}, errors.New("cannot specify both branch and tag")
	}
	if rs.Branch != "" {
		tracking = source.BranchTracking{Branch: rs.Branch}
	} else if rs.Tag != "" {
		tracking = source.TagTracking{Tag: rs.Tag}
	}
	mirrors := make([]source.Mirror, 0, len(rs.Mirrors))
	for local, encoded := range rs.Mirrors {
		if bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
			return source.Source{}, fmt.Errorf("mirror %q: upstream path cannot be null", local)
		}
		var upstream string
		if err := json.Unmarshal(encoded, &upstream); err != nil {
			return source.Source{}, fmt.Errorf("mirror %q: upstream path must be a string", local)
		}
		mirrors = append(mirrors, source.Mirror{LocalPath: local, UpstreamPath: upstream})
	}
	return source.Source{Name: name, URL: source.CleanURL(rs.URL), Tracking: tracking, Revision: rs.Revision, PartialClone: rs.PartialClone, Mirrors: mirrors}, nil
}

func (c Config) SourceNames() []string {
	names := make([]string, 0, len(c.Sources))
	for name := range c.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func (c Config) SourceByName(name string) (source.Source, bool) {
	s, ok := c.Sources[name]
	return s, ok
}
func (c Config) SourceByNameRequired(name string) (source.Source, error) {
	s, ok := c.SourceByName(name)
	if !ok {
		return source.Source{}, &SourceDoesNotExistError{Name: name}
	}
	return s, nil
}
func (c Config) MirrorByLocalPath(localPath string) (source.Source, source.Mirror, bool) {
	localPath = strings.TrimRight(localPath, "/")
	for _, name := range c.SourceNames() {
		s := c.Sources[name]
		if m, ok := s.MirrorByLocalPath(localPath); ok {
			return s, m, true
		}
	}
	return source.Source{}, source.Mirror{}, false
}
func (c Config) MirrorByLocalPathRequired(localPath string) (source.Source, source.Mirror, error) {
	s, m, ok := c.MirrorByLocalPath(localPath)
	if !ok {
		return source.Source{}, source.Mirror{}, &MirrorDoesNotExistError{Path: localPath}
	}
	return s, m, nil
}
func (c Config) SourcesSorted() []source.Source {
	result := make([]source.Source, 0, len(c.Sources))
	for _, name := range c.SourceNames() {
		result = append(result, c.Sources[name])
	}
	return result
}
func (c Config) MirrorsSorted() []source.SourceMirror {
	var result []source.SourceMirror
	for _, s := range c.SourcesSorted() {
		for _, m := range s.SortedMirrors() {
			result = append(result, s.WithMirror(m))
		}
	}
	return result
}
func (c Config) LocalPaths() []string {
	items := c.MirrorsSorted()
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.LocalPath)
	}
	sort.Strings(paths)
	return paths
}

func (c *Config) AddSource(s source.Source) error {
	if c.Sources == nil {
		c.Sources = map[string]source.Source{}
	}
	if _, ok := c.Sources[s.Name]; ok {
		return fmt.Errorf("source name already exists: %s", s.Name)
	}
	if err := validateSource(s); err != nil {
		return err
	}
	if err := c.validateNewPaths(s.Mirrors, ""); err != nil {
		return err
	}
	c.Sources[s.Name] = s
	return nil
}
func (c *Config) UpdateSource(s source.Source) error {
	if _, ok := c.Sources[s.Name]; !ok {
		return &SourceDoesNotExistError{Name: s.Name}
	}
	if err := validateSource(s); err != nil {
		return err
	}
	if err := c.validateNewPaths(s.Mirrors, s.Name); err != nil {
		return err
	}
	c.Sources[s.Name] = s
	return nil
}
func (c *Config) RemoveSource(name string) error {
	if _, ok := c.Sources[name]; !ok {
		return &SourceDoesNotExistError{Name: name}
	}
	delete(c.Sources, name)
	return nil
}
func (c *Config) RemoveMirror(localPath string) (source.Source, bool, error) {
	s, _, ok := c.MirrorByLocalPath(localPath)
	if !ok {
		return source.Source{}, false, &MirrorDoesNotExistError{Path: localPath}
	}
	if len(s.Mirrors) == 1 {
		delete(c.Sources, s.Name)
		return s, true, nil
	}
	kept := make([]source.Mirror, 0, len(s.Mirrors)-1)
	for _, m := range s.Mirrors {
		if m.LocalPath != localPath {
			kept = append(kept, m)
		}
	}
	s.Mirrors = kept
	c.Sources[s.Name] = s
	return s, false, nil
}
func (c Config) validateNewPaths(mirrors []source.Mirror, ignoreSource string) error {
	existing := []string{}
	for _, s := range c.SourcesSorted() {
		if s.Name == ignoreSource {
			continue
		}
		existing = append(existing, s.LocalPaths()...)
	}
	for _, m := range mirrors {
		if err := pathcheck.ValidateLocal(m.LocalPath, existing); err != nil {
			return err
		}
		for _, other := range existing {
			if pathsOverlap(m.LocalPath, other) {
				return &PathAlreadyInUseError{Path: m.LocalPath}
			}
		}
		existing = append(existing, m.LocalPath)
	}
	return nil
}
func pathsOverlap(a, b string) bool {
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
func validateSource(s source.Source) error {
	if !namePattern.MatchString(s.Name) || s.Name == "." || s.Name == ".." {
		return fmt.Errorf("invalid source name %q", s.Name)
	}
	if s.URL == "" {
		return errors.New("missing url")
	}
	if s.Revision == "" {
		return errors.New("missing revision")
	}
	if s.Tracking == nil {
		return errors.New("missing tracking")
	}
	if len(s.Mirrors) == 0 {
		return errors.New("missing mirrors")
	}
	seen := []string{}
	for _, m := range s.SortedMirrors() {
		if err := pathcheck.ValidateLocal(m.LocalPath, seen); err != nil {
			return fmt.Errorf("mirror %q: %w", m.LocalPath, err)
		}
		if m.UpstreamPath != "" {
			if err := pathcheck.ValidateUpstream(m.UpstreamPath); err != nil {
				return fmt.Errorf("mirror %q: %w", m.LocalPath, err)
			}
		}
		for _, other := range seen {
			if pathsOverlap(m.LocalPath, other) {
				return fmt.Errorf("local mirror paths overlap: %s and %s", other, m.LocalPath)
			}
		}
		seen = append(seen, m.LocalPath)
	}
	return nil
}

func (c Config) MarshalJSON() ([]byte, error) {
	raw := writeConfig{ConfigVersion: CurrentVersion, Sources: map[string]writeSource{}}
	for _, s := range c.SourcesSorted() {
		if err := validateSource(s); err != nil {
			return nil, fmt.Errorf("source %q: %w", s.Name, err)
		}
		mirrors := map[string]string{}
		for _, m := range s.SortedMirrors() {
			mirrors[m.LocalPath] = m.UpstreamPath
		}
		ws := writeSource{URL: s.URL, Revision: s.Revision, PartialClone: s.PartialClone, Mirrors: mirrors}
		switch t := s.Tracking.(type) {
		case source.BranchTracking:
			ws.Branch = t.Branch
		case source.TagTracking:
			ws.Tag = t.Tag
		}
		raw.Sources[s.Name] = ws
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
func (c Config) WriteFile(file string) error {
	data, err := c.MarshalJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}

func UpgradeV1(data []byte) (Config, error) {
	var raw struct {
		ConfigVersion int                        `json:"config_version"`
		Mirrors       map[string]json.RawMessage `json:"mirrors"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&raw); err != nil {
		return Config{}, err
	}
	if err := requireJSONEOF(d); err != nil {
		return Config{}, err
	}
	if raw.ConfigVersion != 1 {
		return Config{}, fmt.Errorf("expected config version 1, got %d", raw.ConfigVersion)
	}
	if raw.Mirrors == nil {
		return Config{}, errors.New("missing mirrors")
	}
	type entry struct {
		local string
		r     readMirrorV1
	}
	entries := make([]entry, 0, len(raw.Mirrors))
	for local, encoded := range raw.Mirrors {
		var r readMirrorV1
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&r); err != nil {
			return Config{}, fmt.Errorf("mirror %q: %w", local, err)
		}
		entries = append(entries, entry{strings.TrimRight(local, "/"), r})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].local < entries[j].local })
	type group struct {
		r       readMirrorV1
		mirrors []source.Mirror
	}
	groups := []*group{}
	byKey := map[string]*group{}
	for _, e := range entries {
		cleanURL := source.CleanURL(e.r.URL)
		key := strings.Join([]string{cleanURL, e.r.Branch, e.r.Tag, e.r.Revision}, "\x00")
		g := byKey[key]
		if g == nil {
			e.r.URL = cleanURL
			g = &group{r: e.r}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.mirrors = append(g.mirrors, source.Mirror{LocalPath: e.local, UpstreamPath: strings.TrimRight(e.r.Path, "/")})
	}
	cfg := Empty()
	used := map[string]bool{}
	for _, g := range groups {
		base := sanitizeUpgradeName(source.DerivedName(g.r.URL))
		name := base
		for suffix := 2; used[name]; suffix++ {
			name = fmt.Sprintf("%s-%d", base, suffix)
		}
		used[name] = true
		tracking := source.Tracking(source.RevisionTracking{})
		if g.r.Branch != "" && g.r.Tag != "" {
			return Config{}, errors.New("cannot specify both branch and tag")
		}
		if g.r.Branch != "" {
			tracking = source.BranchTracking{Branch: g.r.Branch}
		} else if g.r.Tag != "" {
			tracking = source.TagTracking{Tag: g.r.Tag}
		}
		if err := cfg.AddSource(source.Source{Name: name, URL: g.r.URL, Tracking: tracking, Revision: g.r.Revision, Mirrors: g.mirrors}); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected data after JSON document")
		}
		return err
	}
	return nil
}

var invalidNameRun = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeUpgradeName(value string) string {
	value = invalidNameRun.ReplaceAllString(value, "-")
	value = strings.Trim(value, "._-")
	for value != "" && !isASCIIAlphanumeric(value[0]) {
		value = value[1:]
	}
	if value == "" || value == "." || value == ".." {
		return "source"
	}
	return value
}

func isASCIIAlphanumeric(value byte) bool {
	return strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", rune(value))
}
