package config

import (
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/mirror"
)

func TestParseModernConfig(t *testing.T) {
	cfg, err := Parse([]byte(`{
  "config_version": 2,
  "mirrors": {
    "vendor/repo": {
      "url": "https://example.test/repo.git",
      "branch": "main",
      "path": "lib",
      "revision": "abc123"
    },
    "vendor/tagged": {
      "url": "https://example.test/tagged.git",
      "tag": "v1",
      "revision": "def456"
    }
  }
}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	paths := cfg.Paths()
	if got, want := strings.Join(paths, ","), "vendor/repo,vendor/tagged"; got != want {
		t.Fatalf("Paths = %q, want %q", got, want)
	}
	m, ok := cfg.Get("vendor/repo/")
	if !ok {
		t.Fatal("Get did not find vendor/repo/")
	}
	if m.URL != "https://example.test/repo.git" || m.Branch != "main" || m.RemotePath != "lib" || m.Revision != "abc123" {
		t.Fatalf("mirror = %#v", m)
	}
}

func TestParseRejectsUnsupportedConfigs(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "future version", json: `{"config_version":999,"mirrors":{}}`, want: "newer than supported"},
		{name: "missing version", json: `{"mirrors":{}}`, want: "missing config_version"},
		{name: "missing mirrors", json: `{"config_version":2}`, want: "missing mirrors"},
		{name: "unknown root field", json: `{"config_version":2,"mirrors":{},"extra":true}`, want: "unknown field"},
		{name: "unknown mirror field", json: `{"config_version":2,"mirrors":{"x":{"url":"u","revision":"r","extra":true}}}`, want: "unknown field"},
		{name: "missing url", json: `{"config_version":2,"mirrors":{"x":{"revision":"r"}}}`, want: "missing url"},
		{name: "missing revision", json: `{"config_version":2,"mirrors":{"x":{"url":"u"}}}`, want: "missing revision"},
		{name: "branch tag conflict", json: `{"config_version":2,"mirrors":{"x":{"url":"u","branch":"main","tag":"v1","revision":"r"}}}`, want: "cannot specify both branch and tag"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.json))
			if err == nil {
				t.Fatal("Parse returned nil error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want containing %q", err.Error(), test.want)
			}
		})
	}
}

func TestConfigAddUpdateRemove(t *testing.T) {
	cfg := Empty()
	m := mirror.Mirror{Path: "vendor/repo", URL: "https://example.test/repo.git", Branch: "main", Revision: "abc123"}

	if err := cfg.Add(m); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := cfg.Add(m); err == nil {
		t.Fatal("Add duplicate returned nil error")
	}

	m.Branch = "other"
	m.Revision = "def456"
	if err := cfg.Update(m); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	got, err := cfg.GetRequired("vendor/repo")
	if err != nil {
		t.Fatalf("GetRequired returned error: %v", err)
	}
	if got.Branch != "other" || got.Revision != "def456" {
		t.Fatalf("updated mirror = %#v", got)
	}

	if err := cfg.Remove("vendor/repo"); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if _, ok := cfg.Get("vendor/repo"); ok {
		t.Fatal("Get found removed mirror")
	}
	if err := cfg.Remove("vendor/repo"); err == nil {
		t.Fatal("Remove missing mirror returned nil error")
	}
}

func TestMarshalJSONStableFormat(t *testing.T) {
	cfg := Empty()
	if err := cfg.Add(mirror.Mirror{
		Path:     "vendor/z",
		URL:      "https://example.test/z.git",
		Tag:      "v1",
		Revision: "def456",
	}); err != nil {
		t.Fatalf("Add z returned error: %v", err)
	}
	if err := cfg.Add(mirror.Mirror{
		Path:       "vendor/a",
		URL:        "https://example.test/a.git",
		Branch:     "main",
		RemotePath: "lib",
		Revision:   "abc123",
	}); err != nil {
		t.Fatalf("Add a returned error: %v", err)
	}

	data, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON returned error: %v", err)
	}
	want := `{
  "config_version": 2,
  "mirrors": {
    "vendor/a": {
      "url": "https://example.test/a.git",
      "branch": "main",
      "path": "lib",
      "revision": "abc123"
    },
    "vendor/z": {
      "url": "https://example.test/z.git",
      "tag": "v1",
      "revision": "def456"
    }
  }
}
`
	if got := string(data); got != want {
		t.Fatalf("MarshalJSON =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteAndLoadFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	cfg := Empty()
	if err := cfg.Add(mirror.Mirror{Path: "vendor/repo", URL: "https://example.test/repo.git", Branch: "main", Revision: "abc123"}); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := cfg.WriteFile(path); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	got, ok := loaded.Get("vendor/repo")
	if !ok {
		t.Fatal("loaded config missing mirror")
	}
	if got.URL != "https://example.test/repo.git" || got.Branch != "main" || got.Revision != "abc123" {
		t.Fatalf("loaded mirror = %#v", got)
	}
}

func TestUpgradeV1(t *testing.T) {
	cfg, err := UpgradeV1([]byte(`{"config_version":1,"mirrors":{"vendor/repo":{"url":"u","path":"lib","revision":"r"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"config_version": 2`) {
		t.Fatalf("upgraded config = %s", data)
	}
}

func TestPartialCloneRequiresPath(t *testing.T) {
	_, err := Parse([]byte(`{"config_version":2,"mirrors":{"vendor/repo":{"url":"u","revision":"r","partial_clone":true}}}`))
	if err == nil || !strings.Contains(err.Error(), "partial clone requires path") {
		t.Fatalf("error = %v", err)
	}
}
