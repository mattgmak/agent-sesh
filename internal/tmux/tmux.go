package tmux

import (
	"fmt"
	"strings"
)

func PaneExists(target string) bool {
	if strings.TrimSpace(target) == "" {
		return false
	}
	if snap, err := GetSnapshot(false); err == nil && snap.HasPane(target) {
		return true
	}
	err := execRun("tmux.list-panes", "tmux", "list-panes", "-t", target, "-F", "#{pane_id}")
	return err == nil
}

func CapturePane(target string, lines int) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("empty pane target")
	}

	// Plain capture-pane returns the pane's current screen — which is the
	// alternate screen when the pane is in one — so it always matches what the
	// user sees. capture-pane -a returns the *other* buffer instead, which for
	// an inline-rendering pane (like pi) is stale leftover content, so it is
	// only used as a last resort. This mirrors sesh's capture behavior.
	if content, ok := capturePaneVisible(target); ok {
		return content, nil
	}
	if content, ok := capturePaneAltScreen(target); ok {
		return content, nil
	}

	if lines <= 0 {
		lines = 200
	}
	out, err := execOutput(
		"tmux.capture-pane", "tmux", "capture-pane", "-e", "-p", "-J", "-S", fmt.Sprintf("-%d", lines), "-t", target,
	)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func capturePaneVisible(target string) (string, bool) {
	out, err := execOutput("tmux.capture-pane-visible", "tmux", "capture-pane", "-e", "-p", "-t", target)
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", false
	}
	return string(out), true
}

func capturePaneAltScreen(target string) (string, bool) {
	out, err := execOutput("tmux.capture-pane-alt", "tmux", "capture-pane", "-e", "-p", "-a", "-q", "-t", target)
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", false
	}
	return string(out), true
}

func SwitchClient(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("empty pane target")
	}
	if kind, _ := ParseTarget(target); kind == TargetPane {
		if err := execRun("tmux.select-pane", "tmux", "select-pane", "-t", target); err != nil {
			return err
		}
	}
	return execRun("tmux.switch-client", "tmux", "switch-client", "-t", target)
}

// ClientTTYs lists the ttys of every client attached to the tmux server.
func ClientTTYs() ([]string, error) {
	out, err := execOutput("tmux.list-clients", "tmux", "list-clients", "-F", "#{client_tty}")
	if err != nil {
		return nil, err
	}
	var ttys []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ttys = append(ttys, line)
		}
	}
	return ttys, nil
}

// ClientTTY returns the tty of the client attached to target. With an empty
// target it falls back to the first client of the server.
func ClientTTY(target string) (string, error) {
	if target = strings.TrimSpace(target); target != "" {
		out, err := execOutput("tmux.client-tty", "tmux", "display-message", "-p", "-t", target, "#{client_tty}")
		if err == nil {
			if tty := strings.TrimSpace(string(out)); tty != "" {
				return tty, nil
			}
		}
	}
	ttys, err := ClientTTYs()
	if err != nil {
		return "", err
	}
	if len(ttys) == 0 {
		return "", fmt.Errorf("no tmux client attached")
	}
	return ttys[0], nil
}

func SessionName(target string) (string, error) {
	out, err := execOutput("tmux.session-name", "tmux", "display-message", "-p", "-t", target, "#{session_name}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func PanePath(target string) (string, error) {
	out, err := execOutput("tmux.pane-path", "tmux", "display-message", "-p", "-t", target, "#{pane_current_path}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func RenameSession(session string, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("session name cannot be empty")
	}
	return execRun("tmux.rename-session", "tmux", "rename-session", "-t", session, name)
}

func KillPane(target string) error {
	return execRun("tmux.kill-pane", "tmux", "kill-pane", "-t", target)
}

func KillSession(name string) error {
	return execRun("tmux.kill-session", "tmux", "kill-session", "-t", name)
}

func NewWindowAtPanePath(target string) error {
	path, err := PanePath(target)
	if err != nil {
		return err
	}
	session, err := SessionName(target)
	if err != nil {
		return err
	}
	args := []string{"new-window", "-t", session}
	if path != "" {
		args = append(args, "-c", path)
	}
	return execRun("tmux.new-window", append([]string{"tmux"}, args...)...)
}

// TargetKind classifies a tmux target string.
type TargetKind int

const (
	TargetUnknown TargetKind = iota
	TargetPane
	TargetWindow
	TargetSession
)

// ParseTarget inspects common tmux target forms without calling tmux.
func ParseTarget(target string) (TargetKind, string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return TargetUnknown, ""
	}
	if strings.HasPrefix(target, "%") {
		return TargetPane, target
	}
	if strings.Contains(target, ":") {
		return TargetWindow, target
	}
	return TargetSession, target
}
