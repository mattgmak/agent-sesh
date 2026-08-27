package tmux

import (
	"strconv"
	"strings"
)

const defaultProcessTreeDepth = 5

type psProcess struct {
	pid     int
	ppid    int
	command string
}

// piAgentsInProcessTrees reports which root shell PIDs have a pi agent in their
// descendant tree. One ps invocation covers every root instead of per-pane pgrep.
func piAgentsInProcessTrees(roots []int, maxDepth int) map[int]bool {
	if len(roots) == 0 {
		return nil
	}
	if maxDepth <= 0 {
		maxDepth = defaultProcessTreeDepth
	}

	out, err := execOutput("ps.process-list", "ps", "-ax", "-o", "pid=,ppid=,command=")
	if err != nil {
		return nil
	}
	processes, err := parsePSProcessList(string(out))
	if err != nil || len(processes) == 0 {
		return nil
	}

	children := make(map[int][]int, len(processes))
	commands := make(map[int]string, len(processes))
	for _, proc := range processes {
		children[proc.ppid] = append(children[proc.ppid], proc.pid)
		commands[proc.pid] = proc.command
	}

	found := make(map[int]bool, len(roots))
	for _, root := range roots {
		if root <= 0 {
			continue
		}
		if treeHasPi(root, maxDepth, children, commands) {
			found[root] = true
		}
	}
	return found
}

func treeHasPi(pid, depth int, children map[int][]int, commands map[int]string) bool {
	if pid <= 0 || depth <= 0 {
		return false
	}
	if isPiProcessLine(commands[pid]) {
		return true
	}
	for _, child := range children[pid] {
		if treeHasPi(child, depth-1, children, commands) {
			return true
		}
	}
	return false
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
