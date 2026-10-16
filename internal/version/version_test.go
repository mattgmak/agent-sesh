package version

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteStampedVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "0.1.0"

	var buf bytes.Buffer
	if err := Write(&buf); err != nil {
		t.Fatal(err)
	}
	want := "agent-sesh 0.1.0\n"
	if got := buf.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestStringFallsBack(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = ""

	if got := String(); got == "" {
		t.Fatal("String() = empty, want non-empty fallback")
	}
}

func TestWriteEndsWithNewline(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "agent-sesh ") || !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("output = %q, want \"agent-sesh <version>\\n\"", buf.String())
	}
}
