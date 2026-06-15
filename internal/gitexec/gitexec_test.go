package gitexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRunnerCapturesOutputAndStatus(t *testing.T) {
	runner := helperRunner(t, map[string]string{"GITEXEC_HELPER_EXIT": "7"})

	result, err := runner.Run(context.Background(), "mock-output")

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
	if result.Stdout != "helper stdout\n" {
		t.Fatalf("Stdout = %q", result.Stdout)
	}
	if result.Stderr != "helper stderr\n" {
		t.Fatalf("Stderr = %q", result.Stderr)
	}
	if !reflect.DeepEqual(result.GitArgs, []string{"mock-output"}) {
		t.Fatalf("GitArgs = %#v", result.GitArgs)
	}
}

func TestRunnerRunOKMapsNonZeroToExitError(t *testing.T) {
	runner := helperRunner(t, map[string]string{"GITEXEC_HELPER_EXIT": "9"})

	result, err := runner.RunOK(context.Background(), "mock-output")

	if result.ExitCode != 9 {
		t.Fatalf("ExitCode = %d, want 9", result.ExitCode)
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T %v, want *ExitError", err, err)
	}
	if !strings.Contains(err.Error(), "helper stderr") {
		t.Fatalf("error = %q, want stderr included", err.Error())
	}
}

