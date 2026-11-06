package tmux

import (
	"strconv"
	"strings"

	"github.com/mattgmak/agent-sesh/internal/config"
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
	ShellPID       int
	Width          int
	Exists         bool
	HasPiAgent     bool
	// Agent is the configured agent name the pane runs, empty when no agent runs there.
	Agent string
	// IsSubagent: pane runs a pi-interactive-subagents launch script
	// (bash <...>/subagent-scripts/<name>-<id>.sh). Runs pi too, but must not
	// surface as a pi session.
	IsSubagent bool
}

// PaneTTY returns the tty device for a tmux pane target.
func PaneTTY(target string) (string, error) {
	out, err := execOutput("tmux.pane-tty", "tmux", "display-message", "-p", "-t", target, "#{pane_tty}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PaneLocation returns window.pane coordinates for a tmux pane target.
func PaneLocation(target string) (window, pane string, ok bool) {
	out, err := execOutput("tmux.pane-location", "tmux", "display-message", "-p", "-t", target, "#{window_index}\t#{pane_index}")
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
	out, err := execOutput("tmux.pane-pid", "tmux", "display-message", "-p", "-t", target, "#{pane_pid}")
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

// PaneIsSubagent reports whether the pane is a pi-interactive-subagents surface.
func PaneIsSubagent(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if snap, err := GetSnapshot(false); err == nil {
		if info, ok := snap.PaneInfo(target); ok {
			return info.IsSubagent
		}
	}
	return PaneInfoFor(target).IsSubagent
}

func paneHasPiOnTTY(target string) bool {
	_, ok := paneAgentOnTTY(target)
	return ok
}

// paneAgentOnTTY returns the agent name running on a pane's tty.
func paneAgentOnTTY(target string) (string, bool) {
	tty, err := PaneTTY(target)
	if err != nil || tty == "" {
		return "", false
	}
	out, err := execOutput("ps.pane-tty", "ps", "-t", tty, "-o", "command=")
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if name, ok := agentProcessName(line); ok {
			return name, true
		}
	}
	return "", false
}

// ttyBaseName strips the /dev prefix from a tmux pane_tty so it matches the
// tty column reported by ps.
func ttyBaseName(tty string) string {
	return strings.TrimPrefix(strings.TrimSpace(tty), "/dev/")
}

// piAgentTTYs scans the given ttys once and returns the agent name each tty has
// running on it.
func piAgentTTYs(ttys []string) (map[string]string, error) {
	if len(ttys) == 0 {
		return nil, nil
	}
	out, err := execOutput("ps.tty-scan", "ps", "-t", strings.Join(ttys, ","), "-o", "tty=,command=")
	if err != nil {
		return nil, err
	}
	return collectPiTTYs(string(out)), nil
}

func collectPiTTYs(psOutput string) map[string]string {
	agents := make(map[string]string)
	for _, line := range strings.Split(psOutput, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if idx := strings.IndexByte(trimmed, ' '); idx > 0 {
			tty := trimmed[:idx]
			if tty == "" || tty == "?" || tty == "??" {
				continue
			}
			if name, ok := agentProcessName(trimmed[idx+1:]); ok {
				agents[tty] = name
			}
		}
	}
	return agents
}

// isPiProcessLine reports whether a ps command line runs a configured agent.
func isPiProcessLine(line string) bool {
	_, ok := agentProcessName(line)
	return ok
}

// agentProcessName returns the configured agent name a ps command line runs.
// Nix loadout wrappers name their process after the loadout (pi-local, pi-stock),
// so the names come from config instead of a hardcoded pi.
func agentProcessName(line string) (string, bool) {
	for _, name := range config.Agents() {
		if commandRuns(line, name) {
			return name, true
		}
	}
	return "", false
}

// commandRuns reports whether a command line executes the named binary. A line
// that mentions lazygit never counts, because its preview helper shares the name.
func commandRuns(line, name string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	if strings.Contains(line, "lazygit") {
		return false
	}

	for _, field := range strings.Fields(line) {
		base := field
		if idx := strings.LastIndex(field, "/"); idx >= 0 {
			base = field[idx+1:]
		}
		if base == name {
			return true
		}
	}
	return false
}

func processTreeHasPi(pid int, depth int) bool {
	if pid <= 0 || depth <= 0 {
		return false
	}
	out, err := execOutput("pgrep.children", "pgrep", "-P", strconv.Itoa(pid))
	if err != nil {
		return false
	}
	for _, line := range strings.Fields(string(out)) {
		child, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		cmd, err := execOutput("ps.command", "ps", "-o", "command=", "-p", line)
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
				if cached.Agent == "" {
					cached.Agent = config.DefaultAgent
				}
			}
			return cached
		}
	}

	info.Exists = PaneExists(info.Target)
	format := "#{pane_id}\t#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_tty}\t#{pane_current_command}\t#{pane_start_command}\t#{pane_current_path}"
	out, err := execOutput("tmux.pane-info", "tmux", "display-message", "-p", "-t", info.Target, "-F", format)
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
		info.Agent = config.DefaultAgent
	} else {
		info.Agent, info.HasPiAgent = agentForPane(info)
	}
	info.IsSubagent = isSubagentScriptLine(info.StartCommand) || isSubagentScriptLine(info.CurrentCommand)
	if !info.IsSubagent {
		info.IsSubagent = paneHasSubagentScriptOnTTY(info.Target)
	}
	return info
}

// paneHasSubagentScriptOnTTY: pi-interactive-subagents launch script with the
// pane's tty as controlling terminal.
func paneHasSubagentScriptOnTTY(target string) bool {
	tty, err := PaneTTY(target)
	if err != nil || tty == "" {
		return false
	}
	out, err := execOutput("ps.pane-tty-subagent", "ps", "-t", tty, "-o", "command=")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if isSubagentScriptLine(line) {
			return true
		}
	}
	return false
}

// ListPaneIDs returns all pane ids in the tmux server.
func ListPaneIDs() ([]string, error) {
	if snap, err := GetSnapshot(false); err == nil {
		if ids := snap.PaneIDs(); len(ids) > 0 {
			return ids, nil
		}
	}
	out, err := execOutput("tmux.list-panes-ids", "tmux", "list-panes", "-a", "-F", "#{pane_id}")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}
