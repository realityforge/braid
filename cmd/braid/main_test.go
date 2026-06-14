package main

import (
	"bytes"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run returned exit code %d", code)
	}
	if got, want := stdout.String(), "braid 0.0.0-dev\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
}

func TestRunUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)

	if code == 0 {
		t.Fatal("run returned success for missing command")
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
	if got, want := stderr.String(), "usage: braid version\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}
