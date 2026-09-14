//go:build unix

// Whitebox package: configureProcessGroup is unexported, with no exported entry point that
// reaches it without waiting out the real mermaidRenderTimeout const (30s, not injectable) via
// runMmdc — testing it directly here is the only way to get fast, deterministic coverage of it at
// all.
package pptx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestConfigureProcessGroup_KillsGrandchildOnCancel is a justified exception to using
// testing/synctest for timing: this test's timing is real OS process scheduling and signal
// delivery, not anything a fake clock can stand in for.
func TestConfigureProcessGroup_KillsGrandchildOnCancel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	stub := filepath.Join(dir, "stub.sh")
	script := fmt.Sprintf("#!/bin/sh\n(sleep 30 &\necho $! > %q)\nsleep 30\n", pidFile)
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil { //nolint:gosec // test-local executable stub
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, stub) //nolint:gosec // stub is our own temp-dir script, not user input
	configureProcessGroup(cmd)
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err == nil {
		t.Fatal("expected the stub to be killed by the context timeout")
	}

	// The stub's own subshell (forking the grandchild, then writing its pid) has to run before
	// the 500ms cancellation fires, but shell startup time under test-runner load is variable —
	// briefly retry the read rather than assuming it always lands within one attempt.
	var data []byte
	var err error
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if data, err = os.ReadFile(pidFile); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("read grandchild pid file: %v", err)
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		t.Fatalf("parse grandchild pid: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild process %d is still alive after configureProcessGroup should have killed it", pid)
}
