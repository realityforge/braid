package clitest

import (
	"strings"
	"testing"

	"braid/internal/cli"
)

func TestGeneratedSyntaxConstraintCases(t *testing.T) {
	for _, test := range SyntaxConstraintCases() {
		t.Run(test.Name, func(t *testing.T) {
			_, err := cli.Parse(test.Args)
			if test.WantError == "" {
				if err != nil {
					t.Fatalf("Parse(%v) returned error: %v", test.Args, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.WantError) {
				t.Fatalf("Parse(%v) error = %v, want containing %q", test.Args, err, test.WantError)
			}
		})
	}
}
