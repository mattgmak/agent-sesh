package tmux

import (
	"os/exec"
	"strconv"
	"strings"
)

// PaneInfo is a snapshot of live tmux pane metadata used for validation/debugging.
type PaneInfo struct {
	Target         string
	PaneID         string
	SessionName    string
	WindowIndex    string
	PaneIndex      string
	TTY            string
	CurrentCommand string
	StartCommand   string
	CurrentPath    string
	Exists         bool
	HasPiAgent     bool
}

// PaneTTY returns the tty device for a tmux pane target.
func PaneTTY(target string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_tty}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PaneLocation returns window.pane coordinates for a tmux pane target.
func PaneLocation(target string) (window, pane string, ok bool) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{window_index}\t#{pane_index}").Output()
	if err != nil {
		return "", "", false
	}
	fields := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(fields) != 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// PanePID returns the shell pid for a tmux pane target.
func PanePID(target string) (int, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_pid}").Output()
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

// PaneHasPiAgent reports whether a pi coding-agent process is running in the pane.
func PaneHasPiAgent(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if snap, err := GetSnapshot(false); err == nil && snap.HasPiAgent(target) {
		return true
	}
	if paneHasPiOnTTY(target) {
		return true
	}
	pid, err := PanePID(target)
	if err != nil {
		return false
	}
	return processTreeHasPi(pid, 5)
}

func paneHasPiOnTTY(target string) bool {
	tty, err := PaneTTY(target)
	if err != nil || tty == "" {
		return false
	}
	out, err := exec.Command("ps", "-t", tty, "-o", "command=").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if isPiProcessLine(line) {
			return true
		}
	}
	return false
}

// ttyBaseName strips the /dev prefix from a tmux pane_tty so it matches the
// tty column reported by ps.
func ttyBaseName(tty string) string {
	return strings.TrimPrefix(strings.TrimSpace(tty), "/dev/")
}

// piAgentTTYs scans all processes once and returns the set of ttys that have a
// pi agent running on them.
func piAgentTTYs() map[string]bool {
	out, err := exec.Command("ps", "-e", "-o", "tty=,command=").Output()
	if err != nil {
		return nil
	}
	return collectPiTTYs(string(out))
}

func collectPiTTYs(psOutput string) map[string]bool {
	ttys := make(map[string]bool)
	for _, line := range strings.Split(psOutput, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if idx := strings.IndexByte(trimmed, ' '); idx > 0 {
			tty := trimmed[:idx]
			if tty == "" || tty == "?" || tty == "??" {
				continue
			}
			if isPiProcessLine(trimmed[idx+1:]) {
				ttys[tty] = true
			}
		}
	}
	return ttys
}

func isPiProcessLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	if strings.Contains(line, "lazygit") {
		return false
	}

	fields := strings.Fields(line)
	for _, field := range fields {
		base := field
		if idx := strings.LastIndex(field, "/"); idx >= 0 {
			base = field[idx+1:]
		}
		if base == "pi" {
			return true
		}
	}
	return false
}

func processTreeHasPi(pid int, depth int) bool {
	if pid <= 0 || depth <= 0 {
		return false
	}
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Fields(string(out)) {
		child, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		cmd, err := exec.Command("ps", "-o", "command=", "-p", line).Output()
		if err == nil && isPiProcessLine(string(cmd)) {
			return true
		}
		if processTreeHasPi(child, depth-1) {
			return true
		}
	}
	return false
}

// PaneInfoFor returns live metadata for a tmux target.
func PaneInfoFor(target string) PaneInfo {
	return PaneInfoForOpts(target, false)
}

// PaneInfoForOpts returns pane metadata, optionally skipping a redundant pi-agent check.
func PaneInfoForOpts(target string, knownPi bool) PaneInfo {
	info := PaneInfo{Target: strings.TrimSpace(target)}
	if info.Target == "" {
		return info
	}

	if snap, err := GetSnapshot(false); err == nil {
		if cached, ok := snap.PaneInfo(info.Target); ok {
			if knownPi {
				cached.HasPiAgent = true
			}
			return cached
		}
	}

	info.Exists = PaneExists(info.Target)
	format := "#{pane_id}\t#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_tty}\t#{pane_current_command}\t#{pane_start_command}\t#{pane_current_path}"
	out, err := exec.Command("tmux", "display-message", "-p", "-t", info.Target, "-F", format).Output()
	if err != nil {
		return info
	}

	fields := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(fields) < 8 {
		return info
	}

	info.PaneID = fields[0]
	info.SessionName = fields[1]
	info.WindowIndex = fields[2]
	info.PaneIndex = fields[3]
	info.TTY = fields[4]
	info.CurrentCommand = fields[5]
	info.StartCommand = fields[6]
	info.CurrentPath = fields[7]
	if knownPi {
		info.HasPiAgent = true
	} else {
		info.HasPiAgent = detectPiAgent(info)
	}
	return info
}

// ListPaneIDs returns all pane ids in the tmux server.
func ListPaneIDs() ([]string, error) {
	if snap, err := GetSnapshot(false); err == nil {
		if ids := snap.PaneIDs(); len(ids) > 0 {
			return ids, nil
		}
	}
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id}").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}
