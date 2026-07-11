package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"braid/internal/cli"
	"braid/internal/gitexec"
	"braid/internal/mirror"
)

type CacheMode string

const (
	CacheModeDisabled        CacheMode = "disabled"
	CacheModeRepositoryLocal CacheMode = "repository-local"
	CacheModeGlobal          CacheMode = "global"
	mirrorCacheIDBytes                 = 16
)

type CacheConfig struct {
	Enabled bool
	Mode    CacheMode
	Dir     string
}

type EnvLookup func(string) (string, bool)

type MirrorObjectCache struct {
	Config  CacheConfig
	Verbose bool
	Trace   ioWriter
}

// Repository-local caches are per-mirror bare repositories owned by Braid. They
// are intentionally disposable: a fresh clone or a deleted .git/braid/cache tree
// can rebuild them only while the upstream can still serve the recorded commit.
// Full 40-hex revisions are fetched directly at depth 1; short or symbolic
// requested revisions force that mirror cache to a full fetch so resolution
// happens in the upstream/cache namespace rather than in downstream refs.
//
// A shallow bare cache cannot be fetched into the downstream repository unless
// Git is allowed to update downstream shallow metadata, so repo-local mirror
// fetches use --update-shallow. This keeps the cache shallow but can make the
// downstream repository report as shallow even though the shallow roots are
// Braid-owned mirror commits rather than the downstream branch history.

type fetchGit interface {
	Fetch(context.Context, ...string) error
}

type ioWriter interface {
	Write([]byte) (int, error)
}

var (
	cacheLockTimeout = 30 * time.Second
	cacheLockRetry   = 50 * time.Millisecond
)

func ResolveCache(global cli.GlobalOptions, lookup EnvLookup, cwd string) (CacheConfig, error) {
	if _, ok := lookup("BRAID_LOCAL_CACHE_DIR"); ok {
		return CacheConfig{}, fmt.Errorf("BRAID_LOCAL_CACHE_DIR has been replaced by BRAID_GLOBAL_CACHE_DIR")
	}
	if global.NoCache && global.GlobalCacheDirSet {
		return CacheConfig{}, fmt.Errorf("--no-cache and --global-cache-dir cannot be used together")
	}
	if global.NoCache {
		return CacheConfig{Enabled: false, Mode: CacheModeDisabled}, nil
	}
	if global.GlobalCacheDirSet {
		dir, err := absolutePath(global.GlobalCacheDir, cwd)
		if err != nil {
			return CacheConfig{}, err
		}
		return CacheConfig{Enabled: true, Mode: CacheModeGlobal, Dir: dir}, nil
	}

	enabled := true
	if value, ok := lookup("BRAID_USE_LOCAL_CACHE"); ok && value != "" && value != "true" && value != "1" {
		enabled = false
	}
	if !enabled {
		return CacheConfig{Enabled: false, Mode: CacheModeDisabled}, nil
	}

	if value, ok := lookup("BRAID_GLOBAL_CACHE_DIR"); ok {
		expanded, err := absolutePath(expandHome(value, lookup), cwd)
		if err != nil {
			return CacheConfig{}, err
		}
		return CacheConfig{Enabled: true, Mode: CacheModeGlobal, Dir: expanded}, nil
	}

	return CacheConfig{Enabled: true, Mode: CacheModeRepositoryLocal}, nil
}

func ResolveRepositoryCache(ctx context.Context, repo RepoContext, global cli.GlobalOptions, lookup EnvLookup, cwd string, verbose bool, trace ioWriter) (CacheConfig, error) {
	cache, err := ResolveCache(global, lookup, cwd)
	if err != nil {
		return CacheConfig{}, err
	}
	if !cache.Enabled || cache.Mode != CacheModeRepositoryLocal {
		return cache, nil
	}
	gitPath, err := gitexec.New(repo.GitWorkTreeRoot, verbose, trace).RepoFilePath(ctx, "braid/cache")
	if err != nil {
		return CacheConfig{}, err
	}
	dir, err := gitRepoOSPath(gitPath, repo.GitWorkTreeRoot)
	if err != nil {
		return CacheConfig{}, err
	}
	cache.Dir = dir
	return cache, nil
}

