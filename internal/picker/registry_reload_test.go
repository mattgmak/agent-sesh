package picker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestRegistryReloadMsgReturnsCommandWithoutApplying(t *testing.T) {
	current := sampleSessions()
	next := []registry.Session{{ID: "new", TmuxTarget: "%9", Status: registry.StatusHalted}}
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := registry.Save(path, next); err != nil {
		t.Fatal(err)
	}

	m := testModel(current)
	m.registry = path
	m.registryReloadPending = true
	m.statusLine = "unchanged"
	m.syncSessionsRenderKey()

	updated, cmd := m.Update(registryReloadMsg{})
	got := updated.(model)
	if cmd == nil {
		t.Fatal("expected registry reload command")
	}
	if len(got.sessions) != len(current) || got.sessions[0].ID != current[0].ID {
		t.Fatalf("registry reload trigger applied sessions: %+v", got.sessions)
	}
	if got.statusLine != "unchanged" {
		t.Fatalf("statusLine = %q, want unchanged", got.statusLine)
	}
	if got.registryReloadGen != 1 || !got.registryReloadInFlight || !got.registryReloadPending {
		t.Fatalf("reload state = gen %d, in-flight %v, pending %v", got.registryReloadGen, got.registryReloadInFlight, got.registryReloadPending)
	}
}

func TestRegistryReloadedMsgAppliesResult(t *testing.T) {
	current := sampleSessions()
	next := []registry.Session{{ID: "new", TmuxTarget: "%9", Status: registry.StatusHalted}}
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	m := testModel(current)
	m.registry = path
	m.width = 0
	m.registryReloadGen = 1
	m.registryReloadInFlight = true
	m.registryReloadPending = true
	m.statusLine = "previous status"
	m.syncSessionsRenderKey()

	updated, cmd := m.Update(registryReloadedMsg{
		gen:      1,
		baseKey:  m.sessionsRenderKey,
		sessions: next,
	})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("expected no timer or preview command with preview disabled")
	}
	if len(got.sessions) != 1 || got.sessions[0].ID != "new" || got.selectedID != "new" {
		t.Fatalf("reload result not applied: sessions=%+v selected=%q", got.sessions, got.selectedID)
	}
	if got.statusLine != "previous status" {
		t.Fatalf("successful reload cleared statusLine: %q", got.statusLine)
	}
	if !got.registryMtime.Equal(info.ModTime()) {
		t.Fatalf("registryMtime = %v, want %v", got.registryMtime, info.ModTime())
	}
	if got.registryReloadInFlight || got.registryReloadPending {
		t.Fatalf("reload state remained in-flight: in-flight=%v pending=%v", got.registryReloadInFlight, got.registryReloadPending)
	}
}

func TestRegistryReloadedMsgErrorSetsStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := testModel(sampleSessions())
	m.registry = path
	m.registryReloadGen = 1
	m.registryReloadInFlight = true
	m.registryReloadPending = true
	m.syncSessionsRenderKey()
	before := append([]registry.Session(nil), m.sessions...)

	updated, cmd := m.Update(registryReloadedMsg{
		gen:     1,
		baseKey: m.sessionsRenderKey,
		err:     errors.New("reload failed"),
	})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("expected no command after reload error")
	}
	if got.statusLine != "reload failed" {
		t.Fatalf("statusLine = %q, want reload failed", got.statusLine)
	}
	if !got.registryMtime.IsZero() {
		t.Fatalf("failed reload marked registry current at %v", got.registryMtime)
	}
	if !got.registryFileChanged() {
		t.Fatal("failed reload suppressed next registry change check")
	}
	if len(got.sessions) != len(before) || got.sessions[0].ID != before[0].ID {
		t.Fatalf("reload error changed sessions: %+v", got.sessions)
	}
	if got.registryReloadInFlight || got.registryReloadPending {
		t.Fatalf("failed reload remained in-flight: in-flight=%v pending=%v", got.registryReloadInFlight, got.registryReloadPending)
	}
}

func TestRegistryReloadedMsgIgnoresStaleGeneration(t *testing.T) {
	m := testModel(sampleSessions())
	m.registryReloadGen = 2
	m.registryReloadInFlight = true
	m.registryReloadPending = true
	m.syncSessionsRenderKey()
	before := append([]registry.Session(nil), m.sessions...)

	updated, cmd := m.Update(registryReloadedMsg{
		gen:      1,
		baseKey:  m.sessionsRenderKey,
		sessions: []registry.Session{{ID: "stale", TmuxTarget: "%9"}},
	})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("stale result returned a command")
	}
	if len(got.sessions) != len(before) || got.sessions[0].ID != before[0].ID {
		t.Fatalf("stale result changed sessions: %+v", got.sessions)
	}
	if !got.registryReloadInFlight || !got.registryReloadPending {
		t.Fatal("stale result cleared current reload state")
	}
}

func TestRegistryReloadedMsgIgnoresStaleBaseKey(t *testing.T) {
	m := testModel(sampleSessions())
	m.registryReloadGen = 1
	m.registryReloadInFlight = true
	m.registryReloadPending = true
	m.syncSessionsRenderKey()
	before := append([]registry.Session(nil), m.sessions...)

	updated, cmd := m.Update(registryReloadedMsg{
		gen:      1,
		baseKey:  "stale-render-key",
		sessions: []registry.Session{{ID: "stale", TmuxTarget: "%9"}},
	})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("stale-base result returned a command")
	}
	if len(got.sessions) != len(before) || got.sessions[0].ID != before[0].ID {
		t.Fatalf("stale-base result changed sessions: %+v", got.sessions)
	}
	if got.registryReloadInFlight || got.registryReloadPending {
		t.Fatal("completed stale-base reload remained in-flight")
	}
}
