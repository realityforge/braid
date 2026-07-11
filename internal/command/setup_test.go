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
	cwd := rootedTestPath("work")
	home := rootedTestPath("home")

	tests := []struct {
		name    string
		global  cli.GlobalOptions
		env     map[string]string
		want    CacheConfig
		wantErr string
	}{
		{name: "default enabled", env: map[string]string{"HOME": home}, want: CacheConfig{Enabled: true, Mode: CacheModeRepositoryLocal}},
		{name: "env true", env: map[string]string{"HOME": home, "BRAID_USE_LOCAL_CACHE": "true"}, want: CacheConfig{Enabled: true, Mode: CacheModeRepositoryLocal}},
		{name: "env one", env: map[string]string{"HOME": home, "BRAID_USE_LOCAL_CACHE": "1"}, want: CacheConfig{Enabled: true, Mode: CacheModeRepositoryLocal}},
		{name: "env disabled", env: map[string]string{"HOME": home, "BRAID_USE_LOCAL_CACHE": "false"}, want: CacheConfig{Enabled: false, Mode: CacheModeDisabled}},
		{name: "env global cache dir", env: map[string]string{"HOME": home, "BRAID_GLOBAL_CACHE_DIR": "~/custom"}, want: CacheConfig{Enabled: true, Mode: CacheModeGlobal, Dir: filepath.Join(home, "custom")}},
		{name: "flag no cache", global: cli.GlobalOptions{NoCache: true}, env: map[string]string{"HOME": home}, want: CacheConfig{Enabled: false, Mode: CacheModeDisabled}},
		{name: "flag global cache dir", global: cli.GlobalOptions{GlobalCacheDir: "rel-cache", GlobalCacheDirSet: true}, env: map[string]string{"HOME": home, "BRAID_USE_LOCAL_CACHE": "false"}, want: CacheConfig{Enabled: true, Mode: CacheModeGlobal, Dir: filepath.Join(cwd, "rel-cache")}},
		{name: "invalid both flags", global: cli.GlobalOptions{NoCache: true, GlobalCacheDir: "cache", GlobalCacheDirSet: true}, env: map[string]string{"HOME": home}, wantErr: "--no-cache and --global-cache-dir cannot be used together"},
		{name: "old local cache dir env", env: map[string]string{"HOME": home, "BRAID_LOCAL_CACHE_DIR": ""}, wantErr: "BRAID_LOCAL_CACHE_DIR has been replaced by BRAID_GLOBAL_CACHE_DIR"},
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

func rootedTestPath(elem ...string) string {
	root := string(filepath.Separator)
	if wd, err := os.Getwd(); err == nil {
		root = filepath.VolumeName(wd) + root
	}
	parts := append([]string{root}, elem...)
	return filepath.Join(parts...)
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
		if result := testutil.Git(t, repo, "ls-remote", remote, "refs/heads/main"); strings.TrimSpace(result.Stdout) == "" {
			t.Fatalf("remote %q was not hydrated", remote)
		}
	}
}

func TestSetupCommandCreatesAndReusesRemotes(t *testing.T) {
	repo := setupRepoWithConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"setup"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", code, stderr.String())
	}

	remote := "main_braid_vendor_repo"
	firstURL := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", remote).Stdout)
	canonicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("canonicalize repo path: %v", err)
	}
	if !strings.HasPrefix(firstURL, filepath.Join(canonicalRepo, ".git", "braid", "cache")) {
		t.Fatalf("remote URL = %q, want repository-local cache path", firstURL)
	}
	if result := testutil.Git(t, repo, "ls-remote", remote, "refs/heads/main"); strings.TrimSpace(result.Stdout) == "" {
		t.Fatalf("remote %q was not hydrated", remote)
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

func TestSetupCommandRehydratesExistingRepositoryLocalCachePath(t *testing.T) {
	repo := setupRepoWithConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	if code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"setup", "vendor/repo"}, &stdout, &stderr); code != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", code, stderr.String())
	}

	remote := "main_braid_vendor_repo"
	cacheURL := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", remote).Stdout)
	if err := os.RemoveAll(cacheURL); err != nil {
		t.Fatalf("remove cache: %v", err)
	}
	if err := os.MkdirAll(cacheURL, 0o755); err != nil {
		t.Fatalf("create unusable cache dir: %v", err)
	}

	stderr.Reset()
	if code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"setup", "vendor/repo"}, &stdout, &stderr); code != 0 {
		t.Fatalf("second setup exit = %d, stderr = %q", code, stderr.String())
	}
	if result := testutil.Git(t, repo, "ls-remote", remote, "refs/heads/main"); strings.TrimSpace(result.Stdout) == "" {
		t.Fatalf("remote %q was not rehydrated", remote)
	}
}

func TestSetupCommandRehydratesStaleRepositoryLocalRevision(t *testing.T) {
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	baseRevision := testutil.CommitAll(t, upstream, "base")

	repo := testutil.InitRepo(t)
	testutil.WriteFile(t, repo, "README.md", "downstream\n")
	testutil.CommitAll(t, repo, "downstream")

	m := mirror.Mirror{Path: "vendor/repo", URL: upstream, Branch: "main", Revision: baseRevision}
	writeSingleMirrorConfig(t, repo, m)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	if code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"setup", "vendor/repo"}, &stdout, &stderr); code != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", code, stderr.String())
	}
	remote := "main_braid_vendor_repo"
	cacheURL := strings.TrimSpace(testutil.Git(t, repo, "remote", "get-url", remote).Stdout)

	testutil.WriteFile(t, upstream, "README.md", "updated\n")
	updatedRevision := testutil.CommitAll(t, upstream, "updated")
	m.Revision = updatedRevision
	writeSingleMirrorConfig(t, repo, m)

	stderr.Reset()
	if code := NewAppWithOptions(Options{WorkDir: repo, ConfigRoot: repo}).Run([]string{"setup", "vendor/repo"}, &stdout, &stderr); code != 0 {
		t.Fatalf("second setup exit = %d, stderr = %q", code, stderr.String())
	}
	recordedRef := (CacheConfig{}).RecordedRef(m)
	got := strings.TrimSpace(testutil.Git(t, cacheURL, "rev-parse", recordedRef+"^{commit}").Stdout)
	if got != updatedRevision {
		t.Fatalf("%s = %s, want %s", recordedRef, got, updatedRevision)
	}
}

func TestSetupCommandHonorsNoCacheAndGlobalCacheDir(t *testing.T) {
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
	if code := app.Run([]string{"--global-cache-dir", "local-cache", "setup", "vendor/repo"}, &stdout, &stderr); code != 0 {
		t.Fatalf("setup --global-cache-dir exit = %d, stderr = %q", code, stderr.String())
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

func TestSetupCommandFromSubdirectoryUsesProcessRelativeGlobalCacheDir(t *testing.T) {
	repo := setupRepoWithConfig(t)
	workDir := filepath.Join(repo, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Chdir(workDir)

	var stdout, stderr bytes.Buffer
	if code := NewAppWithOptions(Options{WorkDir: workDir}).Run([]string{"--global-cache-dir", "local-cache", "setup", "../../vendor/repo"}, &stdout, &stderr); code != 0 {
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
