package tmux

import (
	"testing"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestRegistrySanitizeOptionsDropsNonAgent(t *testing.T) {
	sessions := []registry.Session{
		{ID: "gone", TmuxTarget: "%1", Agent: "pi"},
		{ID: "live", TmuxTarget: "%2", Agent: "pi"},
		{ID: "sub", TmuxTarget: "%3", Agent: "pi"},
	}

	opts := RegistrySanitizeOptions(&Snapshot{
		panes: map[string]PaneInfo{
			"%1": {Target: "%1", Exists: true, HasPiAgent: false},
			"%2": {Target: "%2", Exists: true, HasPiAgent: true},
			// Subagent surfaces run pi but must be dropped from the registry.
			"%3": {Target: "%3", Exists: true, HasPiAgent: true, IsSubagent: true},
		},
	})

	kept, removed := registry.Sanitize(sessions, opts)
	if len(kept) != 1 || kept[0].ID != "live" {
		t.Fatalf("kept = %+v", kept)
	}
	if len(removed) != 2 {
		t.Fatalf("removed = %+v", removed)
	}
}
