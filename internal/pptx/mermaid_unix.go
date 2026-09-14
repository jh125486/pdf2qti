//go:build unix

package pptx

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts cmd in its own process group (Setpgid) and replaces its default
// context-cancellation behavior (killing only the direct child) with killing that whole group —
// see runMmdc's doc comment for why: mmdc's own process is not what outlives a timeout, the
// headless Chromium it launches is, and cmd.WaitDelay alone only forces CombinedOutput's pipes
// closed, it never signals that grandchild at all. A negative PID in syscall.Kill targets the
// process GROUP rather than a single PID, which is exactly what reaches Chromium here: as the
// group's session leader (Setpgid with no explicit Pgid makes the child its own group leader),
// mmdc's own PID doubles as the group's PID that every descendant it forks inherits.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
