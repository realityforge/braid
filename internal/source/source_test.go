package source

import "testing"

func TestCleanURLPreservesRoots(t *testing.T) {
	for _, value := range []string{"/", `C:\`, "file:///"} {
		if got := CleanURL(value); got != value {
			t.Fatalf("CleanURL(%q)=%q", value, got)
		}
	}
}
func TestDerivedName(t *testing.T) {
	for _, test := range []struct{ url, want string }{{"https://example.test/repo.git/", "repo"}, {`C:\work\repo.git`, "repo"}, {"git@example.test:org/repo.git", "repo"}} {
		if got := DerivedName(test.url); got != test.want {
			t.Fatalf("DerivedName(%q)=%q want %q", test.url, got, test.want)
		}
	}
}
func TestSourceRefs(t *testing.T) {
	s := Source{Name: "repo", Tracking: BranchTracking{Branch: "main"}}
	if got := s.Remote(); got != "main_braid_repo" {
		t.Fatal(got)
	}
	if got := s.LocalRef(); got != "main_braid_repo/main" {
		t.Fatal(got)
	}
}
