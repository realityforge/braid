package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/mirror"
	"braid/internal/testutil"
)

func TestResolveCacheContract(t *testing.T) {
	env := func(values map[string]string) EnvLookup {
		return func(key string) (string, bool) {
			value, ok := values[key]
			return value, ok
		}
	}
	cwd := filepath.Join(string(filepath.Separator), "work")
	home := filepath.Join(string(filepath.Separator), "home")
	userCache := filepath.Join(string(filepath.Separator), "user-cache")
	withUserCacheDir(t, userCache, nil)

	tests := []struct {
		name    string
		global  cli.GlobalOptions
		env     map[string]string
		want    CacheConfig
		wantErr string
	}{
		{name: "default enabled", env: map[string]string{"HOME": home}, want: CacheConfig{Enabled: true, Dir: filepath.Join(userCache, "braid")}},
		{name: "env true", env: map[string]string{"HOME": home, "BRAID_USE_LOCAL_CACHE": "true"}, want: CacheConfig{Enabled: true, Dir: filepath.Join(userCache, "braid")}},
		{name: "env one", env: map[string]string{"HOME": home, "BRAID_USE_LOCAL_CACHE": "1"}, want: CacheConfig{Enabled: true, Dir: filepath.Join(userCache, "braid")}},
		{name: "env disabled", env: map[string]string{"HOME": home, "BRAID_USE_LOCAL_CACHE": "false"}, want: CacheConfig{Enabled: false}},
		{name: "env cache dir", env: map[string]string{"HOME": home, "BRAID_LOCAL_CACHE_DIR": "~/custom"}, want: CacheConfig{Enabled: true, Dir: filepath.Join(home, "custom")}},
		{name: "flag no cache", global: cli.GlobalOptions{NoCache: true}, env: map[string]string{"HOME": home}, want: CacheConfig{Enabled: false}},
		{name: "flag cache dir", global: cli.GlobalOptions{CacheDir: "rel-cache", CacheDirSet: true}, env: map[string]string{"HOME": home, "BRAID_USE_LOCAL_CACHE": "false"}, want: CacheConfig{Enabled: true, Dir: filepath.Join(cwd, "rel-cache")}},
		{name: "invalid both flags", global: cli.GlobalOptions{NoCache: true, CacheDir: "cache", CacheDirSet: true}, env: map[string]string{"HOME": home}, wantErr: "--no-cache and --cache-dir cannot be used together"},
		{name: "invalid empty env cache dir", env: map[string]string{"HOME": home, "BRAID_LOCAL_CACHE_DIR": ""}, wantErr: "cache directory cannot be empty"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveCache(test.global, env(test.env), cwd)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("ResolveCache error = %v, want containing %q", err, test.wantErr)
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

func TestSetupCommandCreatesAllRemotes(t *testing.T) {
	upstreamOne := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamOne, "README.md", "one\n")
	revisionOne := testutil.CommitAll(t, upstreamOne, "upstream one")

	upstreamTwo := testutil.InitRepo(t)
	testutil.WriteFile(t, upstreamTwo, "README.md", "two\n")
	revisionTwo := testutil.CommitAll(t, upstreamTwo, "upstream two")

	repo := testutil.InitRepo(t)
	testutil.WriteFile(t, repo, "README.md", "downstream\n")
	testutil.CommitAll(t, repo, "downstream")

	cfg := config.Empty()
	for _, m := range []mirror.Mirror{
		{Path: "vendor/one", URL: upstreamOne, Branch: "main", Revision: revisionOne},
		{Path: "vendor/two", URL: upstreamTwo, Branch: "main", Revision: revisionTwo},
	} {
		if err := cfg.Add(m); err != nil {
			t.Fatalf("Add mirror config: %v", err)
		}
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("Write config: %v", err)
	}

	withUserCacheDir(t, filepath.Join(t.TempDir(), "user-cache"), nil)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"setup"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", code, stderr.String())
	}
	for _, remote := range []string{"main_braid_vendor_one", "main_braid_vendor_two"} {
		if result := testutil.Git(t, repo, "remote", "get-url", remote); strings.TrimSpace(result.Stdout) == "" {
			t.Fatalf("remote %q was not created", remote)
		}
	}
}

func TestSetupCommandCreatesAndReusesRemotes(t *testing.T) {
	repo := setupRepoWithConfig(t)
	cacheRoot := filepath.Join(t.TempDir(), "user-cache")
	withUserCacheDir(t, cacheRoot, nil)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"setup"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", code, stderr.String())
	}

	remote := "main_braid_vendor_repo"
	firstURL := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", remote).Stdout)
	if !strings.HasPrefix(firstURL, filepath.Join(cacheRoot, "braid")) {
		t.Fatalf("remote URL = %q, want default cache path", firstURL)
	}

	testutil.Git(t, repo, "remote", "set-url", remote, "manually-kept")
	stderr.Reset()
	code = NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"setup"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("second setup exit = %d, stderr = %q", code, stderr.String())
	}
	reusedURL := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", remote).Stdout)
	if reusedURL != "manually-kept" {
		t.Fatalf("reused URL = %q, want manually-kept", reusedURL)
	}

	stderr.Reset()
	code = NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"setup", "--force"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("force setup exit = %d, stderr = %q", code, stderr.String())
	}
	forcedURL := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", remote).Stdout)
	if forcedURL == "manually-kept" {
		t.Fatal("force setup did not replace existing remote")
	}
}

