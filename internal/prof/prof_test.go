package prof

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestProfileAppendsAcrossInvocations(t *testing.T) {
	logPath := t.TempDir() + "/profile.log"
	t.Setenv("AGENT_SESH_PROFILE", logPath)

	path1, err := Init()
	if err != nil {
		t.Fatalf("Init first: %v", err)
	}
	if path1 != logPath {
		t.Fatalf("Init path = %q, want %q", path1, logPath)
	}
	done := Start("first-op")
	time.Sleep(time.Millisecond)
	done()
	p1 := Close()
	if p1 != logPath {
		t.Fatalf("Close path = %q, want %q", p1, logPath)
	}

	path2, err := Init()
	if err != nil {
		t.Fatalf("Init second: %v", err)
	}
	if path2 != logPath {
		t.Fatalf("Init path = %q, want %q", path2, logPath)
	}
	done = Start("second-op")
	time.Sleep(time.Millisecond)
	done()
	p2 := Close()
	if p2 != logPath {
		t.Fatalf("Close path = %q, want %q", p2, logPath)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read profile log: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "first-op") {
		t.Fatalf("expected first-op in log, got:\n%s", text)
	}
	if !strings.Contains(text, "second-op") {
		t.Fatalf("expected second-op in log, got:\n%s", text)
	}
	startCount := strings.Count(text, "profile started")
	endCount := strings.Count(text, "profile ended")
	if startCount != 2 || endCount != 2 {
		t.Fatalf("expected 2 session starts/ends, got starts=%d ends=%d:\n%s", startCount, endCount, text)
	}
}