func TestRunnerRunInteractivePassesStdioAndStatus(t *testing.T) {
	runner := helperRunner(t, map[string]string{"GITEXEC_HELPER_EXIT": "7"})
	var stdout, stderr bytes.Buffer

	result, err := runner.RunInteractive(context.Background(), strings.NewReader("input text\n"), &stdout, &stderr, "interactive")

	if err != nil {
		t.Fatalf("RunInteractive returned error: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
	if stdout.String() != "helper stdout: input text\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "helper stderr: input text\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if result.Stdout != "" {
		t.Fatalf("Result.Stdout = %q, want empty for interactive run", result.Stdout)
	}
	if result.Stderr != "" {
		t.Fatalf("Result.Stderr = %q, want empty for interactive run", result.Stderr)
	}
	if !reflect.DeepEqual(result.GitArgs, []string{"interactive"}) {
		t.Fatalf("GitArgs = %#v", result.GitArgs)
	}
}

func TestRunnerRunInteractiveOKMapsNonZeroToExitError(t *testing.T) {
	runner := helperRunner(t, map[string]string{"GITEXEC_HELPER_EXIT": "9"})
	var stdout, stderr bytes.Buffer

	result, err := runner.RunInteractiveOK(context.Background(), strings.NewReader("failure\n"), &stdout, &stderr, "interactive")

	if result.ExitCode != 9 {
		t.Fatalf("ExitCode = %d, want 9", result.ExitCode)
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T %v, want *ExitError", err, err)
	}
	if stdout.String() != "helper stdout: failure\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "helper stderr: failure\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunnerUsesArgvWithoutShellInterpretation(t *testing.T) {
	runner := helperRunner(t, nil)
	dangerous := "semi;colon $(echo no) && still-one-arg"

	result, err := runner.RunOK(context.Background(), "argv", dangerous, "plain")

	if err != nil {
		t.Fatalf("RunOK returned error: %v", err)
	}
	got := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	want := []string{"argv", dangerous, "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestRunnerEnvironmentOverridesAreScoped(t *testing.T) {
	const key = "GITEXEC_SCOPED_VALUE"
	if old := os.Getenv(key); old != "" {
		t.Fatalf("%s unexpectedly set in test environment", key)
	}

	runner := helperRunner(t, map[string]string{key: "from-runner"})
	result, err := runner.RunOK(context.Background(), "env", key)

	if err != nil {
		t.Fatalf("RunOK returned error: %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != "from-runner" {
		t.Fatalf("env output = %q, want from-runner", got)
	}
	if got := os.Getenv(key); got != "" {
		t.Fatalf("process env leaked %s=%q", key, got)
	}
}

func TestRunnerVerboseTraceUsesDeterministicArgv(t *testing.T) {
	var trace bytes.Buffer
	dir := t.TempDir()
	runner := helperRunner(t, nil)
	runner.WorkDir = dir
	runner.Verbose = true
	runner.Trace = &trace

	_, err := runner.RunOK(context.Background(), "status", "--short", "path with space")

	if err != nil {
		t.Fatalf("RunOK returned error: %v", err)
	}
	want := fmt.Sprintf("Braid: Executing [\"git\", \"status\", \"--short\", \"path with space\"] in %s\n", dir)
	if got := trace.String(); got != want {
		t.Fatalf("trace = %q, want %q", got, want)
	}
}

func TestGitVersionAndRequirement(t *testing.T) {
	git := Git{Runner: helperRunner(t, map[string]string{"GITEXEC_HELPER_VERSION": "git version 2.51.0 (Apple Git-171)\n"})}

	version, err := git.Version(context.Background())
	if err != nil {
		t.Fatalf("Version returned error: %v", err)
	}
	if version != "2.51.0" {
		t.Fatalf("version = %q, want 2.51.0", version)
	}
	if err := git.RequireVersion(context.Background(), MinimumGitVersion); err != nil {
		t.Fatalf("RequireVersion returned error: %v", err)
	}
	if err := git.RequireVersion(context.Background(), "2.60.0"); err == nil {
		t.Fatal("RequireVersion returned nil for too-low version")
	}
}

func TestParseAndCompareVersions(t *testing.T) {
	parseCases := map[string]string{
		"git version 1.5.5.1.98.gf0ec4\n":    "1.5.5.1.98.gf0ec4",
		"git version 2.39.3 (Apple Git-146)": "2.39.3",
		"not git":                            "",
	}
	for input, want := range parseCases {
		if got := ParseVersion(input); got != want {
			t.Fatalf("ParseVersion(%q) = %q, want %q", input, got, want)
		}
	}

	if CompareVersions("1.5.5.1.98.gf0ec4", "1.5.4.5") <= 0 {
		t.Fatal("expected 1.5.5.1.98.gf0ec4 to be greater than 1.5.4.5")
	}
	if CompareVersions("2.38.9", MinimumGitVersion) >= 0 {
		t.Fatal("expected 2.38.9 to be less than minimum")
	}
	if CompareVersions("2.39.0", MinimumGitVersion) != 0 {
		t.Fatal("expected 2.39.0 to equal minimum")
	}
}

func TestGitPreflightWrappers(t *testing.T) {
	git := Git{Runner: helperRunner(t, nil)}

	inside, err := git.IsInsideWorkTree(context.Background())
	if err != nil {
		t.Fatalf("IsInsideWorkTree returned error: %v", err)
	}
	if !inside {
		t.Fatal("IsInsideWorkTree = false, want true")
	}

	prefix, err := git.RelativeWorkingDir(context.Background())
	if err != nil {
		t.Fatalf("RelativeWorkingDir returned error: %v", err)
	}
	if prefix != "sub/" {
		t.Fatalf("RelativeWorkingDir = %q, want sub/", prefix)
	}

	path, err := git.RepoFilePath(context.Background(), "MERGE_MSG")
	if err != nil {
		t.Fatalf("RepoFilePath returned error: %v", err)
	}
	if path != ".git/MERGE_MSG" {
		t.Fatalf("RepoFilePath = %q, want .git/MERGE_MSG", path)
	}
}

func helperRunner(t *testing.T, env map[string]string) Runner {
	t.Helper()
	merged := map[string]string{
		"GITEXEC_HELPER_PROCESS": "1",
	}
	for key, value := range env {
		merged[key] = value
	}
	return Runner{
		Executable: os.Args[0],
		PrefixArgs: []string{
			"-test.run=TestHelperProcess",
			"--",
		},
		Env: merged,
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GITEXEC_HELPER_PROCESS") != "1" {
		return
	}

	args := helperGitArgs(os.Args)
	if len(args) == 0 {
		os.Exit(0)
	}

	switch args[0] {
	case "mock-output":
		helperFprintln(os.Stdout, "helper stdout")
		helperFprintln(os.Stderr, "helper stderr")
		os.Exit(helperExitCode())
	case "interactive":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			helperFprintf(os.Stderr, "read stdin: %v\n", err)
			os.Exit(2)
		}
		helperFprintf(os.Stdout, "helper stdout: %s", input)
		helperFprintf(os.Stderr, "helper stderr: %s", input)
		os.Exit(helperExitCode())
	case "argv":
		for _, arg := range args {
			helperFprintln(os.Stdout, arg)
		}
	case "env":
		if len(args) != 2 {
			helperFprintln(os.Stderr, "env requires key")
			os.Exit(2)
		}
		helperFprintln(os.Stdout, os.Getenv(args[1]))
	case "status":
	case "--version":
		version := os.Getenv("GITEXEC_HELPER_VERSION")
		if version == "" {
			version = "git version 2.51.0\n"
		}
		helperFprint(os.Stdout, version)
	case "rev-parse":
		helperRevParse(args[1:])
	default:
		helperFprintf(os.Stderr, "unknown helper command %s\n", args[0])
		os.Exit(127)
	}
	os.Exit(0)
}

func helperFprint(w io.Writer, a ...any) {
	if _, err := fmt.Fprint(w, a...); err != nil {
		os.Exit(2)
	}
}

func helperFprintf(w io.Writer, format string, a ...any) {
	if _, err := fmt.Fprintf(w, format, a...); err != nil {
		os.Exit(2)
	}
}

func helperFprintln(w io.Writer, a ...any) {
	if _, err := fmt.Fprintln(w, a...); err != nil {
		os.Exit(2)
	}
}

func helperGitArgs(args []string) []string {
	for i, arg := range args {
		if arg == "--" {
			return args[i+1:]
		}
	}
	return nil
}

func helperExitCode() int {
	if value := os.Getenv("GITEXEC_HELPER_EXIT"); value != "" {
		var code int
		if _, err := fmt.Sscanf(value, "%d", &code); err == nil {
			return code
		}
	}
	return 0
}

func helperRevParse(args []string) {
	if len(args) == 0 {
		helperFprintln(os.Stderr, "missing rev-parse arg")
		os.Exit(2)
	}
	switch args[0] {
	case "--is-inside-work-tree":
		helperFprintln(os.Stdout, "true")
	case "--show-prefix":
		helperFprintln(os.Stdout, "sub/")
	case "--git-path":
		if len(args) != 2 {
			helperFprintln(os.Stderr, "--git-path requires path")
			os.Exit(2)
		}
		helperFprintf(os.Stdout, ".git/%s\n", args[1])
	default:
		helperFprintf(os.Stdout, "%s-resolved\n", args[0])
	}
}
