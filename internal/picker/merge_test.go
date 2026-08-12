package picker

import (
	"testing"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestMergeSessionsIncrementalPreservesDiscoveredID(t *testing.T) {
	current := []registry.Session{{
		ID:         "discovered:1",
		TmuxTarget: "%1",
		Status:     registry.StatusIdle,
	}}
	loaded := []registry.Session{{
		ID:         "discovered:1",
		TmuxTarget: "%1",
		Status:     registry.StatusIdle,
		Title:      "path",
	}}

	got := mergeSessionsIncremental(current, loaded)
	if len(got) != 1 || got[0].ID != "discovered:1" {
		t.Fatalf("mergeSessionsIncremental() = %+v", got)
	}
}

func TestMergeSessionsIncrementalAddsNewTargets(t *testing.T) {
	current := []registry.Session{{
		ID:         "1",
		TmuxTarget: "%1",
		Status:     registry.StatusWorking,
	}}
	loaded := []registry.Session{
		{ID: "1", TmuxTarget: "%1", Status: registry.StatusWorking},
		{ID: "2", TmuxTarget: "%2", Status: registry.StatusHalted},
	}

	got := mergeSessionsIncremental(current, loaded)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestMergeSessionsIncrementalUpgradesDiscoveredToRegistryID(t *testing.T) {
	current := []registry.Session{{
		ID:         "discovered:1",
		TmuxTarget: "%1",
		Status:     registry.StatusIdle,
	}}
	loaded := []registry.Session{{
		ID:         "pi-uuid",
		TmuxTarget: "%1",
		Status:     registry.StatusWorking,
	}}

	got := mergeSessionsIncremental(current, loaded)
	if len(got) != 1 || got[0].ID != "pi-uuid" {
		t.Fatalf("mergeSessionsIncremental() = %+v", got)
	}
}
