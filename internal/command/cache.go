package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"braid/internal/cli"
)

type CacheConfig struct {
	Enabled bool
	Dir     string
}

type EnvLookup func(string) (string, bool)

func ResolveCache(global cli.GlobalOptions, lookup EnvLookup, cwd string) (CacheConfig, error) {
	if global.NoCache && global.CacheDirSet {
		return CacheConfig{}, fmt.Errorf("--no-cache and --cache-dir cannot be used together")
	}
	if global.NoCache {
		return CacheConfig{Enabled: false}, nil
	}
	if global.CacheDirSet {
		dir, err := absolutePath(global.CacheDir, cwd)
		if err != nil {
			return CacheConfig{}, err
		}
		return CacheConfig{Enabled: true, Dir: dir}, nil
	}

	enabled := true
	if value, ok := lookup("BRAID_USE_LOCAL_CACHE"); ok && value != "" && value != "true" && value != "1" {
		enabled = false
	}
	if !enabled {
		return CacheConfig{Enabled: false}, nil
	}

	dir := ""
	if value, ok := lookup("BRAID_LOCAL_CACHE_DIR"); ok {
		dir = value
	} else if home, ok := lookup("HOME"); ok {
		dir = filepath.Join(home, ".braid", "cache")
	} else {
		dir = filepath.Join(".braid", "cache")
	}
	expanded, err := absolutePath(expandHome(dir, lookup), cwd)
	if err != nil {
		return CacheConfig{}, err
	}
	return CacheConfig{Enabled: true, Dir: expanded}, nil
}

func CachePath(cacheDir, url string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", "@", "_")
	return filepath.Join(cacheDir, replacer.Replace(url))
}

func runtimeCache(global cli.GlobalOptions) (CacheConfig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return CacheConfig{}, err
	}
	return ResolveCache(global, os.LookupEnv, cwd)
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
	if value == "~" {
		if home, ok := lookup("HOME"); ok {
			return home
		}
	}
	if strings.HasPrefix(value, "~/") {
		if home, ok := lookup("HOME"); ok {
			return filepath.Join(home, value[2:])
		}
	}
	return value
}
