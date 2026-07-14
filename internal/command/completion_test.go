package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"braid/internal/clitest"
	"braid/internal/testutil"
)

func TestCompletionBashPrintsScript(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: t.TempDir()}).Run([]string{"completion", "bash"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("completion bash exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	assertContains(t, stdout.String(), "_braid()")
	assertContains(t, stdout.String(), `"${COMP_WORDS[0]}" __complete bash -- "${COMP_WORDS[@]:1}"`)
	assertContains(t, stdout.String(), "complete -o default -o bashdefault -F _braid braid")
}

func TestCompleteRootCommandsAndGlobalOptions(t *testing.T) {
	dir := t.TempDir()

	candidates := completeCandidates(t, dir, "")
	assertCandidate(t, candidates, "add")
	assertCandidate(t, candidates, "pull")
	assertCandidate(t, candidates, "completion")
	assertCandidate(t, candidates, "help")
	assertCandidate(t, candidates, "update")
	assertCandidate(t, candidates, "up")
	assertNoCandidate(t, candidates, "__complete")

	candidates = completeCandidates(t, dir, "--")
	assertCandidate(t, candidates, "--verbose")
	assertCandidate(t, candidates, "--quiet")
	assertCandidate(t, candidates, "--no-cache")
	assertCandidate(t, candidates, "--global-cache-dir")
	assertCandidate(t, candidates, "--help")
	candidates = completeCandidates(t, dir, "-")
	assertCandidate(t, candidates, "-h")

	candidates = completeCandidates(t, dir, "--verbose", "")
	assertNoCandidate(t, candidates, "--verbose")
	assertNoCandidate(t, candidates, "-v")
	assertNoCandidate(t, candidates, "--quiet")
	assertCandidate(t, candidates, "--no-cache")

	candidates = completeCandidates(t, dir, "--global-cache-dir", "cache", "")
	assertNoCandidate(t, candidates, "--no-cache")
	assertCandidate(t, candidates, "--global-cache-dir")
}

func TestCompleteCommandOptions(t *testing.T) {
	dir := t.TempDir()

	candidates := completeCandidates(t, dir, "sync", "--")
	assertCandidate(t, candidates, "--pull-only")
	assertCandidate(t, candidates, "--autostash")
	assertCandidate(t, candidates, "--keep")
	assertNoCandidate(t, candidates, "--no-commit")

	candidates = completeCandidates(t, dir, "sync", "--autostash", "--")
	assertNoCandidate(t, candidates, "--autostash")
	assertCandidate(t, candidates, "--pull-only")

	candidates = completeCandidates(t, dir, "pull", "")
	assertCandidate(t, candidates, "--branch")
	assertCandidate(t, candidates, "-b")
	assertCandidate(t, candidates, "--tag")
	assertCandidate(t, candidates, "--revision")
	assertCandidate(t, candidates, "--keep")
	assertCandidate(t, candidates, "--no-commit")

	candidates = completeCandidates(t, dir, "add", "")
	assertCandidate(t, candidates, "--no-commit")
	assertCandidate(t, candidates, "--sync-push")
	candidates = completeCandidates(t, dir, "add", ":replicant", "--")
	assertCandidate(t, candidates, "--no-commit")
	assertNoCandidate(t, candidates, "--name")
	assertNoCandidate(t, candidates, "--branch")
	assertNoCandidate(t, candidates, "--tag")
	assertNoCandidate(t, candidates, "--revision")
	assertNoCandidate(t, candidates, "--partial-clone")
	assertNoCandidate(t, candidates, "--sync-push")

	candidates = completeCandidates(t, dir, "push", "")
	assertCandidate(t, candidates, "--message")
	assertCandidate(t, candidates, "-m")
	assertNoCandidate(t, candidates, "--no-commit")

	candidates = completeCandidates(t, dir, "remove", "")
	assertCandidate(t, candidates, "--no-commit")

	candidates = completeCandidates(t, dir, "pull", "--no-commit", "--")
	assertNoCandidate(t, candidates, "--no-commit")

	candidates = completeCandidates(t, dir, "update", "")
	assertCandidate(t, candidates, "--no-commit")

	candidates = completeCandidates(t, dir, "up", "")
	assertCandidate(t, candidates, "--no-commit")

	candidates = completeCandidates(t, dir, "completion", "")
	assertCandidate(t, candidates, "bash")
	assertCandidate(t, candidates, "help")
	assertCandidate(t, candidates, "--help")
	assertCandidate(t, candidates, "-h")
}

func TestCompleteEveryCommandOptionAtEveryValidPosition(t *testing.T) {
	dir := t.TempDir()
	for _, test := range clitest.CompletionContractCases() {
		t.Run(test.Name, func(t *testing.T) {
			candidates := completeCandidates(t, dir, test.Words...)
			if test.WantEmpty && len(candidates) != 0 {
				t.Fatalf("candidates = %#v, want empty", candidates)
			}
			for _, want := range test.Want {
				assertCandidate(t, candidates, want)
			}
			for _, unwanted := range test.Unwanted {
				assertNoCandidate(t, candidates, unwanted)
			}
		})
	}
}

func TestCompleteMirrorPathsRelativeToCurrentDirectory(t *testing.T) {
	repo := testutil.InitRepo(t)
	writeCompletionConfig(t, repo)
	subdir := filepath.Join(repo, "apps", "web")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	candidates := completeCandidates(t, subdir, "status", "")
	assertCandidate(t, candidates, ":root")
	assertCandidate(t, candidates, "vendor/local")
	assertCandidate(t, candidates, "../../vendor/root")
	assertCandidate(t, candidates, "../../path with spaces/lib")

	candidates = completeCandidates(t, repo, "status", "path")
	assertCandidate(t, candidates, "path with spaces/lib")

	candidates = completeCandidates(t, repo, "update", "")
	assertCandidate(t, candidates, "apps/web/vendor/local")
	assertCandidate(t, candidates, "vendor/root")

	candidates = completeCandidates(t, repo, "up", "path")
	assertCandidate(t, candidates, "path with spaces/lib")
}

func TestCompleteMirrorPathsForSyncExcludeAlreadySelectedPaths(t *testing.T) {
	repo := testutil.InitRepo(t)
	writeCompletionConfig(t, repo)

	candidates := completeCandidates(t, repo, "sync", "vendor/root", "")
	assertNoCandidate(t, candidates, "vendor/root")
	assertCandidate(t, candidates, "apps/web/vendor/local")
	assertCandidate(t, candidates, "path with spaces/lib")
}

func TestCompleteSingleMirrorPathCommandStopsAfterPath(t *testing.T) {
	repo := testutil.InitRepo(t)
	writeCompletionConfig(t, repo)

	candidates := completeCandidates(t, repo, "status", "vendor/root", "")
	assertNoCandidate(t, candidates, "vendor/root")
	assertNoCandidate(t, candidates, "apps/web/vendor/local")
}

func TestCompleteMirrorPathsAreSilentOutsideRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := NewAppWithOptions(Options{WorkDir: t.TempDir()}).Run([]string{"__complete", "bash", "--", "status", ""}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("__complete exit = %d, stderr = %q", code, stderr.String())
	}
	assertCandidate(t, splitCompletionLines(stdout.String()), "help")
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestCompleteFilesystemContexts(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatalf("create vendor dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	candidates := completeCandidates(t, dir, "--global-cache-dir", "ca")
	assertCandidate(t, candidates, "cache/")

	candidates = completeCandidates(t, dir, "--global-cache-dir=ca")
	assertCandidate(t, candidates, "--global-cache-dir=cache/")

	candidates = completeCandidates(t, dir, "add", "https://example.test/repo.git", "ven")
	assertCandidate(t, candidates, "vendor/")

	candidates = completeCandidates(t, dir, "diff", "--", "READ")
	assertCandidate(t, candidates, "README.md")
}

func completeCandidates(t *testing.T, dir string, words ...string) []string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := append([]string{"__complete", "bash", "--"}, words...)
	code := NewAppWithOptions(Options{WorkDir: dir}).Run(args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("braid %v exit = %d, stderr = %q", args, code, stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("braid %v stderr = %q, want empty", args, stderr.String())
	}
	return splitCompletionLines(stdout.String())
}

func splitCompletionLines(output string) []string {
	output = strings.TrimSuffix(output, "\n")
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

func assertCandidate(t *testing.T, candidates []string, want string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate == want {
			return
		}
	}
	t.Fatalf("candidates missing %q: %#v", want, candidates)
}

func assertNoCandidate(t *testing.T, candidates []string, unwanted string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate == unwanted {
			t.Fatalf("candidates contain %q unexpectedly: %#v", unwanted, candidates)
		}
	}
}

func writeCompletionConfig(t *testing.T, repo string) {
	t.Helper()
	data := []byte(`{
  "config_version": 2,
  "sources": {
    "local": {
      "url": "https://example.test/local.git",
      "branch": "main",
      "revision": "1111111",
      "mirrors": {"apps/web/vendor/local": ""}
    },
    "spaces": {
      "url": "https://example.test/spaces.git",
      "branch": "main",
      "revision": "2222222",
      "mirrors": {"path with spaces/lib": ""}
    },
    "root": {
      "url": "https://example.test/root.git",
      "branch": "main",
      "revision": "3333333",
      "mirrors": {"vendor/root": ""}
    }
  }
}
`)
	if err := os.WriteFile(filepath.Join(repo, ".braids.json"), data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
