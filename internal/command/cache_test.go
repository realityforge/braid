package command

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/cli"
)

func TestResolveCacheUsesUserCacheDirByDefault(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "user-cache")
	withUserCacheDir(t, cacheRoot, nil)

	got, err := ResolveCache(cli.GlobalOptions{}, envLookup(nil), t.TempDir())
	if err != nil {
		t.Fatalf("ResolveCache returned error: %v", err)
	}
	if !got.Enabled {
		t.Fatal("cache Enabled = false, want true")
	}
	if want := filepath.Join(cacheRoot, "braid"); got.Dir != want {
		t.Fatalf("cache Dir = %q, want %q", got.Dir, want)
	}
}

func TestResolveCachePrecedence(t *testing.T) {
	cwd := t.TempDir()
	cacheRoot := filepath.Join(t.TempDir(), "user-cache")
	withUserCacheDir(t, cacheRoot, nil)

	tests := []struct {
		name    string
		global  cli.GlobalOptions
		env     map[string]string
		want    CacheConfig
		wantErr string
	}{
		{
			name:   "no cache disables cache",
			global: cli.GlobalOptions{NoCache: true},
			env:    map[string]string{"BRAID_LOCAL_CACHE_DIR": "ignored"},
			want:   CacheConfig{Enabled: false},
		},
		{
			name:    "no cache conflicts with cache dir",
			global:  cli.GlobalOptions{NoCache: true, CacheDir: "cache", CacheDirSet: true},
			wantErr: "--no-cache and --cache-dir cannot be used together",
		},
		{
			name:   "cache dir flag overrides disabling env",
			global: cli.GlobalOptions{CacheDir: "flag-cache", CacheDirSet: true},
			env:    map[string]string{"BRAID_USE_LOCAL_CACHE": "false", "BRAID_LOCAL_CACHE_DIR": "ignored"},
			want:   CacheConfig{Enabled: true, Dir: filepath.Join(cwd, "flag-cache")},
		},
		{
			name: "use local cache false disables cache",
			env:  map[string]string{"BRAID_USE_LOCAL_CACHE": "false"},
			want: CacheConfig{Enabled: false},
		},
		{
			name: "env cache dir overrides user cache dir",
			env:  map[string]string{"BRAID_LOCAL_CACHE_DIR": "env-cache"},
			want: CacheConfig{Enabled: true, Dir: filepath.Join(cwd, "env-cache")},
		},
		{
			name: "backslash home expansion",
			env:  map[string]string{"HOME": filepath.Join(cwd, "home"), "BRAID_LOCAL_CACHE_DIR": `~\braid-cache`},
			want: CacheConfig{Enabled: true, Dir: filepath.Join(cwd, "home", "braid-cache")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveCache(test.global, envLookup(test.env), cwd)
			if test.wantErr != "" {
				if err == nil || err.Error() != test.wantErr {
					t.Fatalf("ResolveCache error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveCache returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("ResolveCache = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestResolveCacheFallsBackWhenUserCacheDirUnavailable(t *testing.T) {
	withUserCacheDir(t, "", errors.New("unavailable"))
	cwd := t.TempDir()
	home := filepath.Join(cwd, "home")

	got, err := ResolveCache(cli.GlobalOptions{}, envLookup(map[string]string{"USERPROFILE": home}), cwd)
	if err != nil {
		t.Fatalf("ResolveCache returned error: %v", err)
	}
	if want := filepath.Join(home, ".braid", "cache"); got.Dir != want {
		t.Fatalf("cache Dir = %q, want %q", got.Dir, want)
	}
}

func TestRuntimeCacheUsesCanonicalWorkingDirForRelativeCacheDir(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("create real dir: %v", err)
	}
	linkDir := filepath.Join(root, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Chdir(linkDir)

	got, err := runtimeCache(cli.GlobalOptions{CacheDir: "cache", CacheDirSet: true})
	if err != nil {
		t.Fatalf("runtimeCache returned error: %v", err)
	}
	canonicalRealDir, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatalf("canonicalize real dir: %v", err)
	}
	want := CacheConfig{Enabled: true, Dir: filepath.Join(canonicalRealDir, "cache")}
	if got != want {
		t.Fatalf("runtimeCache = %#v, want %#v", got, want)
	}
}

func TestCachePathUsesStableWindowsSafeHashKey(t *testing.T) {
	cacheDir := t.TempDir()
	url := `https://example.test/repo?name=bad*chars|"<>@\windows\path`

	got := CachePath(cacheDir, url)
	gotAgain := CachePath(cacheDir, url)

	if got != gotAgain {
		t.Fatalf("CachePath is not stable: %q then %q", got, gotAgain)
	}
	key := filepath.Base(got)
	if len(key) != 64 {
		t.Fatalf("cache key length = %d, want 64", len(key))
	}
	if strings.ContainsAny(key, "<>:\"/\\|?*") {
		t.Fatalf("cache key %q contains Windows-invalid filename characters", key)
	}
}
