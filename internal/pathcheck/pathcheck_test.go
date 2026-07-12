package pathcheck

import (
	"strings"
	"testing"

	"braid/internal/source"
)

func TestValidateLocalPathContract(t *testing.T) {
	reject := []string{
		"",
		".",
		"/absolute",
		"../parent",
		"child/..",
		".git/config",
		"nested/.git/config",
		`C:\absolute`,
		"C:relative",
		`\\server\share`,
		`has\backslash`,
		"CON",
		"com1.txt",
		"trailingdot.",
		"trailingspace ",
		"has:colon",
	}
	for _, value := range reject {
		t.Run("reject "+value, func(t *testing.T) {
			if err := ValidateLocal(value, nil); err == nil {
				t.Fatalf("ValidateLocal(%q) returned nil error", value)
			}
		})
	}

	accept := []string{
		"vendor/repo",
		"path with spaces/repo",
		".dotfolder/.dotfile.ext",
		"auxiliary/data",
	}
	for _, value := range accept {
		t.Run("accept "+value, func(t *testing.T) {
			if err := ValidateLocal(value, nil); err != nil {
				t.Fatalf("ValidateLocal(%q) returned error: %v", value, err)
			}
		})
	}
}

func TestValidateUpstreamPathContract(t *testing.T) {
	reject := []string{
		"",
		".",
		"/absolute",
		"../parent",
		"child/..",
		`C:\absolute`,
		"C:relative",
		`\\server\share`,
		`has\backslash`,
		"trailingdot.",
		"trailingspace ",
		"has:colon",
	}
	for _, value := range reject {
		t.Run("reject "+value, func(t *testing.T) {
			if err := ValidateUpstream(value); err == nil {
				t.Fatalf("ValidateUpstream(%q) returned nil error", value)
			}
		})
	}

	accept := []string{
		".git/objects",
		"CON",
		"COM1.txt",
		"path with spaces/file",
	}
	for _, value := range accept {
		t.Run("accept "+value, func(t *testing.T) {
			if err := ValidateUpstream(value); err != nil {
				t.Fatalf("ValidateUpstream(%q) returned error: %v", value, err)
			}
		})
	}
}

func TestCaseFoldCollision(t *testing.T) {
	err := ValidateLocal("Vendor/Repo", []string{"vendor/repo"})
	if err == nil {
		t.Fatal("ValidateLocal returned nil error")
	}
	if !strings.Contains(err.Error(), "case-fold collision") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRemoteNameCollision(t *testing.T) {
	existing := []source.Source{{Name: "a.b", Tracking: source.BranchTracking{Branch: "main"}}}
	candidate := source.Source{Name: "a_b", Tracking: source.BranchTracking{Branch: "main"}}

	err := CheckRemoteCollision(candidate, existing)
	if err == nil {
		t.Fatal("CheckRemoteCollision returned nil error")
	}
	if !strings.Contains(err.Error(), "main_braid_a_b") {
		t.Fatalf("error = %q", err.Error())
	}
}
