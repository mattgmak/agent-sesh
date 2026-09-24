package picker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mattgmak/agent-sesh/internal/registry"
	"github.com/mattgmak/agent-sesh/internal/tmux"
)

func TestDisplaySessionsDoesNotEnrichDuringRender(t *testing.T) {
	m := testModel(sampleSessions())
	before := append([]registry.Session(nil), m.sessions...)
	got := m.displaySessions()
	if len(got) != len(before) {
		t.Fatalf("len(displaySessions()) = %d, want %d", len(got), len(before))
	}
	for i := range before {
		if got[i] != before[i] {
			t.Fatalf("displaySessions mutated row %d: got %+v want %+v", i, got[i], before[i])
		}
	}
}

func TestSessionPaneCoordsUsesStoredFieldsOnly(t *testing.T) {
	window, pane, ok := sessionPaneCoords(registry.Session{TmuxPane: "1"})
	if ok || window != "" || pane != "1" {
		t.Fatalf("incomplete coords = (%q, %q, %v), want (\"\", \"1\", false)", window, pane, ok)
	}

	window, pane, ok = sessionPaneCoords(registry.Session{TmuxWindow: " 2 ", TmuxPane: " 1 "})
	if !ok || window != "2" || pane != "1" {
		t.Fatalf("complete coords = (%q, %q, %v), want (\"2\", \"1\", true)", window, pane, ok)
	}
}

func TestViewDoesNotExecuteTmux(t *testing.T) {
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "tmux-called")
	script := "#!/bin/sh\nprintf called > '" + marker + "'\n"
	if err := os.WriteFile(filepath.Join(binDir, "tmux"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	tmux.InvalidateSnapshot()
	t.Cleanup(tmux.InvalidateSnapshot)

	m := testModel(sampleSessions())
	m.syncInputWidth()
	viewContent(m)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("View invoked tmux; marker stat error = %v", err)
	}
}
