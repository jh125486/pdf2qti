//go:build windows

package pptx

import "os/exec"

// configureProcessGroup is a no-op on Windows: unlike unix, there's no single-syscall way to
// signal a whole process tree, and building the Job-Object-based equivalent needed to actually
// reap mmdc's Chromium descendant is out of scope here. On this platform a timed-out render still
// stops Render from hanging (via cmd.WaitDelay closing the pipes), it just may leave that
// descendant running afterward, unlike unix where the sibling configureProcessGroup kills it.
func configureProcessGroup(*exec.Cmd) {}
