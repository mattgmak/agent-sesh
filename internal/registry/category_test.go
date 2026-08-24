package registry

import "testing"

func TestStatusCategory(t *testing.T) {
	tests := []struct {
		status Status
		want   Category
	}{
		{StatusHalted, CategoryAttention},
		{StatusAwaitingInput, CategoryAttention},
		{StatusWorking, CategoryActive},
		{StatusToolCall, CategoryActive},
		{StatusIdle, CategoryIdle},
		{Status("unknown"), CategoryIdle},
	}
	for _, tc := range tests {
		if got := StatusCategory(tc.status); got != tc.want {
			t.Errorf("StatusCategory(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestCountByCategory(t *testing.T) {
	sessions := []Session{
		{ID: "a", Status: StatusHalted},
		{ID: "b", Status: StatusAwaitingInput},
		{ID: "c", Status: StatusWorking},
		{ID: "d", Status: StatusToolCall},
		{ID: "e", Status: StatusIdle},
		{ID: "f", Status: StatusIdle},
	}
	got := CountByCategory(sessions)
	want := CategoryCounts{Attention: 2, Active: 2, Idle: 2}
	if got != want {
		t.Fatalf("CountByCategory() = %+v, want %+v", got, want)
	}
}