func CachePath(cacheDir, url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(cacheDir, hex.EncodeToString(sum[:]))
}

func RepositoryCachePath(cacheDir string, m mirror.Mirror) string {
	return filepath.Join(cacheDir, MirrorCacheID(m)+".git")
}

func MirrorCacheID(m mirror.Mirror) string {
	parts := []string{
		m.URL,
		m.Path,
		m.RemotePath,
		m.TrackingName(),
		m.Branch,
		m.Tag,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	// The ID appears in both the cache directory and Braid refs; keep it short
	// enough for default Windows path limits while retaining 128 bits of hash.
	return hex.EncodeToString(sum[:mirrorCacheIDBytes])
}

func runtimeCache(global cli.GlobalOptions) (CacheConfig, error) {
	cwd, err := currentWorkingDir()
	if err != nil {
		return CacheConfig{}, err
	}
	return ResolveCache(global, os.LookupEnv, cwd)
}

func runtimeCacheForRepo(ctx context.Context, repo RepoContext, global cli.GlobalOptions, verbose bool, trace ioWriter) (CacheConfig, error) {
	cwd, err := currentWorkingDir()
	if err != nil {
		return CacheConfig{}, err
	}
	return ResolveRepositoryCache(ctx, repo, global, os.LookupEnv, cwd, verbose, trace)
}

func (cache CacheConfig) RemoteURL(m mirror.Mirror) string {
	if !cache.Enabled {
		return m.URL
	}
	switch cache.Mode {
	case CacheModeGlobal:
		return CachePath(cache.Dir, m.URL)
	case CacheModeRepositoryLocal:
		return RepositoryCachePath(cache.Dir, m)
	default:
		return m.URL
	}
}

func (cache CacheConfig) RecordedRef(m mirror.Mirror) string {
	return "refs/braid/recorded/" + MirrorCacheID(m)
}

func (cache CacheConfig) RequestedRef(m mirror.Mirror) string {
	return "refs/braid/requested/" + MirrorCacheID(m)
}

func (cache CacheConfig) TipRef(m mirror.Mirror) string {
	return "refs/braid/tip/" + MirrorCacheID(m)
}

func cacheResolveRecordedRevision(cache CacheConfig, m mirror.Mirror, requested string) string {
	if requested != "" && cache.Enabled && cache.Mode == CacheModeRepositoryLocal {
		return cache.RecordedRef(m)
	}
	return requested
}

func cacheResolveRequestedRevision(cache CacheConfig, m mirror.Mirror, requested string) string {
	if requested != "" && cache.Enabled && cache.Mode == CacheModeRepositoryLocal {
		return cache.RequestedRef(m)
	}
	return requested
}

func (cache MirrorObjectCache) Hydrate(ctx context.Context, m mirror.Mirror, extraRevisions ...string) error {
	if !cache.Config.Enabled {
		return nil
	}
	switch cache.Config.Mode {
	case CacheModeGlobal:
		return cache.hydrateGlobal(ctx, m)
	case CacheModeRepositoryLocal:
		return cache.hydrateRepositoryLocal(ctx, m, extraRevisions...)
	default:
		return nil
	}
}

func fetchCache(ctx context.Context, cache CacheConfig, m mirror.Mirror, verbose bool, progress progressReporter, trace ioWriter, extraRevisions ...string) error {
	if !cache.Enabled {
		return nil
	}
	return runProgress(
		progress,
		fmt.Sprintf("Braid: updating cache for mirror %s", m.Path),
		fmt.Sprintf("Braid: updated cache for mirror %s", m.Path),
		func() error {
			return MirrorObjectCache{Config: cache, Verbose: verbose, Trace: trace}.Hydrate(ctx, m, extraRevisions...)
		},
	)
}

func fetchMirror(ctx context.Context, git fetchGit, cache CacheConfig, m mirror.Mirror, progress progressReporter) error {
	return runProgress(
		progress,
		fmt.Sprintf("Braid: fetching mirror %s", m.Path),
		fmt.Sprintf("Braid: fetched mirror %s", m.Path),
		func() error {
			if cache.Enabled && cache.Mode == CacheModeRepositoryLocal {
				args := []string{"--update-shallow", "-n", m.Remote()}
				if m.Branch != "" {
					args = append(args, "+refs/heads/"+m.Branch+":refs/remotes/"+m.Remote()+"/"+m.Branch)
				}
				if m.Tag != "" {
					args = append(args, "+refs/tags/"+m.Tag+":refs/tags/"+m.Tag)
				}
				args = append(args, "+refs/braid/*:refs/braid/*")
				return git.Fetch(ctx, args...)
			}
			if err := git.Fetch(ctx, "-n", m.Remote()); err != nil {
				return err
			}
			if m.Tag != "" {
				return git.Fetch(ctx, "-n", m.Remote(), "+refs/tags/"+m.Tag+":refs/tags/"+m.Tag)
			}
			return nil
		},
	)
}

func (cache MirrorObjectCache) hydrateGlobal(ctx context.Context, m mirror.Mirror) error {
	cachePath := CachePath(cache.Config.Dir, m.URL)
	if _, err := os.Stat(filepath.Join(cachePath, ".git")); err == nil {
		if err := os.RemoveAll(cachePath); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if _, err := os.Stat(cachePath); err == nil {
		return gitexec.New(cachePath, cache.Verbose, cache.Trace).Fetch(ctx)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(cache.Config.Dir, 0o755); err != nil {
		return err
	}
	return gitexec.New(".", cache.Verbose, cache.Trace).CloneMirror(ctx, m.URL, cachePath)
}

func (cache MirrorObjectCache) hydrateRepositoryLocal(ctx context.Context, m mirror.Mirror, extraRevisions ...string) error {
	cachePath := RepositoryCachePath(cache.Config.Dir, m)
	lockPath := cachePath + ".lock"
	release, err := acquireCacheLock(ctx, lockPath, m.Path)
	if err != nil {
		return err
	}
	defer release()

	if err := cache.ensureRepositoryLocalCache(ctx, cachePath); err != nil {
		return err
	}
	cacheGit := gitexec.New(cachePath, cache.Verbose, cache.Trace)
	full := false

	recordedRevision := strings.TrimSpace(m.Revision)
	if recordedRevision != "" {
		if isFullObjectID(recordedRevision) {
			if err := cache.fetchFullObjectID(ctx, cacheGit, m.URL, recordedRevision, cache.Config.RecordedRef(m)); err != nil {
				full = true
				if err := cache.fetchFullMirror(ctx, cachePath, cacheGit, m); err != nil {
					return err
				}
			}
		} else {
			full = true
			if err := cache.fetchFullMirror(ctx, cachePath, cacheGit, m); err != nil {
				return err
			}
		}
		resolved, err := cacheGit.RevParse(ctx, recordedRevision+"^{commit}")
		if err != nil {
			return unavailableRecordedRevisionError(m, recordedRevision)
		}
		if err := cacheGit.UpdateRef(ctx, cache.Config.RecordedRef(m), resolved); err != nil {
			return err
		}
	}

	for _, revision := range extraRevisions {
		revision = strings.TrimSpace(revision)
		if revision == "" {
			continue
		}
		if isFullObjectID(revision) {
			if err := cache.fetchFullObjectID(ctx, cacheGit, m.URL, revision, cache.Config.RequestedRef(m)); err != nil {
				full = true
				if err := cache.fetchFullMirror(ctx, cachePath, cacheGit, m); err != nil {
					return err
				}
			}
		} else if !full {
			full = true
			if err := cache.fetchFullMirror(ctx, cachePath, cacheGit, m); err != nil {
				return err
			}
		}
		resolved, err := cacheGit.RevParse(ctx, revision+"^{commit}")
		if err != nil {
			return unavailableRecordedRevisionError(m, revision)
		}
		if err := cacheGit.UpdateRef(ctx, cache.Config.RequestedRef(m), resolved); err != nil {
			return err
		}
	}

	switch {
	case m.Branch != "":
		if !full {
			if err := cacheGit.Fetch(ctx, "--depth=1", m.URL, "+refs/heads/"+m.Branch+":refs/heads/"+m.Branch); err != nil {
				return err
			}
		}
		resolved, err := cacheGit.RevParse(ctx, "refs/heads/"+m.Branch+"^{commit}")
		if err != nil {
			return err
		}
		return cacheGit.UpdateRef(ctx, cache.Config.TipRef(m), resolved)
	case m.Tag != "":
		if !full {
			if err := cacheGit.Fetch(ctx, "--depth=1", m.URL, "+refs/tags/"+m.Tag+":refs/tags/"+m.Tag); err != nil {
				return err
			}
		}
		resolved, err := cacheGit.RevParse(ctx, "refs/tags/"+m.Tag+"^{commit}")
		if err != nil {
			return err
		}
		return cacheGit.UpdateRef(ctx, cache.Config.TipRef(m), resolved)
	}
	return nil
}

func (cache MirrorObjectCache) ensureRepositoryLocalCache(ctx context.Context, cachePath string) error {
	if info, err := os.Stat(cachePath); err == nil {
		replace := !info.IsDir()
		if !replace {
			if _, err := os.Stat(filepath.Join(cachePath, ".git")); err == nil {
				replace = true
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if !replace {
			bare, err := isBareRepository(ctx, cachePath, cache.Verbose, cache.Trace)
			if err == nil && bare {
				return nil
			}
			replace = true
		}
		if replace {
			if err := os.RemoveAll(cachePath); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	tempPath, err := os.MkdirTemp(filepath.Dir(cachePath), ".tmp-"+filepath.Base(cachePath)+"-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempPath)
		}
	}()
	if err := gitexec.New(".", cache.Verbose, cache.Trace).InitBare(ctx, tempPath); err != nil {
		return err
	}
	if err := os.Rename(tempPath, cachePath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func isBareRepository(ctx context.Context, path string, verbose bool, trace ioWriter) (bool, error) {
	out, err := gitexec.New(path, verbose, trace).Output(ctx, "rev-parse", "--is-bare-repository")
	if err != nil {
		return false, err
	}
	return out == "true", nil
}

func (cache MirrorObjectCache) fetchFullObjectID(ctx context.Context, git gitexec.Git, url, objectID, ref string) error {
	return git.Fetch(ctx, "--depth=1", url, objectID+":"+ref)
}

func (cache MirrorObjectCache) fetchFullMirror(ctx context.Context, cachePath string, git gitexec.Git, m mirror.Mirror) error {
	args := []string{"--prune"}
	if _, err := os.Stat(filepath.Join(cachePath, "shallow")); err == nil {
		args = append(args, "--unshallow")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	args = append(args, m.URL, "+refs/*:refs/*")
	return git.Fetch(ctx, args...)
}

func acquireCacheLock(ctx context.Context, lockPath, mirrorPath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	deadline := time.NewTimer(cacheLockTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(cacheLockRetry)
	defer ticker.Stop()

	for {
		if err := os.Mkdir(lockPath, 0o700); err == nil {
			return func() { _ = os.RemoveAll(lockPath) }, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("timed out waiting for cache lock for mirror %s at %s; another Braid process may be updating the cache", mirrorPath, lockPath)
		case <-ticker.C:
		}
	}
}

func unavailableRecordedRevisionError(m mirror.Mirror, revision string) error {
	return fmt.Errorf("recorded revision %s for mirror %s is unavailable from upstream %s; the repository-local cache may have been deleted or the upstream history may have been rewritten", revision, m.Path, m.URL)
}

func isFullObjectID(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			continue
		}
		return false
	}
	return true
}

func currentWorkingDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(cwd)
}

func absolutePath(value, cwd string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("cache directory cannot be empty")
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	return filepath.Abs(filepath.Join(cwd, value))
}

func expandHome(value string, lookup EnvLookup) string {
	home, ok := homeDir(lookup)
	if !ok {
		return value
	}
	if value == "~" {
		return home
	}
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		return filepath.Join(home, filepath.FromSlash(strings.ReplaceAll(value[2:], `\`, "/")))
	}
	return value
}

func homeDir(lookup EnvLookup) (string, bool) {
	if home, ok := lookup("HOME"); ok && home != "" {
		return home, true
	}
	if home, ok := lookup("USERPROFILE"); ok && home != "" {
		return home, true
	}
	return "", false
}
