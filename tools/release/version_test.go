package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionScript(t *testing.T) {
	script := versionScript(t)

	assertStdout(t, script, "0.1.0", "normalize", "0.1.0")
	assertStdout(t, script, "0.1.0", "normalize", "v0.1.0")
	assertStdout(t, script, "10.20.30", "normalize", "v10.20.30")
	assertStdout(t, script, "v1.2.3", "tag", "1.2.3")
	assertStdout(t, script, "v1.2.3", "tag", "v1.2.3")

	assertFails(t, script, "normalize", "")
	assertFails(t, script, "normalize", "01.2.3")
	assertFails(t, script, "normalize", "1.02.3")
	assertFails(t, script, "normalize", "1.2.03")
	assertFails(t, script, "normalize", "1.2")
	assertFails(t, script, "normalize", "v1.2.3-rc.1")
	assertFails(t, script, "normalize", "v1.2.3+build")

	assertOK(t, script, "is-stable-tag", "v1.2.3")
	assertFails(t, script, "is-stable-tag", "1.2.3")
	assertFails(t, script, "is-stable-tag", "v01.2.3")
	assertFails(t, script, "is-stable-tag", "v1.2.3-rc.1")

	assertStdout(t, script, "v1.0.0", "assert-newer", "1.0.0")
	assertStdout(t, script, "v1.2.4", "assert-newer", "1.2.4", "v1.2.3", "v0.9.9", "not-a-version", "v2.0")
	assertStdout(t, script, "v2.0.0", "assert-newer", "v2.0.0", "v1.9.9", "v1.10.0", "v1.10.1")
	assertFails(t, script, "assert-newer", "1.2.3", "v1.2.3")
	assertFails(t, script, "assert-newer", "1.2.2", "v1.2.3")
	assertFails(t, script, "assert-newer", "1.2.3", "v2.0.0")
}

func versionScript(t *testing.T) string {
	t.Helper()

	for _, root := range []string{os.Getenv("RUNFILES_DIR"), os.Getenv("TEST_SRCDIR")} {
		if root == "" {
			continue
		}
		var found string
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, filepath.Join("tools", "release", "version.sh")) {
				found = path
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk runfiles %s: %v", root, err)
		}
		if found != "" {
			return found
		}
	}
	t.Fatal("tools/release/version.sh not found in runfiles")
	return ""
}

func assertOK(t *testing.T, script string, args ...string) {
	t.Helper()
	cmd := exec.Command(script, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func assertStdout(t *testing.T, script, want string, args ...string) {
	t.Helper()
	cmd := exec.Command(script, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	got := strings.TrimSpace(string(output))
	if got != want {
		t.Fatalf("%s stdout = %q, want %q", strings.Join(args, " "), got, want)
	}
}

func assertFails(t *testing.T, script string, args ...string) {
	t.Helper()
	cmd := exec.Command(script, args...)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("%s succeeded unexpectedly\n%s", strings.Join(args, " "), output)
	}
}
