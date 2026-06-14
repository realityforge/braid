package testutil

import (
	"os/exec"
	"strings"
	"testing"

	"braid/internal/mirror"
)

func TestRubyOracleRemoteName(t *testing.T) {
	if _, err := exec.LookPath("ruby"); err != nil {
		t.Fatalf("ruby is required for migration oracle test: %v", err)
	}

	script := `puts "#{ARGV[0]}_braid_#{ARGV[1]}".gsub(/[^-A-Za-z0-9]/, "_")`
	out, err := exec.Command("ruby", "-e", script, "master", ".dotfolder/.dotfile.ext").Output()
	if err != nil {
		t.Fatalf("ruby oracle failed: %v", err)
	}

	goMirror := mirror.Mirror{Path: ".dotfolder/.dotfile.ext", Branch: "master"}
	if got, want := goMirror.Remote(), strings.TrimSpace(string(out)); got != want {
		t.Fatalf("Go remote = %q, Ruby oracle = %q", got, want)
	}
}
