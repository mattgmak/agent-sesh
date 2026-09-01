package registry

import "testing"

func TestShouldPersistSanitizePersistsAgentPrune(t *testing.T) {
	before := []Session{{ID: "a", TmuxTarget: "%1", Agent: "pi"}}
	after := []Session{}
	pruned := []PruneReason{{Reason: "agent not running in pane"}}

	if !ShouldPersistSanitize(before, after, pruned) {
		t.Fatal("expected agent prune to persist when session count changes")
	}
}

func TestShouldPersistSanitizeKeepsMissingPanePrune(t *testing.T) {
	before := []Session{{ID: "a", TmuxTarget: "%1", Agent: "pi"}}
	after := []Session{}
	pruned := []PruneReason{{Reason: "pane missing"}}

	if !ShouldPersistSanitize(before, after, pruned) {
		t.Fatal("expected pane-missing prune to persist")
	}
}
