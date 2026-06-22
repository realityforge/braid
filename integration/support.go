package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	defaultName  = "Braid Integration"
	defaultEmail = "braid-integration@example.invalid"
)

func expectedBraidVersion() string {
	if version := os.Getenv("BRAID_EXPECTED_VERSION"); version != "" {
		return version
	}
	return "0.0.0-dev"
}

type processEnv struct {
	values map[string]string
}

func newProcessEnv(t *testing.T, root string) processEnv {
	t.Helper()
	dirs := []string{
		filepath.Join(root, "home"),
		filepath.Join(root, "userprofile"),
		filepath.Join(root, "xdg-config"),
		filepath.Join(root, "xdg-cache"),
		filepath.Join(root, "appdata"),
		filepath.Join(root, "local-appdata"),
		filepath.Join(root, "tmp"),
		filepath.Join(root, "braid-cache"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create env dir %s: %v", dir, err)
		}
	}
	globalConfig := filepath.Join(root, "xdg-config", "gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0o644); err != nil {
		t.Fatalf("write global gitconfig: %v", err)
	}

	values := map[string]string{
		"APPDATA":               filepath.Join(root, "appdata"),
		"BRAID_LOCAL_CACHE_DIR": filepath.Join(root, "braid-cache"),
		"GIT_AUTHOR_DATE":       "2001-02-03T04:05:06Z",
		"GIT_COMMITTER_DATE":    "2001-02-03T04:05:06Z",
		"GIT_CONFIG_GLOBAL":     globalConfig,
		"GIT_CONFIG_NOSYSTEM":   "1",
		"GIT_TERMINAL_PROMPT":   "0",
		"HOME":                  filepath.Join(root, "home"),
		"LANG":                  "C",
		"LC_ALL":                "C",
		"LOCALAPPDATA":          filepath.Join(root, "local-appdata"),
		"TEMP":                  filepath.Join(root, "tmp"),
		"TMP":                   filepath.Join(root, "tmp"),
		"TMPDIR":                filepath.Join(root, "tmp"),
		"USERPROFILE":           filepath.Join(root, "userprofile"),
		"XDG_CACHE_HOME":        filepath.Join(root, "xdg-cache"),
		"XDG_CONFIG_HOME":       filepath.Join(root, "xdg-config"),
	}
	if runtime.GOOS == "windows" {
		if value, ok := os.LookupEnv("Path"); ok {
			values["Path"] = value
		} else if value, ok := os.LookupEnv("PATH"); ok {
			values["Path"] = value
		}
		for _, key := range []string{"COMSPEC", "PATHEXT", "SystemRoot", "WINDIR"} {
			if value, ok := os.LookupEnv(key); ok {
				values[key] = value
			}
		}
	} else if value, ok := os.LookupEnv("PATH"); ok {
		values["PATH"] = value
	}
	return processEnv{values: values}
}

func (e processEnv) list() []string {
	keys := make([]string, 0, len(e.values))
	for key := range e.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+e.values[key])
	}
	return env
}

func (e processEnv) with(key, value string) processEnv {
	values := make(map[string]string, len(e.values)+1)
	for existingKey, existingValue := range e.values {
		values[existingKey] = existingValue
	}
	values[key] = value
	return processEnv{values: values}
}

func (e processEnv) without(keys ...string) processEnv {
	values := make(map[string]string, len(e.values))
	for existingKey, existingValue := range e.values {
		values[existingKey] = existingValue
	}
	for _, key := range keys {
		delete(values, key)
	}
	return processEnv{values: values}
}

func (e processEnv) braidCacheDir() string {
	return e.values["BRAID_LOCAL_CACHE_DIR"]
}

func (e processEnv) defaultBraidCacheDir() string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(e.values["HOME"], "Library", "Caches", "braid")
	case "windows":
		return filepath.Join(e.values["LOCALAPPDATA"], "braid")
	default:
		return filepath.Join(e.values["XDG_CACHE_HOME"], "braid")
	}
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runBraid(t *testing.T, env processEnv, workdir, braid string, args ...string) commandResult {
	t.Helper()
	return runProcess(t, env, workdir, braid, args...)
}

func gitOK(t *testing.T, env processEnv, workdir string, args ...string) commandResult {
	t.Helper()
	result := runProcess(t, env, workdir, "git", args...)
	if result.exitCode != 0 {
		t.Fatalf("git %v failed in %s with exit %d\nstdout:\n%s\nstderr:\n%s", args, workdir, result.exitCode, result.stdout, result.stderr)
	}
	return result
}

func runProcess(t *testing.T, env processEnv, workdir, executable string, args ...string) commandResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = workdir
	cmd.Env = env.list()
	if runtime.GOOS != "windows" {
		if _, ok := env.values["PWD"]; !ok {
			cmd.Env = append(cmd.Env, "PWD="+workdir)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %v timed out in %s", executable, args, workdir)
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("%s %v failed to start in %s: %v", executable, args, workdir, err)
		}
	}
	return commandResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

