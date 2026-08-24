package registry

import (
	"testing"
	"time"
)

func TestMergeSessionsKeepsNewestPerTarget(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	a := []Session{
		{ID: "a", TmuxTarget: "%1", UpdatedAt: now.Add(-time.Hour).Format(time.RFC3339), Status: StatusIdle},
		{ID: "b", TmuxTarget: "%2", UpdatedAt: now.Format(time.RFC3339), Status: StatusWorking},
	}
	b := []Session{
		{ID: "c", TmuxTarget: "%1", UpdatedAt: now.Format(time.RFC3339), Status: StatusWorking},
	}

	got := MergeSessions(a, b)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	byTarget := map[string]Session{}
	for _, session := range got {
		byTarget[session.TmuxTarget] = session
	}
	if byTarget["%1"].ID != "c" || byTarget["%1"].Status != StatusWorking {
		t.Fatalf("target %%1 = %+v, want id c working", byTarget["%1"])
	}
	if byTarget["%2"].ID != "b" {
		t.Fatalf("target %%2 = %+v", byTarget["%2"])
	}
}

func TestFilterPersistableDropsDiscovered(t *testing.T) {
	sessions := []Session{
		{ID: "real", TmuxTarget: "%1"},
		{ID: "discovered:1", TmuxTarget: "%2"},
	}
	got := filterPersistable(sessions)
	if len(got) != 1 || got[0].ID != "real" {
		t.Fatalf("filterPersistable() = %+v", got)
	}
}
