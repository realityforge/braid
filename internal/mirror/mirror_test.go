package mirror

import "testing"

func TestNewFromOptionsDefaultsPath(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		remotePath string
		want       string
	}{
		{name: "url basename", url: "http://example.test/path.git", want: "path"},
		{name: "trailing slash", url: "http://example.test/path.git/", want: "path"},
		{name: "remote path basename", url: "http://example.test/repo.git", remotePath: "lib/component", want: "component"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewFromOptions(test.url, Options{RemotePath: test.remotePath})
			if err != nil {
				t.Fatalf("NewFromOptions returned error: %v", err)
			}
			if got.Path != test.want {
				t.Fatalf("Path = %q, want %q", got.Path, test.want)
			}
		})
	}
}

func TestNewFromOptionsStripsSpecifiedPath(t *testing.T) {
	got, err := NewFromOptions("http://example.test/path.git", Options{LocalPath: "vendor/tools/mytool/"})
	if err != nil {
		t.Fatalf("NewFromOptions returned error: %v", err)
	}
	if got.Path != "vendor/tools/mytool" {
		t.Fatalf("Path = %q, want vendor/tools/mytool", got.Path)
	}
}

func TestMirrorRefsAndRemote(t *testing.T) {
	branch, err := NewFromOptions("http://example.test/mytool.git", Options{Branch: "mybranch"})
	if err != nil {
		t.Fatalf("NewFromOptions branch returned error: %v", err)
	}
	if got, want := branch.Remote(), "mybranch_braid_mytool"; got != want {
		t.Fatalf("Remote = %q, want %q", got, want)
	}
	if got, want := branch.LocalRef(), "mybranch_braid_mytool/mybranch"; got != want {
		t.Fatalf("LocalRef = %q, want %q", got, want)
	}
	if got, err := branch.RemoteRef(); err != nil || got != "+refs/heads/mybranch" {
		t.Fatalf("RemoteRef = %q, %v; want +refs/heads/mybranch, nil", got, err)
	}

	tag, err := NewFromOptions("http://example.test/mytool.git", Options{Tag: "v1"})
	if err != nil {
		t.Fatalf("NewFromOptions tag returned error: %v", err)
	}
	if got, want := tag.Remote(), "v1_braid_mytool"; got != want {
		t.Fatalf("Remote = %q, want %q", got, want)
	}
	if got, want := tag.LocalRef(), "tags/v1"; got != want {
		t.Fatalf("LocalRef = %q, want %q", got, want)
	}
	if got, err := tag.RemoteRef(); err != nil || got != "+refs/tags/v1" {
		t.Fatalf("RemoteRef = %q, %v; want +refs/tags/v1, nil", got, err)
	}
}

func TestRemoteSanitizesPathLikeRuby(t *testing.T) {
	got, err := NewFromOptions("http://example.test/path.git", Options{
		LocalPath: ".dotfolder/.dotfile.ext",
		Branch:    "master",
	})
	if err != nil {
		t.Fatalf("NewFromOptions returned error: %v", err)
	}
	if got.Remote() != "master_braid__dotfolder__dotfile_ext" {
		t.Fatalf("Remote = %q", got.Remote())
	}
}

func TestRevisionLockedMirror(t *testing.T) {
	got, err := NewFromOptions("http://example.test/path.git", Options{Revision: "abc123"})
	if err != nil {
		t.Fatalf("NewFromOptions returned error: %v", err)
	}
	if !got.Locked() {
		t.Fatal("Locked = false, want true")
	}
	if got.LocalRef() != "abc123" {
		t.Fatalf("LocalRef = %q, want abc123", got.LocalRef())
	}
	if _, err := got.RemoteRef(); err == nil {
		t.Fatal("RemoteRef returned nil error for revision-locked mirror")
	}
}

func TestNewFromOptionsRejectsInvalidTrackingCombinations(t *testing.T) {
	if _, err := NewFromOptions("http://example.test/path.git", Options{Branch: "main", Tag: "v1"}); err == nil {
		t.Fatal("branch+tag returned nil error")
	}
	if _, err := NewFromOptions("http://example.test/path.git", Options{Revision: "abc123", Tag: "v1"}); err == nil {
		t.Fatal("revision+tag returned nil error")
	}
}
