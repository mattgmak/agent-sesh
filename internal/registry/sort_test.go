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
		{StatusAwaitingInput, 0},
		{StatusHalted, 1},
		{StatusUnknown, 2},
		{StatusIdle, 3},
		{StatusBgJobsRunning, 4},
		{StatusSubagentsRunning, 5},
		{StatusToolCall, 6},
		{StatusWorking, 7},
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

	want := []string{"waiting", "idle", "tool", "working"}
	for i, id := range want {
		if sessions[i].ID != id {
			t.Fatalf("sessions[%d].ID = %q, want %q (full=%+v)", i, sessions[i].ID, id, sessions)
		}
	}
}

func TestSortSessionsBusyCountersOrder(t *testing.T) {
	sessions := []Session{
		{ID: "bg", Status: StatusBgJobsRunning},
		{ID: "tool", Status: StatusToolCall},
		{ID: "subagents", Status: StatusSubagentsRunning},
		{ID: "working", Status: StatusWorking},
	}

	SortSessions(sessions)

	want := []string{"bg", "subagents", "tool", "working"}
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

// displayOrder renders the top-to-bottom on-screen order: the picker draws
// index 0 at the bottom of the list.
func displayOrder(sessions []Session) []string {
	out := make([]string, 0, len(sessions))
	for i := len(sessions) - 1; i >= 0; i-- {
		out = append(out, sessions[i].ID)
	}
	return out
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q (got=%v)", i, got[i], want[i], got)
		}
	}
}

func TestSortSessionsPinsDisplayOrder(t *testing.T) {
	sessions := []Session{
		{ID: "awaiting", Status: StatusAwaitingInput},
		{ID: "halted", Status: StatusHalted},
		{ID: "unknown", Status: StatusUnknown},
		{ID: "idle", Status: StatusIdle},
		{ID: "bg", Status: StatusBgJobsRunning},
		{ID: "subagents", Status: StatusSubagentsRunning},
		{ID: "tool", Status: StatusToolCall},
		{ID: "working", Status: StatusWorking},
	}

	SortSessions(sessions)

	// Top of the list first. The active group sits above idle.
	want := []string{"working", "tool", "subagents", "bg", "idle", "unknown", "halted", "awaiting"}
	assertOrder(t, displayOrder(sessions), want)
}

func TestSortSessionsRanksHaltedBusyRowAsActive(t *testing.T) {
	sessions := []Session{
		{ID: "idle", Status: StatusIdle},
		{ID: "halted", Status: StatusHalted},
		{ID: "halted-busy", Status: StatusHalted, SubagentsRunning: 2},
		{ID: "tool", Status: StatusToolCall},
	}

	SortSessions(sessions)

	// The resolved status of halted-busy is subagents_running, so it joins the
	// active group and renders above idle.
	want := []string{"tool", "halted-busy", "idle", "halted"}
	assertOrder(t, displayOrder(sessions), want)
}
