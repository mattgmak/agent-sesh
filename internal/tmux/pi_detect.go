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
	tty     string
	command string
}

// scanProcesses runs the one ps listing every pane lookup needs. The rows carry
// pid, ppid, tty and command, so the tty map and the process trees share one
// fork instead of each paying its own.
func scanProcesses() ([]psProcess, error) {
	out, err := execOutput("ps.process-list", "ps", "-ax", "-o", "pid=,ppid=,tty=,command=")
	if err != nil {
		return nil, err
	}
	procs, err := parsePSProcessList(string(out))
	if err != nil {
		return nil, fmt.Errorf("parse ps process list: %w", err)
	}
	if len(procs) == 0 {
		return nil, fmt.Errorf("parse ps process list: no processes")
	}
	return procs, nil
}

// agentPresenceInProcessTrees reports the agent name each root has in its
// descendant tree (piRoots) and which roots have a pi-interactive-subagents
// launch script (subagentRoots). One ps invocation covers every root.
func agentPresenceInProcessTrees(procs []psProcess, roots []int, maxDepth int) (piRoots map[int]string, subagentRoots map[int]bool) {
	if len(roots) == 0 {
		return nil, nil
	}
	if maxDepth <= 0 {
		maxDepth = defaultProcessTreeDepth
	}

	children := make(map[int][]int, len(procs))
	commands := make(map[int]string, len(procs))
	for _, proc := range procs {
		children[proc.ppid] = append(children[proc.ppid], proc.pid)
		commands[proc.pid] = proc.command
	}

	piRoots = make(map[int]string)
	subagentRoots = make(map[int]bool)
	for _, root := range roots {
		if root <= 0 {
			continue
		}
		if command, ok := treeCommand(root, maxDepth, children, commands, isPiProcessLine); ok {
			if name, ok := agentProcessName(command); ok {
				piRoots[root] = name
			}
		}
		if treeHasCommand(root, maxDepth, children, commands, isSubagentScriptLine) {
			subagentRoots[root] = true
		}
	}
	return piRoots, subagentRoots
}

// treeCommand walks a process tree to a depth and returns the first command line
// the matcher accepts.
func treeCommand(pid, depth int, children map[int][]int, commands map[int]string, match func(string) bool) (string, bool) {
	if pid <= 0 || depth <= 0 {
		return "", false
	}
	if command := commands[pid]; match(command) {
		return command, true
	}
	for _, child := range children[pid] {
		if command, ok := treeCommand(child, depth-1, children, commands, match); ok {
			return command, true
		}
	}
	return "", false
}

func treeHasCommand(pid, depth int, children map[int][]int, commands map[int]string, match func(string) bool) bool {
	_, ok := treeCommand(pid, depth, children, commands, match)
	return ok
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
		if len(fields) < 4 {
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
			tty:     fields[2],
			command: strings.Join(fields[3:], " "),
		})
	}
	return out, nil
}
