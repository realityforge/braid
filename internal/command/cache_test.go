package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"braid/internal/cli"
	"braid/internal/gitexec"
	"braid/internal/source"
	"braid/internal/testutil"
)

func TestResolveCacheUsesRepositoryLocalModeByDefault(t *testing.T) {
	got, err := ResolveCache(cli.GlobalOptions{}, envLookup(nil), t.TempDir())
	if err != nil {
		t.Fatalf("ResolveCache returned error: %v", err)
	}
	want := CacheConfig{Enabled: true, Mode: CacheModeRepositoryLocal}
	if got != want {
		t.Fatalf("ResolveCache = %#v, want %#v", got, want)
	}
}

func TestResolveCachePrecedence(t *testing.T) {
	cwd := t.TempDir()

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
			env:    map[string]string{"BRAID_GLOBAL_CACHE_DIR": "ignored"},
			want:   CacheConfig{Enabled: false, Mode: CacheModeDisabled},
		},
		{
			name:    "no cache conflicts with global cache dir",
			global:  cli.GlobalOptions{NoCache: true, GlobalCacheDir: "cache", GlobalCacheDirSet: true},
			wantErr: "--no-cache and --global-cache-dir cannot be used together",
		},
		{
			name:   "global cache dir flag overrides disabling env",
			global: cli.GlobalOptions{GlobalCacheDir: "flag-cache", GlobalCacheDirSet: true},
			env:    map[string]string{"BRAID_USE_LOCAL_CACHE": "false"},
			want:   CacheConfig{Enabled: true, Mode: CacheModeGlobal, Dir: filepath.Join(cwd, "flag-cache")},
		},
		{
			name: "use local cache false disables cache",
			env:  map[string]string{"BRAID_USE_LOCAL_CACHE": "false"},
			want: CacheConfig{Enabled: false, Mode: CacheModeDisabled},
		},
		{
			name: "global env cache dir overrides user cache dir",
			env:  map[string]string{"BRAID_GLOBAL_CACHE_DIR": "env-cache"},
			want: CacheConfig{Enabled: true, Mode: CacheModeGlobal, Dir: filepath.Join(cwd, "env-cache")},
		},
		{
			name: "backslash home expansion",
			env:  map[string]string{"HOME": filepath.Join(cwd, "home"), "BRAID_GLOBAL_CACHE_DIR": `~\braid-cache`},
			want: CacheConfig{Enabled: true, Mode: CacheModeGlobal, Dir: filepath.Join(cwd, "home", "braid-cache")},
		},
		{
			name:    "old local cache env errors",
			env:     map[string]string{"BRAID_LOCAL_CACHE_DIR": "old-cache"},
			wantErr: "BRAID_LOCAL_CACHE_DIR has been replaced by BRAID_GLOBAL_CACHE_DIR",
		},
		{
			name:    "old local cache env errors with no cache",
			global:  cli.GlobalOptions{NoCache: true},
			env:     map[string]string{"BRAID_LOCAL_CACHE_DIR": "old-cache"},
			wantErr: "BRAID_LOCAL_CACHE_DIR has been replaced by BRAID_GLOBAL_CACHE_DIR",
		},
		{
			name: "global env cache dir disabled by use local cache false",
			env:  map[string]string{"BRAID_USE_LOCAL_CACHE": "false", "BRAID_GLOBAL_CACHE_DIR": "env-cache"},
			want: CacheConfig{Enabled: false, Mode: CacheModeDisabled},
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

func TestResolveRepositoryCacheUsesGitPath(t *testing.T) {
	repo := testutil.InitRepo(t)

	got, err := ResolveRepositoryCache(context.Background(), testRepoContext(repo, nil), cli.GlobalOptions{}, envLookup(nil), t.TempDir(), false, nil)
	if err != nil {
		t.Fatalf("ResolveRepositoryCache returned error: %v", err)
	}
	want := CacheConfig{Enabled: true, Mode: CacheModeRepositoryLocal, Dir: filepath.Join(repo, ".git", "braid", "cache")}
	if got != want {
		t.Fatalf("ResolveRepositoryCache = %#v, want %#v", got, want)
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

	got, err := runtimeCache(cli.GlobalOptions{GlobalCacheDir: "cache", GlobalCacheDirSet: true})
	if err != nil {
		t.Fatalf("runtimeCache returned error: %v", err)
	}
	canonicalRealDir, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatalf("canonicalize real dir: %v", err)
	}
	want := CacheConfig{Enabled: true, Mode: CacheModeGlobal, Dir: filepath.Join(canonicalRealDir, "cache")}
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

func TestMirrorCacheIDUsesStableWindowsSafeKey(t *testing.T) {
	m := testSourceMirror(`vendor\repo`, "pkg/lib", `https://example.test/repo?name=bad*chars|"<>@\windows\path`, "main", "", "", false)

	got := MirrorCacheID(m)
	gotAgain := MirrorCacheID(m)

	if got != gotAgain {
		t.Fatalf("MirrorCacheID is not stable: %q then %q", got, gotAgain)
	}
	if len(got) != 32 {
		t.Fatalf("cache id length = %d, want 32", len(got))
	}
	if strings.ContainsAny(got, "<>:\"/\\|?*") {
		t.Fatalf("cache id %q contains Windows-invalid filename characters", got)
	}

	changedPath := m
	changedPath.LocalPath = "vendor/other"
	if other := MirrorCacheID(changedPath); other != got {
		t.Fatalf("MirrorCacheID changed when mirror path changed")
	}

	changedRemotePath := m
	changedRemotePath.UpstreamPath = "cmd/app"
	if other := MirrorCacheID(changedRemotePath); other != got {
		t.Fatalf("MirrorCacheID changed when remote path changed")
	}
	changedName := m
	changedName.Name = "other"
	if other := MirrorCacheID(changedName); other == got {
		t.Fatal("MirrorCacheID did not change with source name")
	}
	changedKind := m
	changedKind.Tracking = source.TagTracking{Tag: "main"}
	if other := MirrorCacheID(changedKind); other == got {
		t.Fatal("MirrorCacheID did not distinguish branch and tag tracking with the same name")
	}
}

func TestRepositoryLocalReadinessRefChangesWithMirrorTopology(t *testing.T) {
	cache := CacheConfig{Enabled: true, Mode: CacheModeRepositoryLocal}
	m := testSourceMirror("vendor/basic", "pkg", "https://example.test/repo.git", "main", "", strings.Repeat("a", 40), false)
	before := cache.RecordedRef(m)
	m.Mirrors = append(m.Mirrors, source.Mirror{LocalPath: "licenses/repo", UpstreamPath: "LICENSE"})
	afterAdd := cache.RecordedRef(m)
	if afterAdd == before {
		t.Fatal("recorded readiness ref did not change after adding a mirror")
	}
	m.Mirrors = []source.Mirror{{LocalPath: "vendor/basic", UpstreamPath: ""}}
	if afterRoot := cache.RecordedRef(m); afterRoot == before || afterRoot == afterAdd {
		t.Fatal("recorded readiness ref did not change after promoting hydration to the root")
	}
}

func TestRepositoryLocalHydrateBranchPinsRefs(t *testing.T) {
	ctx := context.Background()
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")
	repo := testutil.InitRepo(t)
	cache := repositoryLocalCacheForTest(t, ctx, repo)
	m := testSourceMirror("vendor/basic", "", upstream, "main", "", revision, false)

	if err := (MirrorObjectCache{Config: cache}).Hydrate(ctx, m); err != nil {
		t.Fatalf("Hydrate returned error: %v", err)
	}

	cachePath := RepositoryCachePath(cache.Dir, m)
	assertPathIsDir(t, cachePath)
	cacheGit := gitexec.New(cachePath, false, nil)
	assertRefCommit(t, ctx, cacheGit, "refs/heads/main", revision)
	assertRefCommit(t, ctx, cacheGit, cache.RecordedRef(m), revision)
	assertRefCommit(t, ctx, cacheGit, cache.TipRef(m), revision)
}

func TestRepositoryLocalHydrateShortRevisionUsesFullFallback(t *testing.T) {
	ctx := context.Background()
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")
	repo := testutil.InitRepo(t)
	cache := repositoryLocalCacheForTest(t, ctx, repo)
	m := testSourceMirror("vendor/revision", "", upstream, "", "", revision[:12], false)

	if err := (MirrorObjectCache{Config: cache}).Hydrate(ctx, m); err != nil {
		t.Fatalf("Hydrate returned error: %v", err)
	}

	cachePath := RepositoryCachePath(cache.Dir, m)
	cacheGit := gitexec.New(cachePath, false, nil)
	assertRefCommit(t, ctx, cacheGit, cache.RecordedRef(m), revision)
	if _, err := os.Stat(filepath.Join(cachePath, "shallow")); err == nil {
		t.Fatalf("repository-local cache stayed shallow after short revision hydration")
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat shallow file: %v", err)
	}
}

func TestRepositoryLocalHydrateReplacesFileCachePath(t *testing.T) {
	ctx := context.Background()
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")
	repo := testutil.InitRepo(t)
	cache := repositoryLocalCacheForTest(t, ctx, repo)
	m := testSourceMirror("vendor/basic", "", upstream, "main", "", revision, false)
	cachePath := RepositoryCachePath(cache.Dir, m)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("create cache parent: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("not a repository\n"), 0o644); err != nil {
		t.Fatalf("write cache path file: %v", err)
	}

	if err := (MirrorObjectCache{Config: cache}).Hydrate(ctx, m); err != nil {
		t.Fatalf("Hydrate returned error: %v", err)
	}

	assertPathIsDir(t, cachePath)
	cacheGit := gitexec.New(cachePath, false, nil)
	assertRefCommit(t, ctx, cacheGit, "refs/heads/main", revision)
	assertRefCommit(t, ctx, cacheGit, cache.RecordedRef(m), revision)
	assertRefCommit(t, ctx, cacheGit, cache.TipRef(m), revision)
}

func TestFetchMirrorFromRepositoryLocalCacheFetchesBraidRefs(t *testing.T) {
	ctx := context.Background()
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")
	repo := testutil.InitRepo(t)
	cache := repositoryLocalCacheForTest(t, ctx, repo)
	m := testSourceMirror("vendor/basic", "", upstream, "main", "", revision, false)

	if err := (MirrorObjectCache{Config: cache}).Hydrate(ctx, m); err != nil {
		t.Fatalf("Hydrate returned error: %v", err)
	}
	git := gitexec.New(repo, false, nil)
	if err := configureMirrorRemote(ctx, git, m, true, cache); err != nil {
		t.Fatalf("configureMirrorRemote returned error: %v", err)
	}
	if err := fetchMirror(ctx, git, cache, m, progressReporter{}); err != nil {
		t.Fatalf("fetchMirror returned error: %v", err)
	}

	assertRefCommit(t, ctx, git, m.Remote()+"/main", revision)
	assertRefCommit(t, ctx, git, cache.RecordedRef(m), revision)
	if shallow, err := git.Output(ctx, "rev-parse", "--is-shallow-repository"); err != nil {
		t.Fatalf("check shallow repository: %v", err)
	} else if shallow != "true" {
		t.Fatalf("downstream shallow state = %q, want true after fetching from shallow cache", shallow)
	}
}

func TestFetchTagMirrorFromRepositoryLocalCacheUsesRemoteTrackingRef(t *testing.T) {
	ctx := context.Background()
	upstream := testutil.InitRepo(t)
	testutil.WriteFile(t, upstream, "README.md", "base\n")
	revision := testutil.CommitAll(t, upstream, "base")
	testutil.Git(t, upstream, "tag", "v1")
	repo := testutil.InitRepo(t)
	cache := repositoryLocalCacheForTest(t, ctx, repo)
	m := testSourceMirror("vendor/basic", "", upstream, "", "v1", revision, false)

	if err := (MirrorObjectCache{Config: cache}).Hydrate(ctx, m); err != nil {
		t.Fatalf("Hydrate returned error: %v", err)
	}
	git := gitexec.New(repo, false, nil)
	if err := configureMirrorRemote(ctx, git, m, true, cache); err != nil {
		t.Fatalf("configureMirrorRemote returned error: %v", err)
	}
	if err := fetchMirror(ctx, git, cache, m, progressReporter{}); err != nil {
		t.Fatalf("fetchMirror returned error: %v", err)
	}

	assertRefCommit(t, ctx, git, m.LocalRef(), revision)
	assertRefMissing(t, ctx, git, "refs/tags/v1")
}

func TestAcquireCacheLockTimesOut(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "source.git.lock")
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("create lock: %v", err)
	}
	oldTimeout := cacheLockTimeout
	oldRetry := cacheLockRetry
	cacheLockTimeout = 10 * time.Millisecond
	cacheLockRetry = time.Millisecond
	t.Cleanup(func() {
		cacheLockTimeout = oldTimeout
		cacheLockRetry = oldRetry
	})

	_, err := acquireCacheLock(context.Background(), lockPath, "vendor/basic")
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for cache lock for source :vendor/basic") {
		t.Fatalf("acquireCacheLock error = %v, want timeout diagnostic", err)
	}
}

func repositoryLocalCacheForTest(t *testing.T, ctx context.Context, repo string) CacheConfig {
	t.Helper()
	cache, err := ResolveRepositoryCache(ctx, testRepoContext(repo, nil), cli.GlobalOptions{}, envLookup(nil), t.TempDir(), false, nil)
	if err != nil {
		t.Fatalf("ResolveRepositoryCache returned error: %v", err)
	}
	return cache
}

func assertPathIsDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}
