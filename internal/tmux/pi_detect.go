package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

const defaultProcessTreeDepth = 5

type psProcess struct {
	pid     int
	ppid    int
	command string
}

// agentPresenceInProcessTrees reports which roots have a pi agent (piRoots) and
// which have a pi-interactive-subagents launch script (subagentRoots) in their
// descendant tree. One ps invocation covers every root.
func agentPresenceInProcessTrees(roots []int, maxDepth int) (piRoots, subagentRoots map[int]bool, err error) {
	if len(roots) == 0 {
		return nil, nil, nil
	}
	if maxDepth <= 0 {
		maxDepth = defaultProcessTreeDepth
	}

	out, err := execOutput("ps.process-list", "ps", "-ax", "-o", "pid=,ppid=,command=")
	if err != nil {
		return nil, nil, err
	}
	processes, err := parsePSProcessList(string(out))
	if err != nil {
		return nil, nil, fmt.Errorf("parse ps process list: %w", err)
	}
	if len(processes) == 0 {
		return nil, nil, fmt.Errorf("parse ps process list: no processes")
	}

	children := make(map[int][]int, len(processes))
	commands := make(map[int]string, len(processes))
	for _, proc := range processes {
		children[proc.ppid] = append(children[proc.ppid], proc.pid)
		commands[proc.pid] = proc.command
	}

	piRoots = make(map[int]bool)
	subagentRoots = make(map[int]bool)
	for _, root := range roots {
		if root <= 0 {
			continue
		}
		if treeHasCommand(root, maxDepth, children, commands, isPiProcessLine) {
			piRoots[root] = true
		}
		if treeHasCommand(root, maxDepth, children, commands, isSubagentScriptLine) {
			subagentRoots[root] = true
		}
	}
	return piRoots, subagentRoots, nil
}

func treeHasCommand(pid, depth int, children map[int][]int, commands map[int]string, match func(string) bool) bool {
	if pid <= 0 || depth <= 0 {
		return false
	}
	if match(commands[pid]) {
		return true
	}
	for _, child := range children[pid] {
		if treeHasCommand(child, depth-1, children, commands, match) {
			return true
		}
	}
	return false
}

// subagentScriptMarker matches pi-interactive-subagents launch-script paths:
// `<agentDir>/sessions/.../artifacts/<id>/subagent-scripts/<name>-<id>.sh` and
// old `$TMPDIR/pi-subagent-scripts/cmd-*.sh` (substring).
const subagentScriptMarker = "subagent-scripts"

// isSubagentScriptLine reports whether a command line runs a
// pi-interactive-subagents launch script.
func isSubagentScriptLine(line string) bool {
	return strings.Contains(line, subagentScriptMarker)
}

func parsePSProcessList(output string) ([]psProcess, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	out := make([]psProcess, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		out = append(out, psProcess{
			pid:     pid,
			ppid:    ppid,
			command: strings.Join(fields[2:], " "),
		})
	}
	return out, nil
}
