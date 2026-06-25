package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExecutableBashCompletion(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	script := runBraid(t, env, root, braid, "completion", "bash")
	assertExit(t, script, 0)
	assertEmpty(t, "completion bash stderr", script.stderr)
	assertContains(t, script.stdout, "_braid()")
	assertContains(t, script.stdout, `"${COMP_WORDS[0]}" __complete bash -- "${COMP_WORDS[@]:1}"`)
	assertContains(t, script.stdout, "complete -o default -o bashdefault -F _braid braid")

	rootCandidates := completeExecutable(t, env, root, braid, "")
	assertCompletionCandidate(t, rootCandidates, "add")
	assertCompletionCandidate(t, rootCandidates, "pull")
	assertCompletionCandidate(t, rootCandidates, "completion")
	assertNoCompletionCandidate(t, rootCandidates, "update")
	assertNoCompletionCandidate(t, rootCandidates, "up")
	assertNoCompletionCandidate(t, rootCandidates, "__complete")

	globalFlags := completeExecutable(t, env, root, braid, "--")
	assertCompletionCandidate(t, globalFlags, "--verbose")
	assertCompletionCandidate(t, globalFlags, "--quiet")
	assertCompletionCandidate(t, globalFlags, "--cache-dir")

	noRepoMirrors := runBraid(t, env, root, braid, "__complete", "bash", "--", "status", "")
	assertResult(t, noRepoMirrors, 0, "", "")
}

func TestExecutableBashCompletionMirrorPathsFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)

	_, workDir := writeCompletionDownstream(t, env, root)

	statusCandidates := completeExecutable(t, env, workDir, braid, "status", "")
	assertCompletionCandidate(t, statusCandidates, "vendor/local")
	assertCompletionCandidate(t, statusCandidates, "../../vendor/root")
	assertCompletionCandidate(t, statusCandidates, "../../path with spaces/lib")

	syncCandidates := completeExecutable(t, env, workDir, braid, "sync", "../../vendor/root", "")
	assertNoCompletionCandidate(t, syncCandidates, "../../vendor/root")
	assertCompletionCandidate(t, syncCandidates, "vendor/local")
	assertCompletionCandidate(t, syncCandidates, "../../path with spaces/lib")
}

func TestExecutableEmittedBashCompletionScriptWorks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash completion script smoke test uses Unix shell semantics")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash unavailable: %v", err)
	}

	root := t.TempDir()
	env := newProcessEnv(t, root)
	braid := braidBinary(t)
	_, workDir := writeCompletionDownstream(t, env, root)

	script := runBraid(t, env, root, braid, "completion", "bash")
	assertExit(t, script, 0)
	assertEmpty(t, "completion bash stderr", script.stderr)

	completionPath := filepath.Join(root, "braid-completion.bash")
	if err := os.WriteFile(completionPath, []byte(script.stdout), 0o644); err != nil {
		t.Fatalf("write emitted completion script: %v", err)
	}
	probePath := filepath.Join(root, "probe-completion.bash")
	probe := `set -euo pipefail
source "$1"
complete -p braid >/dev/null
COMP_WORDS=("$2" status "")
_braid
printf '%s\n' "${COMPREPLY[@]}"
`
	if err := os.WriteFile(probePath, []byte(probe), 0o755); err != nil {
		t.Fatalf("write completion probe script: %v", err)
	}

	result := runProcess(t, env, workDir, bash, probePath, completionPath, braid)
	assertExit(t, result, 0)
	assertEmpty(t, "completion probe stderr", result.stderr)
	candidates := splitCompletionOutput(result.stdout)
	assertCompletionCandidate(t, candidates, "vendor/local")
	assertCompletionCandidate(t, candidates, "../../vendor/root")
	assertCompletionCandidate(t, candidates, "../../path with spaces/lib")
}

func writeCompletionDownstream(t *testing.T, env processEnv, root string) (string, string) {
	t.Helper()
	downstream := filepath.Join(root, "downstream")
	initRepo(t, env, downstream)
	writeFile(t, downstream, "README.md", "downstream\n")
	commitAll(t, env, downstream, "seed downstream")
	writeConfig(t, downstream, map[string]configMirror{
		"apps/web/vendor/local": {
			URL:      "https://example.invalid/local.git",
			Branch:   "main",
			Revision: "1111111",
		},
		"path with spaces/lib": {
			URL:      "https://example.invalid/spaces.git",
			Branch:   "main",
			Revision: "2222222",
		},
		"vendor/root": {
			URL:      "https://example.invalid/root.git",
			Branch:   "main",
			Revision: "3333333",
		},
	})
	workDir := filepath.Join(downstream, "apps", "web")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}
	return downstream, workDir
}

func completeExecutable(t *testing.T, env processEnv, workdir, braid string, words ...string) []string {
	t.Helper()
	args := append([]string{"__complete", "bash", "--"}, words...)
	result := runBraid(t, env, workdir, braid, args...)
	assertExit(t, result, 0)
	assertEmpty(t, "completion stderr", result.stderr)
	return splitCompletionOutput(result.stdout)
}

func splitCompletionOutput(output string) []string {
	output = strings.TrimSuffix(output, "\n")
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

func assertCompletionCandidate(t *testing.T, candidates []string, want string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate == want {
			return
		}
	}
	t.Fatalf("completion candidates missing %q: %#v", want, candidates)
}

func assertNoCompletionCandidate(t *testing.T, candidates []string, unwanted string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate == unwanted {
			t.Fatalf("completion candidates contain %q unexpectedly: %#v", unwanted, candidates)
		}
	}
}
