package tmux

import (
	"testing"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestRegistrySanitizeOptionsDropsNonAgent(t *testing.T) {
	sessions := []registry.Session{
		{ID: "gone", TmuxTarget: "%1", Agent: "pi"},
		{ID: "live", TmuxTarget: "%2", Agent: "pi"},
	}

	opts := RegistrySanitizeOptions(&Snapshot{
		panes: map[string]PaneInfo{
			"%1": {Target: "%1", Exists: true, HasPiAgent: false},
			"%2": {Target: "%2", Exists: true, HasPiAgent: true},
		},
	})

	kept, removed := registry.Sanitize(sessions, opts)
	if len(kept) != 1 || kept[0].ID != "live" {
		t.Fatalf("kept = %+v", kept)
	}
	if len(removed) != 1 || removed[0].Reason != "agent not running in pane" {
		t.Fatalf("removed = %+v", removed)
	}
}
