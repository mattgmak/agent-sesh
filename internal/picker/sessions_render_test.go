package picker

import (
	"testing"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestSessionsRenderKeyIgnoresBookkeepingFields(t *testing.T) {
	a := []registry.Session{{
		ID:           "1",
		TmuxTarget:   "%1",
		Status:       registry.StatusWorking,
		ToolName:     "Shell",
		Title:        "test",
		LastPrompt:   "hello",
		UpdatedAt:    "2026-01-01T00:00:00Z",
		LastPromptAt: "2026-01-01T00:00:00Z",
		Model:        "claude",
	}}
	b := []registry.Session{{
		ID:           "1",
		TmuxTarget:   "%1",
		Status:       registry.StatusWorking,
		ToolName:     "Shell",
		Title:        "test",
		LastPrompt:   "hello",
		UpdatedAt:    "2026-01-02T00:00:00Z",
		LastPromptAt: "2026-01-02T00:00:00Z",
		Model:        "gpt",
	}}
	if sessionsRenderKey(a) != sessionsRenderKey(b) {
		t.Fatal("expected bookkeeping-only changes to keep the same render key")
	}
}

func TestSessionsRenderKeyDetectsStatusChange(t *testing.T) {
	idle := []registry.Session{{ID: "1", TmuxTarget: "%1", Status: registry.StatusIdle}}
	working := []registry.Session{{ID: "1", TmuxTarget: "%1", Status: registry.StatusWorking}}
	if sessionsRenderKey(idle) == sessionsRenderKey(working) {
		t.Fatal("expected status change to affect render key")
	}
}

func TestApplySessionsIfChangedSkipsNoopReload(t *testing.T) {
	m := testModel(sampleSessions())
	m.syncSessionsRenderKey()

	next := append([]registry.Session(nil), m.sessions...)
	next[0].UpdatedAt = "2099-01-01T00:00:00Z"
	next[0].Model = "other-model"

	if m.applySessionsIfChanged(next) {
		t.Fatal("expected bookkeeping-only reload to be ignored")
	}
}

func TestApplySessionsIfChangedAcceptsVisibleChange(t *testing.T) {
	m := testModel(sampleSessions())
	m.syncSessionsRenderKey()

	next := append([]registry.Session(nil), m.sessions...)
	next[0].Status = registry.StatusToolCall
	next[0].ToolName = "Read"

	if !m.applySessionsIfChanged(next) {
		t.Fatal("expected visible status change to update sessions")
	}
	if m.sessions[0].Status != registry.StatusToolCall {
		t.Fatalf("sessions not updated: %+v", m.sessions[0])
	}
}
