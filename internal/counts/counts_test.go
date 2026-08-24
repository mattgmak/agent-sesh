package counts

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestWriteDefaultFormat(t *testing.T) {
	var buf bytes.Buffer
	counts := registry.CategoryCounts{Attention: 2, Active: 5, Idle: 1}
	if err := Write(&buf, Options{}, counts); err != nil {
		t.Fatal(err)
	}
	want := iconAttention + "2 " + iconActive + "5 " + iconIdle + "1"
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
