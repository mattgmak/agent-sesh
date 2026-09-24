package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/mattgmak/agent-sesh/internal/prof"
)

const (
	defaultExecTimeout = 5 * time.Second
	execWaitDelay      = time.Second
)

var execTimeout = defaultExecTimeout

func commandContext(argv []string) (*exec.Cmd, context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = execWaitDelay
	return cmd, ctx, cancel
}

func commandError(name string, ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", name, ctxErr)
	}
	return err
}

// execOutput runs a command with a bounded lifetime, captures stdout, and
// records its duration under the given profile name.
func execOutput(name string, argv ...string) ([]byte, error) {
	defer prof.Start(name)()
	cmd, ctx, cancel := commandContext(argv)
	defer cancel()
	out, err := cmd.Output()
	return out, commandError(name, ctx, err)
}

// execRun runs a command with a bounded lifetime, discards stdout, and records
// its duration under the given profile name.
func execRun(name string, argv ...string) error {
	defer prof.Start(name)()
	cmd, ctx, cancel := commandContext(argv)
	defer cancel()
	return commandError(name, ctx, cmd.Run())
}
