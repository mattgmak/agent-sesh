package tmux

import "testing"

func TestIsSubagentScriptLine(t *testing.T) {
	subagent := []string{
		"bash /Users/x/.pi/agent/sessions/--Users-x-proj--/artifacts/abc123/subagent-scripts/worker-abc123.sh",
		"bash /var/folders/ab/cdef/T/pi-subagent-scripts/cmd-1710000000-1a2b3c.sh",
	}
	for _, line := range subagent {
		if !isSubagentScriptLine(line) {
			t.Fatalf("expected %q to match subagent launch script", line)
		}
	}

	regular := []string{
		"/usr/local/bin/pi --session /Users/x/.pi/sessions/proj/session.jsonl",
		"bash",
		"-zsh",
		"lazygit",
	}
	for _, line := range regular {
		if isSubagentScriptLine(line) {
			t.Fatalf("unexpected match for %q", line)
		}
	}
}

func TestTreeHasCommand(t *testing.T) {
	// 1 (bash script) -> 2 (pi) -> 3 (node)
	children := map[int][]int{1: {2}, 2: {3}}
	commands := map[int]string{
		1: "bash /x/.pi/agent/sessions/--y--/artifacts/a/subagent-scripts/worker-a.sh",
		2: "/usr/local/bin/pi --session /x/s.jsonl",
		3: "node /nix/store/abc-pi/bin/pi",
	}

	if !treeHasCommand(1, 5, children, commands, isPiProcessLine) {
		t.Fatal("expected pi in subtree of root 1")
	}
	if !treeHasCommand(1, 5, children, commands, isSubagentScriptLine) {
		t.Fatal("expected subagent launch script in subtree of root 1")
	}
	if !treeHasCommand(3, 5, children, commands, isPiProcessLine) {
		t.Fatal("expected pi at leaf 3")
	}

	misses := []struct {
		pid      int
		depth    int
		commands map[int]string
	}{
		// Script 2 levels down is invisible at depth 1.
		{1, 1, map[int]string{1: "bash", 2: "bash /x/subagent-scripts/y.sh"}},
		{99, 5, map[int]string{99: "bash"}},
		{3, 5, map[int]string{3: "nu"}},
	}
	for _, m := range misses {
		if treeHasCommand(m.pid, m.depth, children, m.commands, isSubagentScriptLine) {
			t.Fatalf("unexpected match for pid=%d depth=%d", m.pid, m.depth)
		}
	}
	// Same tree at depth 2 does see the script.
	if !treeHasCommand(1, 2, children, map[int]string{1: "bash", 2: "bash /x/subagent-scripts/y.sh"}, isSubagentScriptLine) {
		t.Fatal("expected script visible at depth 2")
	}
}
