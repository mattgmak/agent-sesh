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

func TestMergeSessionsDropsStaleRowForSameSessionID(t *testing.T) {
	now := time.Date(2026, 10, 16, 8, 24, 0, 0, time.UTC)
	stale := []Session{
		{ID: "01a13dbe", TmuxTarget: "%175", UpdatedAt: now.Add(-time.Minute).Format(time.RFC3339), PiSessionName: "masta"},
	}
	current := []Session{
		{ID: "01a13dbe", TmuxTarget: "%11", UpdatedAt: now.Format(time.RFC3339), PiSessionName: "masta"},
		{ID: "01a14328", TmuxTarget: "%112", UpdatedAt: now.Format(time.RFC3339)},
	}

	got := MergeSessions(stale, current)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
	}
	byID := map[string]Session{}
	for _, session := range got {
		byID[session.ID] = session
	}
	if byID["01a13dbe"].TmuxTarget != "%11" {
		t.Fatalf("session 01a13dbe = %+v, want newest target %%11", byID["01a13dbe"])
	}
	if byID["01a14328"].TmuxTarget != "%112" {
		t.Fatalf("session 01a14328 = %+v", byID["01a14328"])
	}
}

func TestMergeSessionsKeepsIdlessRows(t *testing.T) {
	got := MergeSessions([]Session{{TmuxTarget: "%1"}, {TmuxTarget: "%2"}})
	if len(got) != 2 {
		t.Fatalf("MergeSessions() = %+v, want both id-less rows", got)
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
