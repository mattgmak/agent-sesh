package tmux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestExecHelpersTimeout(t *testing.T) {
	oldTimeout := execTimeout
	execTimeout = 20 * time.Millisecond
	t.Cleanup(func() { execTimeout = oldTimeout })

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "output",
			run: func() error {
				argv := append([]string{"test.timeout-output"}, blockingCommand()...)
				_, err := execOutput(argv[0], argv[1:]...)
				return err
			},
		},
		{
			name: "run",
			run: func() error {
				argv := append([]string{"test.timeout-run"}, blockingCommand()...)
				return execRun(argv[0], argv[1:]...)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "pid")
			t.Setenv("AGENT_SESH_TIMEOUT_PID", pidFile)
			done := make(chan error, 1)
			go func() { done <- tt.run() }()

			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("error = %v, want context deadline exceeded", err)
				}
			case <-timer.C:
				killBlockingCommand(t, pidFile)
				t.Fatal("command did not honor its timeout")
			}
		})
	}
}

func blockingCommand() []string {
	return []string{
		"sh",
		"-c",
		`printf '%s' "$$" > "$AGENT_SESH_TIMEOUT_PID"; while :; do :; done`,
	}
}

func killBlockingCommand(t *testing.T, pidFile string) {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Logf("read blocking command pid: %v", err)
		return
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Logf("parse blocking command pid: %v", err)
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Logf("find blocking command process: %v", err)
		return
	}
	if err := process.Kill(); err != nil {
		t.Logf("kill blocking command process: %v", err)
	}
}

func writeFakeCommand(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
