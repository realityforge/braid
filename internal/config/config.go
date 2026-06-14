package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"braid/internal/mirror"
)

const (
	FileName          = ".braids.json"
	LegacyFileName    = ".braids"
	CurrentVersion    = 1
	unsupportedLegacy = "legacy .braids config is unsupported"
)

type Config struct {
	Mirrors map[string]mirror.Mirror
}

type MirrorDoesNotExistError struct {
	Path string
}

func (e *MirrorDoesNotExistError) Error() string {
	return "mirror does not exist: " + e.Path
}

type PathAlreadyInUseError struct {
	Path string
}

func (e *PathAlreadyInUseError) Error() string {
	return "path already in use: " + e.Path
}

type UnsupportedLegacyError struct {
	Path string
}

func (e *UnsupportedLegacyError) Error() string {
	return unsupportedLegacy + ": " + e.Path
}

func Empty() Config {
	return Config{Mirrors: map[string]mirror.Mirror{}}
}

func Load(root string) (Config, error) {
	legacyPath := filepath.Join(root, LegacyFileName)
	if _, err := os.Stat(legacyPath); err == nil {
		return Config{}, &UnsupportedLegacyError{Path: legacyPath}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	return LoadFile(filepath.Join(root, FileName))
}

func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

func Parse(data []byte) (Config, error) {
	var raw rawConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, err
	}
	if raw.ConfigVersion == 0 {
		return Config{}, errors.New("missing config_version")
	}
	if raw.ConfigVersion > CurrentVersion {
		return Config{}, fmt.Errorf("config version %d is newer than supported version %d", raw.ConfigVersion, CurrentVersion)
	}
	if raw.ConfigVersion < CurrentVersion {
		return Config{}, fmt.Errorf("config version %d is unsupported; expected version %d", raw.ConfigVersion, CurrentVersion)
	}
	if raw.Mirrors == nil {
		return Config{}, errors.New("missing mirrors")
	}

	cfg := Empty()
	for localPath, rawMirror := range raw.Mirrors {
		parsed, err := parseMirror(strings.TrimRight(localPath, "/"), rawMirror)
		if err != nil {
			return Config{}, fmt.Errorf("mirror %q: %w", localPath, err)
		}
		cfg.Mirrors[parsed.Path] = parsed
	}
	return cfg, nil
}

func (c Config) Get(localPath string) (mirror.Mirror, bool) {
	m, ok := c.Mirrors[strings.TrimRight(localPath, "/")]
	return m, ok
}

func (c Config) GetRequired(localPath string) (mirror.Mirror, error) {
	m, ok := c.Get(localPath)
	if !ok {
		return mirror.Mirror{}, &MirrorDoesNotExistError{Path: localPath}
	}
	return m, nil
}

func (c Config) Paths() []string {
	paths := make([]string, 0, len(c.Mirrors))
	for localPath := range c.Mirrors {
		paths = append(paths, localPath)
	}
	sort.Strings(paths)
	return paths
}

func (c *Config) Add(m mirror.Mirror) error {
	if c.Mirrors == nil {
		c.Mirrors = map[string]mirror.Mirror{}
	}
	m.Path = strings.TrimRight(m.Path, "/")
	if _, exists := c.Mirrors[m.Path]; exists {
		return &PathAlreadyInUseError{Path: m.Path}
	}
	if err := validateMirror(m); err != nil {
		return err
	}
	c.Mirrors[m.Path] = m
	return nil
}

func (c *Config) Update(m mirror.Mirror) error {
	if c.Mirrors == nil {
		return &MirrorDoesNotExistError{Path: m.Path}
	}
	m.Path = strings.TrimRight(m.Path, "/")
	if _, exists := c.Mirrors[m.Path]; !exists {
		return &MirrorDoesNotExistError{Path: m.Path}
	}
	if err := validateMirror(m); err != nil {
		return err
	}
	c.Mirrors[m.Path] = m
	return nil
}

func (c *Config) Remove(localPath string) error {
	key := strings.TrimRight(localPath, "/")
	if _, exists := c.Mirrors[key]; !exists {
		return &MirrorDoesNotExistError{Path: localPath}
	}
	delete(c.Mirrors, key)
	return nil
}

func (c Config) WriteFile(path string) error {
	data, err := c.MarshalJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c Config) MarshalJSON() ([]byte, error) {
	raw := writeConfig{
		ConfigVersion: CurrentVersion,
		Mirrors:       map[string]writeMirror{},
	}
	for _, localPath := range c.Paths() {
		m := c.Mirrors[localPath]
		if err := validateMirror(m); err != nil {
			return nil, fmt.Errorf("mirror %q: %w", localPath, err)
		}
		raw.Mirrors[localPath] = writeMirror{
			URL:      m.URL,
			Branch:   m.Branch,
			Path:     m.RemotePath,
			Tag:      m.Tag,
			Revision: m.Revision,
		}
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

type rawConfig struct {
	ConfigVersion int                        `json:"config_version"`
	Mirrors       map[string]json.RawMessage `json:"mirrors"`
}

type readMirror struct {
	URL      string `json:"url"`
	Branch   string `json:"branch"`
	Path     string `json:"path"`
	Tag      string `json:"tag"`
	Revision string `json:"revision"`
}

type writeConfig struct {
	ConfigVersion int                    `json:"config_version"`
	Mirrors       map[string]writeMirror `json:"mirrors"`
}

type writeMirror struct {
	URL      string `json:"url"`
	Branch   string `json:"branch,omitempty"`
	Path     string `json:"path,omitempty"`
	Tag      string `json:"tag,omitempty"`
	Revision string `json:"revision"`
}

func parseMirror(localPath string, raw json.RawMessage) (mirror.Mirror, error) {
	var decoded readMirror
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return mirror.Mirror{}, err
	}
	m := mirror.Mirror{
		Path:       localPath,
		URL:        decoded.URL,
		Branch:     decoded.Branch,
		RemotePath: decoded.Path,
		Tag:        decoded.Tag,
		Revision:   decoded.Revision,
	}
	if err := validateMirror(m); err != nil {
		return mirror.Mirror{}, err
	}
	return m, nil
}

func validateMirror(m mirror.Mirror) error {
	if m.Path == "" {
		return errors.New("missing mirror path")
	}
	if m.URL == "" {
		return errors.New("missing url")
	}
	if m.Revision == "" {
		return errors.New("missing revision")
	}
	if m.Branch != "" && m.Tag != "" {
		return errors.New("cannot specify both branch and tag")
	}
	return nil
}
