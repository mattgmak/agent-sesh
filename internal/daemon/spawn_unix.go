//go:build unix

package daemon

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// Detach puts the child in its own process group, so the shell that ran
// `daemon start` cannot take the daemon down when it exits.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// Terminate asks a process to exit.
func Terminate(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

// Alive reports whether pid exists.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// Context returns a context that Serve stops on. SIGINT and SIGTERM both end it.
func Context() (context.Context, func()) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
