//go:build !unix

package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// Detach has no effect where process groups do not exist.
func Detach(cmd *exec.Cmd) {}

// Terminate reports that this platform has no daemon signalling support.
func Terminate(pid int) error {
	return errors.New("daemon control needs a unix platform")
}

// Alive reports false because pid checks are not implemented here.
func Alive(pid int) bool { return false }

// Context returns a context that Serve stops on.
func Context() (context.Context, func()) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}
