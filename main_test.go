package main_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestMainEntrypoint_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          []string
		wantErr       bool
		wantSubstring string
	}{
		{name: "help", args: []string{"run", ".", "--help"}, wantSubstring: "Usage"},
		{name: "missing config", args: []string{"run", ".", "-c", "/tmp/does-not-exist.json", "validate"}, wantErr: true, wantSubstring: "load config"},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("go", tt.args...) //nolint:gosec // Test-only invocation with static arguments.
			out, err := cmd.CombinedOutput()
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v\noutput:\n%s", err, tt.wantErr, string(out))
			}
			if !strings.Contains(string(out), tt.wantSubstring) {
				t.Fatalf("expected output to contain %q, got:\n%s", tt.wantSubstring, string(out))
			}
		})
	}
}
