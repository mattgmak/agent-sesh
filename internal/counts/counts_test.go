package counts

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestLoadAndCountUsesCachedSnapshot(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	// No tmux on PATH: any exec attempt fails, so a passing count proves the
	// persisted snapshot answered the run.
	t.Setenv("PATH", t.TempDir())

	regPath := filepath.Join(stateHome, "agent-sesh", "sessions.json")
	now := time.Now().Format(time.RFC3339Nano)
	sessions := []registry.Session{
		{ID: "a", TmuxTarget: "%1", Agent: "pi", Status: registry.StatusWorking, UpdatedAt: now},
		{ID: "b", TmuxTarget: "%2", Agent: "pi", Status: registry.StatusHalted, UpdatedAt: now},
	}
	if err := registry.Save(regPath, sessions); err != nil {
		t.Fatal(err)
	}

	panes := `{"fetched_at":"` + time.Now().Format(time.RFC3339Nano) + `","panes":{
		"%1":{"Target":"%1","Exists":true,"HasPiAgent":true},
		"%2":{"Target":"%2","Exists":true,"HasPiAgent":true}}}`
	if err := os.WriteFile(filepath.Join(stateHome, "agent-sesh", "pane-snapshot.json"), []byte(panes), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadAndCount()
	if err != nil {
		t.Fatal(err)
	}
	want := registry.CategoryCounts{Attention: 1, Active: 1}
	if got != want {
		t.Fatalf("LoadAndCount() = %+v, want %+v", got, want)
	}
}

func TestWriteDefaultFormat(t *testing.T) {
	var buf bytes.Buffer
	counts := registry.CategoryCounts{Attention: 2, Active: 5, Idle: 1}
	if err := Write(&buf, Options{}, counts); err != nil {
		t.Fatal(err)
	}
	want := iconAttention + " 2 " + iconActive + " 5 " + iconIdle + " 1"
	if got := buf.String(); got != want {
		t.Fatalf("default output = %q, want %q", got, want)
	}
}

func TestWriteCustomFormat(t *testing.T) {
	var buf bytes.Buffer
	counts := registry.CategoryCounts{Attention: 1, Active: 0, Idle: 3}
	format := "{{.AttentionIcon}}{{.Attention}}|{{.IdleIcon}}{{.Idle}}"
	if err := Write(&buf, Options{Format: format}, counts); err != nil {
		t.Fatal(err)
	}
	want := iconAttention + "1|" + iconIdle + "3"
	if got := buf.String(); got != want {
		t.Fatalf("custom output = %q, want %q", got, want)
	}
}

func TestWriteInvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, Options{Format: "{{.Attention"}, registry.CategoryCounts{})
	if err == nil || !strings.Contains(err.Error(), "parse format template") {
		t.Fatalf("expected template parse error, got %v", err)
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	counts := registry.CategoryCounts{Attention: 1, Active: 2, Idle: 3}
	if err := Write(&buf, Options{JSON: true}, counts); err != nil {
		t.Fatal(err)
	}
	var decoded registry.CategoryCounts
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != counts {
		t.Fatalf("json decode = %+v, want %+v", decoded, counts)
	}
}
