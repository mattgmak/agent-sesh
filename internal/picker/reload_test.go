package picker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestReloadMergesRegistryFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	sessions := []registry.Session{{
		ID:          "1",
		TmuxTarget:  "%1",
		TmuxSession: "nixconfig",
		Status:      registry.StatusIdle,
	}}
	if err := registry.Save(path, sessions); err != nil {
		t.Fatalf("save: %v", err)
	}

	m := testModel(sessions)
	m.registry = path
	m.syncSessionsRenderKey()

	fresh := []registry.Session{{
		ID:         "1",
		TmuxTarget: "%1",
		Status:     registry.StatusWorking,
		ToolName:   "Shell",
	}}
	if err := registry.Save(path, fresh); err != nil {
		t.Fatalf("save fresh: %v", err)
	}

	m = m.reload()
	if m.sessions[0].Status != registry.StatusWorking || m.sessions[0].ToolName != "Shell" {
		t.Fatalf("reload merge = %+v", m.sessions[0])
	}
}

func TestReloadNoopWhenRenderKeyUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	sessions := sampleSessions()
	registry.SortSessions(sessions)
	if err := registry.Save(path, sessions); err != nil {
		t.Fatalf("save: %v", err)
	}

	m := testModel(sessions)
	m.registry = path
	m.syncSessionsRenderKey()
	m = m.reload()
	before := m.sessionsRenderKey

	m = m.reload()
	if m.sessionsRenderKey != before {
		t.Fatalf("render key changed on noop reload")
	}
}

func TestRegistryFileChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	if err := registry.Save(path, sampleSessions()); err != nil {
		t.Fatalf("save: %v", err)
	}

	m := testModel(sampleSessions())
	m.registry = path
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	m.registryMtime = info.ModTime()
	if m.registryFileChanged() {
		t.Fatal("expected no change when mtime matches")
	}

	time.Sleep(10 * time.Millisecond)
	if err := registry.Save(path, sampleSessions()); err != nil {
		t.Fatalf("save again: %v", err)
	}
	if !m.registryFileChanged() {
		t.Fatal("expected change after rewrite")
	}
}
