package tmux

import "testing"

func TestIsPiCommand(t *testing.T) {
	if !isPiCommand("pi", "") {
		t.Fatal("expected bare pi command")
	}
	if !isPiCommand("", "/usr/local/bin/pi --model sonnet") {
		t.Fatal("expected pi in start command")
	}
	if isPiCommand("lazygit", "") {
		t.Fatal("lazygit should not match")
	}
}

func TestNeedsDeepPiCheck(t *testing.T) {
	if !needsDeepPiCheck("bash") {
		t.Fatal("bash should need deep check")
	}
	if needsDeepPiCheck("node") {
		t.Fatal("node should not need deep check")
	}
}

func TestSnapshotHasPane(t *testing.T) {
	snap := &Snapshot{
		panes: map[string]PaneInfo{
			"%1": {Target: "%1", Exists: true},
		},
	}
	if !snap.HasPane("%1") || snap.HasPane("%2") {
		t.Fatalf("unexpected HasPane results")
	}
}

func TestPiPanesFilters(t *testing.T) {
	snap := &Snapshot{
		panes: map[string]PaneInfo{
			"%1": {Target: "%1", HasPiAgent: true},
			"%2": {Target: "%2", HasPiAgent: false},
		},
	}
	got := snap.PiPanes()
	if len(got) != 1 || got[0].Target != "%1" {
		t.Fatalf("PiPanes() = %+v", got)
	}
}
