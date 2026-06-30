package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCLIHelpFromMainEntrypoint(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected help command to succeed: %v\noutput:\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "Usage") {
		t.Fatalf("expected usage output, got:\n%s", string(out))
	}
}

func TestCLIMissingConfigReturnsErrorFromMainEntrypoint(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "-c", "/tmp/does-not-exist.json", "validate")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail with missing config\noutput:\n%s", string(out))
	}
	if !strings.Contains(string(out), "load config") {
		t.Fatalf("expected load config error output, got:\n%s", string(out))
	}
}