func braidBinary(t *testing.T) string {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	candidates := []string{
		"_main/cmd/braid/braid_/braid" + suffix,
		"braid/cmd/braid/braid_/braid" + suffix,
		"cmd/braid/braid_/braid" + suffix,
	}
	if manifest := os.Getenv("RUNFILES_MANIFEST_FILE"); manifest != "" {
		data, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatalf("read runfiles manifest %s: %v", manifest, err)
		}
		entries := map[string]string{}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}
			logical, actual, ok := strings.Cut(line, " ")
			if ok {
				entries[logical] = actual
			}
		}
		for _, candidate := range candidates {
			if path, ok := entries[candidate]; ok {
				return path
			}
		}
	}
	for _, rootEnv := range []string{"RUNFILES_DIR", "TEST_SRCDIR"} {
		if dir := os.Getenv(rootEnv); dir != "" {
			for _, candidate := range candidates {
				path := filepath.Join(dir, filepath.FromSlash(candidate))
				if _, err := os.Stat(path); err == nil {
					return path
				}
			}
		}
	}
	t.Fatalf("could not locate //cmd/braid:braid in Bazel runfiles; checked %v", candidates)
	return ""
}

func initRepo(t *testing.T, env processEnv, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
		t.Fatalf("create repo parent: %v", err)
	}
	gitOK(t, env, filepath.Dir(repo), "init", "--initial-branch=main", repo)
	gitOK(t, env, repo, "config", "--local", "user.name", defaultName)
	gitOK(t, env, repo, "config", "--local", "user.email", defaultEmail)
	gitOK(t, env, repo, "config", "--local", "commit.gpgsign", "false")
}

func processWorkingDir(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func commitAll(t *testing.T, env processEnv, repo, message string) string {
	t.Helper()
	gitOK(t, env, repo, "add", ".")
	gitOK(t, env, repo, "commit", "--no-verify", "-m", message)
	return gitOutput(t, env, repo, "rev-parse", "HEAD")
}

func gitOutput(t *testing.T, env processEnv, repo string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(gitOK(t, env, repo, args...).stdout)
}

func writeFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", relativePath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relativePath, err)
	}
}

func readFile(t *testing.T, root, relativePath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(data)
}

func writeConfig(t *testing.T, repo string, mirrors map[string]configMirror) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, ".braids.json"), []byte(expectedConfigRaw(t, mirrors)), 0o644); err != nil {
		t.Fatalf("write .braids.json: %v", err)
	}
}

type configFile struct {
	ConfigVersion int                     `json:"config_version"`
	Mirrors       map[string]configMirror `json:"mirrors"`
}

type configMirror struct {
	URL      string `json:"url"`
	Branch   string `json:"branch,omitempty"`
	Path     string `json:"path,omitempty"`
	Tag      string `json:"tag,omitempty"`
	Revision string `json:"revision"`
}

func expectedConfigRaw(t *testing.T, mirrors map[string]configMirror) string {
	t.Helper()
	data, err := json.MarshalIndent(configFile{ConfigVersion: 1, Mirrors: mirrors}, "", "  ")
	if err != nil {
		t.Fatalf("marshal expected config: %v", err)
	}
	return string(data) + "\n"
}

func assertConfigRaw(t *testing.T, repo string, mirrors map[string]configMirror) {
	t.Helper()
	path := filepath.Join(repo, ".braids.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .braids.json: %v", err)
	}
	want := expectedConfigRaw(t, mirrors)
	if string(data) != want {
		t.Fatalf(".braids.json raw =\n%s\nwant:\n%s", string(data), want)
	}
	var parsed configFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse .braids.json: %v", err)
	}
	if parsed.ConfigVersion != 1 || len(parsed.Mirrors) != len(mirrors) {
		t.Fatalf(".braids.json semantic parse = %#v, want version 1 and %d mirrors", parsed, len(mirrors))
	}
}

func cachePath(cacheDir, url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(cacheDir, hex.EncodeToString(sum[:]))
}

func remoteName(tracking, localPath string) string {
	var b strings.Builder
	for _, r := range tracking + "_braid_" + localPath {
		if r == '-' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func editorCommand(t *testing.T, root, message string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(root, "editor.cmd")
		body := "@echo off\r\n> \"%~1\" echo " + message + "\r\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write editor script: %v", err)
		}
		return `"` + path + `"`
	}
	path := filepath.Join(root, "editor.sh")
	body := "#!/bin/sh\nprintf '%s\\n' " + shellQuote(message) + " > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write editor script: %v", err)
	}
	return shellQuote(path)
}

