package picker

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestDiscoveryTickOwnsTimerAndWorker(t *testing.T) {
	m := testModel(sampleSessions())
	updated, cmd := m.Update(discoveryTickMsg{})
	got := updated.(model)
	if cmd == nil {
		t.Fatal("expected discovery timer and reload command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("discovery tick command returned %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("discovery tick batch has %d commands, want 2", len(batch))
	}
	if got.discoveryReloadGen != 1 || !got.discoveryReloadInFlight {
		t.Fatalf("discovery state = gen %d, in-flight %v", got.discoveryReloadGen, got.discoveryReloadInFlight)
	}

	updated, cmd = got.Update(discoveryTickMsg{})
	got = updated.(model)
	if cmd == nil {
		t.Fatal("expected timer while discovery reload is in flight")
	}
	if got.discoveryReloadGen != 1 {
		t.Fatalf("overlapping tick started generation %d, want 1", got.discoveryReloadGen)
	}
}

func TestDiscoveryLoadedDoesNotScheduleTimer(t *testing.T) {
	tests := []struct {
		name    string
		message func([]registry.Session) discoveryLoadedMsg
	}{
		{
			name: "error",
			message: func([]registry.Session) discoveryLoadedMsg {
				return discoveryLoadedMsg{gen: 1, err: errors.New("discovery failed")}
			},
		},
		{
			name: "no-op",
			message: func(sessions []registry.Session) discoveryLoadedMsg {
				return discoveryLoadedMsg{gen: 1, sessions: sessions}
			},
		},
		{
			name: "changed",
			message: func([]registry.Session) discoveryLoadedMsg {
				return discoveryLoadedMsg{gen: 1, sessions: []registry.Session{{ID: "new", TmuxTarget: "%9"}}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel(sampleSessions())
			m.width = 0
			m.discoveryReloadGen = 1
			m.discoveryReloadInFlight = true

			updated, cmd := m.Update(tt.message(m.sessions))
			got := updated.(model)
			if cmd != nil {
				t.Fatal("discovery result scheduled a command with preview disabled")
			}
			if got.discoveryReloadInFlight {
				t.Fatal("matching discovery result left reload in flight")
			}
		})
	}
}

func TestDiscoveryLoadedIgnoresStaleGeneration(t *testing.T) {
	m := testModel(sampleSessions())
	m.discoveryReloadGen = 2
	m.discoveryReloadInFlight = true
	before := append([]registry.Session(nil), m.sessions...)

	updated, cmd := m.Update(discoveryLoadedMsg{
		gen:      1,
		sessions: []registry.Session{{ID: "stale", TmuxTarget: "%9"}},
	})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("stale discovery result returned a command")
	}
	if len(got.sessions) != len(before) || got.sessions[0].ID != before[0].ID {
		t.Fatalf("stale discovery result changed sessions: %+v", got.sessions)
	}
	if !got.discoveryReloadInFlight {
		t.Fatal("stale discovery result cleared current in-flight state")
	}
}
