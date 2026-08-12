package picker

import (
	"testing"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestRefreshSessionsFromRegistrySorts(t *testing.T) {
	current := []registry.Session{
		{ID: "working", TmuxTarget: "%1", Status: registry.StatusWorking},
		{ID: "waiting", TmuxTarget: "%2", Status: registry.StatusHalted},
	}
	fresh := []registry.Session{
		{ID: "working", TmuxTarget: "%1", Status: registry.StatusWorking, ToolName: "Shell"},
		{ID: "waiting", TmuxTarget: "%2", Status: registry.StatusHalted},
	}

	got := refreshSessionsFromRegistry(current, fresh)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "waiting" || got[1].ID != "working" {
		t.Fatalf("refreshSessionsFromRegistry() = %+v, want waiting before working", got)
	}
}