func capturingEditorCommand(t *testing.T, root, message string) (string, string) {
	t.Helper()
	capture := filepath.Join(root, "captured-commit-message.txt")
	if runtime.GOOS == "windows" {
		path := filepath.Join(root, "capturing-editor.cmd")
		body := "@echo off\r\n" +
			"setlocal\r\n" +
			"if \"%~1\"==\"\" (\r\n" +
			"  echo capturing editor expected Git commit message file argument 1>&2\r\n" +
			"  exit /b 2\r\n" +
			")\r\n" +
			"set \"message_file=%~1\"\r\n" +
			"copy /Y \"%message_file%\" \"" + capture + "\" >NUL\r\n" +
			"if errorlevel 1 exit /b %errorlevel%\r\n" +
			"> \"%message_file%\" echo " + message + "\r\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write capturing editor script: %v", err)
		}
		return capture, `"` + path + `"`
	}
	path := filepath.Join(root, "capturing-editor.sh")
	body := "#!/bin/sh\ncp \"$1\" " + shellQuote(capture) + " || exit 1\nprintf '%s\\n' " + shellQuote(message) + " > \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write capturing editor script: %v", err)
	}
	return capture, shellQuote(path)
}

func failingEditorCommand(t *testing.T, root string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(root, "failing-editor.cmd")
		if err := os.WriteFile(path, []byte("@echo off\r\nexit /b 1\r\n"), 0o644); err != nil {
			t.Fatalf("write failing editor script: %v", err)
		}
		return `"` + path + `"`
	}
	path := filepath.Join(root, "failing-editor.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write failing editor script: %v", err)
	}
	return shellQuote(path)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeFailingPreCommitHook(t *testing.T, repo string) {
	t.Helper()
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	body := "#!/bin/sh\nexit 1\n"
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		body = "@echo off\r\nexit /b 1\r\n"
		mode = 0o644
	}
	if err := os.WriteFile(hook, []byte(body), mode); err != nil {
		t.Fatalf("write pre-commit hook: %v", err)
	}
}

func assertResult(t *testing.T, result commandResult, exitCode int, stdout, stderr string) {
	t.Helper()
	assertExit(t, result, exitCode)
	if result.stdout != stdout || result.stderr != stderr {
		t.Fatalf("result stdout/stderr = %q / %q, want %q / %q", result.stdout, result.stderr, stdout, stderr)
	}
}

func assertExit(t *testing.T, result commandResult, want int) {
	t.Helper()
	if result.exitCode != want {
		t.Fatalf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", result.exitCode, want, result.stdout, result.stderr)
	}
}

func assertEmpty(t *testing.T, label, value string) {
	t.Helper()
	if value != "" {
		t.Fatalf("%s = %q, want empty", label, value)
	}
}

func assertContains(t *testing.T, value, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("value does not contain %q:\n%s", want, value)
	}
}

func assertNotContains(t *testing.T, value, unwanted string) {
	t.Helper()
	if strings.Contains(value, unwanted) {
		t.Fatalf("value contains %q:\n%s", unwanted, value)
	}
}

func assertFile(t *testing.T, root, relativePath, want string) {
	t.Helper()
	got := readFile(t, root, relativePath)
	if got != want {
		t.Fatalf("%s = %q, want %q", relativePath, got, want)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, root, relativePath string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(relativePath)))
	if !os.IsNotExist(err) {
		t.Fatalf("%s exists or returned unexpected stat error: %v", relativePath, err)
	}
}

func assertRemoteURL(t *testing.T, env processEnv, repo, remote, want string) {
	t.Helper()
	got, ok := remoteURL(t, env, repo, remote)
	if !ok {
		t.Fatalf("remote %q missing, want URL %q", remote, want)
	}
	if got != want {
		t.Fatalf("remote %q URL = %q, want %q", remote, got, want)
	}
}

func remoteURL(t *testing.T, env processEnv, repo, remote string) (string, bool) {
	t.Helper()
	result := runProcess(t, env, repo, "git", "config", "--get", "remote."+remote+".url")
	if result.exitCode == 1 {
		return "", false
	}
	if result.exitCode != 0 {
		t.Fatalf("git config remote %q failed with exit %d\nstdout:\n%s\nstderr:\n%s", remote, result.exitCode, result.stdout, result.stderr)
	}
	return strings.TrimSpace(result.stdout), true
}

func assertNoRemote(t *testing.T, env processEnv, repo, remote string) {
	t.Helper()
	if got, ok := remoteURL(t, env, repo, remote); ok {
		t.Fatalf("remote %q exists with URL %q, want absent", remote, got)
	}
}

func assertClean(t *testing.T, env processEnv, repo string) {
	t.Helper()
	if status := strings.TrimSpace(gitOK(t, env, repo, "status", "--porcelain").stdout); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
}

func assertLatestCommit(t *testing.T, env processEnv, repo, identity, subject string) {
	t.Helper()
	got := strings.TrimSpace(gitOK(t, env, repo, "log", "-1", "--pretty=%an <%ae>|%cn <%ce>|%s").stdout)
	want := identity + "|" + identity + "|" + subject
	if got != want {
		t.Fatalf("latest commit metadata = %q, want %q", got, want)
	}
}

func shortRevision(revision string) string {
	if len(revision) < 7 {
		return revision
	}
	return revision[:7]
}
