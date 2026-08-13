package tmux

import (
	"os/exec"

	"github.com/mattgmak/agent-sesh/internal/prof"
)

// execOutput runs a command, captures stdout, and records its duration under
// the given profile name.
func execOutput(name string, argv ...string) ([]byte, error) {
	defer prof.Start(name)()
	return exec.Command(argv[0], argv[1:]...).Output()
}

// execRun runs a command (discarding stdout) and records its duration under
// the given profile name.
func execRun(name string, argv ...string) error {
	defer prof.Start(name)()
	return exec.Command(argv[0], argv[1:]...).Run()
}