func TestSetupCommandHonorsNoCacheAndCacheDir(t *testing.T) {
	repo := setupRepoWithConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	app := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo})
	if code := app.Run([]string{"--no-cache", "setup", "vendor/repo"}, &stdout, &stderr); code != 0 {
		t.Fatalf("setup --no-cache exit = %d, stderr = %q", code, stderr.String())
	}
	remote := "main_braid_vendor_repo"
	noCacheURL := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", remote).Stdout)
	cfg, err := config.Load(repo)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if noCacheURL != cfg.Mirrors["vendor/repo"].URL {
		t.Fatalf("remote URL = %q, want %q", noCacheURL, cfg.Mirrors["vendor/repo"].URL)
	}

	testutil.Git(t, repo, "remote", "rm", remote)
	stderr.Reset()
	if code := app.Run([]string{"--cache-dir", "local-cache", "setup", "vendor/repo"}, &stdout, &stderr); code != 0 {
		t.Fatalf("setup --cache-dir exit = %d, stderr = %q", code, stderr.String())
	}
	cacheURL := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", remote).Stdout)
	canonicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("canonicalize repo path: %v", err)
	}
	if !strings.HasPrefix(cacheURL, filepath.Join(canonicalRepo, "local-cache")) {
		t.Fatalf("cache URL = %q, want under relative cache dir", cacheURL)
	}
}

func TestSetupCommandFromSubdirectoryUsesProcessRelativeCacheDir(t *testing.T) {
	repo := setupRepoWithConfig(t)
	workDir := filepath.Join(repo, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Chdir(workDir)

	var stdout, stderr bytes.Buffer
	if code := NewAppWithOptions(Options{WorkDir: workDir}).Run([]string{"--cache-dir", "local-cache", "setup", "../../vendor/repo"}, &stdout, &stderr); code != 0 {
		t.Fatalf("setup from subdir exit = %d, stderr = %q", code, stderr.String())
	}
	remote := "main_braid_vendor_repo"
	cacheURL := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", remote).Stdout)
	canonicalWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("canonicalize workdir: %v", err)
	}
	if !strings.HasPrefix(cacheURL, filepath.Join(canonicalWorkDir, "local-cache")) {
		t.Fatalf("cache URL = %q, want under process-relative cache dir", cacheURL)
	}
}

func setupRepoWithConfig(t *testing.T) string {
	t.Helper()
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "hello\n")
	revision := testutil.CommitAll(t, upstream, "upstream")

	repo := testutil.InitRepo(t)
	testutil.WriteFile(t, repo, "README.md", "downstream\n")
	testutil.CommitAll(t, repo, "downstream")

	cfg := config.Empty()
	if err := cfg.Add(mirror.Mirror{
		Path:     "vendor/repo",
		URL:      upstream,
		Branch:   "main",
		Revision: revision,
	}); err != nil {
		t.Fatalf("Add mirror config: %v", err)
	}
	if err := cfg.WriteFile(filepath.Join(repo, config.FileName)); err != nil {
		t.Fatalf("Write config: %v", err)
	}
	return repo
}
