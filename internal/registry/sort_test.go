package registry

import (
	"testing"
	"time"
)

func TestStatusPriority(t *testing.T) {
	tests := []struct {
		status Status
		want   int
	}{
		{StatusHalted, 0},
		{StatusAwaitingInput, 1},
		{StatusUnknown, 2},
		{StatusIdle, 3},
		{StatusWorking, 4},
		{StatusToolCall, 5},
		{Status("bogus"), 3},
	}
	for _, tc := range tests {
		if got := StatusPriority(tc.status); got != tc.want {
			t.Errorf("StatusPriority(%q) = %d, want %d", tc.status, got, tc.want)
		}
	}
}

func TestSortSessionsByStatus(t *testing.T) {
	sessions := []Session{
		{ID: "working", Status: StatusWorking},
		{ID: "waiting", Status: StatusHalted},
		{ID: "tool", Status: StatusToolCall},
		{ID: "idle", Status: StatusIdle},
	}

	SortSessions(sessions)

	want := []string{"waiting", "idle", "working", "tool"}
	for i, id := range want {
		if sessions[i].ID != id {
			t.Fatalf("sessions[%d].ID = %q, want %q (full=%+v)", i, sessions[i].ID, id, sessions)
		}
	}
}

func TestSortSessionsMRUWithinStatus(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sessions := []Session{
		{ID: "older", Status: StatusHalted, LastPromptAt: now.Add(-time.Hour).Format(time.RFC3339)},
		{ID: "newer", Status: StatusHalted, LastPromptAt: now.Format(time.RFC3339)},
		{ID: "idle-old", Status: StatusIdle, LastPromptAt: now.Add(-2 * time.Hour).Format(time.RFC3339)},
		{ID: "idle-new", Status: StatusIdle, LastPromptAt: now.Add(-30 * time.Minute).Format(time.RFC3339)},
	}

	SortSessions(sessions)

	want := []string{"newer", "older", "idle-new", "idle-old"}
	for i, id := range want {
		if sessions[i].ID != id {
			t.Fatalf("sessions[%d].ID = %q, want %q (full=%+v)", i, sessions[i].ID, id, sessions)
		}
	}
}

func TestSortSessionsIgnoresStatusUpdatesForMRU(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sessions := []Session{
		{
			ID:           "recent-status",
			Status:       StatusHalted,
			UpdatedAt:    now.Format(time.RFC3339),
			LastPromptAt: now.Add(-2 * time.Hour).Format(time.RFC3339),
		},
		{
			ID:           "recent-prompt",
			Status:       StatusHalted,
			UpdatedAt:    now.Add(-time.Hour).Format(time.RFC3339),
			LastPromptAt: now.Add(-30 * time.Minute).Format(time.RFC3339),
		},
	}

	SortSessions(sessions)

	if sessions[0].ID != "recent-prompt" {
		t.Fatalf("sessions[0].ID = %q, want recent-prompt (full=%+v)", sessions[0].ID, sessions)
	}
}
