package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// StateDir returns the directory for daemon state files. AGENT_SESH_STATE_DIR
// wins, then $XDG_STATE_HOME/agent-sesh, then ~/.local/state/agent-sesh.
func StateDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("AGENT_SESH_STATE_DIR")); dir != "" {
		return dir, nil
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); dir != "" {
		return filepath.Join(dir, "agent-sesh"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "agent-sesh"), nil
}

// slug turns an address into a file name part: 127.0.0.1:2270 becomes 127.0.0.1_2270.
func slug(addr string) string {
	return strings.NewReplacer(":", "_", "/", "_", " ", "_").Replace(strings.TrimSpace(addr))
}

// PIDPath returns the pid file for an address.
func PIDPath(addr string) (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon-"+slug(addr)+".pid"), nil
}

// LogPath returns the log file for an address.
func LogPath(addr string) (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon-"+slug(addr)+".log"), nil
}

// WritePID writes the pid file and creates the state directory when needed.
func WritePID(addr string, pid int) error {
	path, err := PIDPath(addr)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

// ReadPID returns the pid in the file. ok is false when the file is absent.
func ReadPID(addr string) (int, bool, error) {
	path, err := PIDPath(addr)
	if err != nil {
		return 0, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false, fmt.Errorf("bad pid file %s: %w", path, err)
	}
	if pid <= 0 {
		return 0, false, fmt.Errorf("bad pid file %s: %d", path, pid)
	}
	return pid, true, nil
}

// RemovePID deletes the pid file. A missing file is not an error.
func RemovePID(addr string) error {
	path, err := PIDPath(addr)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
