package config

import (
	"strings"
	"testing"

	"braid/internal/source"
)

func TestParseAndMarshalCanonicalV2(t *testing.T) {
	input := []byte(`{"config_version":2,"sources":{"replicant":{"url":"https://example.test/replicant.git/","branch":"main","revision":"abc","mirrors":{"vendor/replicant":"","licenses/LICENSE":"LICENSE"}}}}`)
	cfg, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := cfg.SourceByName("replicant")
	if !ok || s.URL != "https://example.test/replicant.git" || s.Branch() != "main" {
		t.Fatalf("source=%#v", s)
	}
	data, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "config_version": 2,
  "sources": {
    "replicant": {
      "url": "https://example.test/replicant.git",
      "branch": "main",
      "revision": "abc",
      "mirrors": {
        "licenses/LICENSE": "LICENSE",
        "vendor/replicant": ""
      }
    }
  }
}
`
	if string(data) != want {
		t.Fatalf("got\n%s\nwant\n%s", data, want)
	}
}

func TestParseRejectsInvalidV2(t *testing.T) {
	tests := []struct{ name, json, want string }{
		{"obsolete", `{"config_version":2,"mirrors":{}}`, "obsolete unreleased"},
		{"missing sources", `{"config_version":2}`, "missing sources"},
		{"missing mirrors", `{"config_version":2,"sources":{"x":{"url":"u","revision":"r"}}}`, "missing mirrors"},
		{"null mirror path", `{"config_version":2,"sources":{"x":{"url":"u","revision":"r","mirrors":{"x":null}}}}`, "cannot be null"},
		{"non-string mirror path", `{"config_version":2,"sources":{"x":{"url":"u","revision":"r","mirrors":{"x":42}}}}`, "must be a string"},
		{"empty url", `{"config_version":2,"sources":{"x":{"url":"","revision":"r","mirrors":{"x":""}}}}`, "missing url"},
		{"empty revision", `{"config_version":2,"sources":{"x":{"url":"u","revision":"","mirrors":{"x":""}}}}`, "missing revision"},
		{"invalid name", `{"config_version":2,"sources":{"bad name":{"url":"u","revision":"r","mirrors":{"x":""}}}}`, "invalid source name"},
		{"trailing local separator", `{"config_version":2,"sources":{"x":{"url":"u","revision":"r","mirrors":{"x/":""}}}}`, "empty path element"},
		{"trailing upstream separator", `{"config_version":2,"sources":{"x":{"url":"u","revision":"r","mirrors":{"x":"upstream/"}}}}`, "empty path element"},
		{"tracking conflict", `{"config_version":2,"sources":{"x":{"url":"u","branch":"a","tag":"b","revision":"r","mirrors":{"x":""}}}}`, "both branch and tag"},
		{"overlap", `{"config_version":2,"sources":{"x":{"url":"u","revision":"r","mirrors":{"a":"","a/b":"x"}}}}`, "overlap"},
		{"case-fold overlap", `{"config_version":2,"sources":{"x":{"url":"u","revision":"r","mirrors":{"Vendor/Lib":"","vendor/lib/LICENSE":"LICENSE"}}}}`, "case-fold"},
		{"trailing JSON", `{"config_version":2,"sources":{}} {}`, "unexpected data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.json))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v want %q", err, tt.want)
			}
		})
	}
}

func TestConfigExplicitLookupsAndGlobalOverlap(t *testing.T) {
	cfg := Empty()
	s := source.Source{Name: "one", URL: "u", Tracking: source.RevisionTracking{}, Revision: "r", Mirrors: []source.Mirror{{LocalPath: "vendor/one", UpstreamPath: ""}}}
	if err := cfg.AddSource(s); err != nil {
		t.Fatal(err)
	}
	got, m, ok := cfg.MirrorByLocalPath("vendor/one")
	if !ok || got.Name != "one" || m.LocalPath != "vendor/one" {
		t.Fatalf("lookup=%#v %#v %v", got, m, ok)
	}
	s2 := source.Source{Name: "two", URL: "v", Tracking: source.RevisionTracking{}, Revision: "r", Mirrors: []source.Mirror{{LocalPath: "vendor/one/sub", UpstreamPath: ""}}}
	if err := cfg.AddSource(s2); err == nil {
		t.Fatal("expected overlap")
	}
}

func TestUpgradeV1GroupsAndNamesDeterministically(t *testing.T) {
	cfg, err := UpgradeV1([]byte(`{"config_version":1,"mirrors":{"vendor/repo":{"url":"https://x/repo.git/","branch":"main","revision":"r","path":""},"licenses/repo":{"url":"https://x/repo.git","branch":"main","revision":"r","path":"LICENSE"},"other/repo":{"url":"ssh://x/repo.git","branch":"main","revision":"r","path":""}}}`))
	if err != nil {
		t.Fatal(err)
	}
	names := cfg.SourceNames()
	if strings.Join(names, ",") != "repo,repo-2" {
		t.Fatalf("names=%v", names)
	}
	if len(cfg.Sources["repo"].Mirrors) != 2 {
		t.Fatalf("source=%#v", cfg.Sources["repo"])
	}
}

func TestUpgradeSanitizesInvalidName(t *testing.T) {
	cfg, err := UpgradeV1([]byte(`{"config_version":1,"mirrors":{"x":{"url":"https://host/bad name.git","revision":"r"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.SourceByName("bad-name"); !ok {
		t.Fatalf("names=%v", cfg.SourceNames())
	}
}

func TestUpgradeV1AllocatesGloballyUniqueSuffixes(t *testing.T) {
	cfg, err := UpgradeV1([]byte(`{"config_version":1,"mirrors":{"a":{"url":"https://x/repo.git","branch":"main","revision":"1"},"b":{"url":"https://y/repo.git","branch":"main","revision":"2"},"c":{"url":"https://z/repo-2.git","branch":"main","revision":"3"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.SourceNames(), ","); got != "repo,repo-2,repo-2-2" {
		t.Fatalf("names = %s", got)
	}
}
