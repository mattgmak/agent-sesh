package tmux

import (
	"strings"
	"testing"
)

func TestParsePSProcessList(t *testing.T) {
	input := `  100   1 /bin/zsh -l
  200 100 node /path/to/pi
  300 100 lazygit
`
	got, err := parsePSProcessList(input)
	if err != nil {
		t.Fatalf("parsePSProcessList: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[1].pid != 200 || got[1].ppid != 100 || !strings.Contains(got[1].command, "pi") {
		t.Fatalf("second process = %+v", got[1])
	}
}

func TestPiAgentsInProcessTreesFromParsedList(t *testing.T) {
	processes := []psProcess{
		{pid: 100, ppid: 1, command: "/bin/zsh"},
		{pid: 200, ppid: 100, command: "/usr/local/bin/pi run"},
		{pid: 300, ppid: 1, command: "/bin/bash"},
	}
	children := make(map[int][]int)
	commands := make(map[int]string)
	for _, proc := range processes {
		children[proc.ppid] = append(children[proc.ppid], proc.pid)
		commands[proc.pid] = proc.command
	}

	if !treeHasPi(100, 5, children, commands) {
		t.Fatal("expected pi under shell 100")
	}
	if treeHasPi(300, 5, children, commands) {
		t.Fatal("expected no pi under shell 300")
	}
}
