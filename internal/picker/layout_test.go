package picker

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestVisibleCount(t *testing.T) {
	if got := visibleCount(24); got != 22 {
		t.Fatalf("visibleCount(24) = %d, want 22", got)
	}
	if got := visibleCount(1); got != fallbackVisibleCount {
		t.Fatalf("visibleCount(1) = %d, want fallback %d", got, fallbackVisibleCount)
	}
}

func TestRenderListFrameSeparatesEntries(t *testing.T) {
	items := sampleSessions()
	registry.SortSessions(items)
	out := renderListFrame(items, 0, 20, 58, listRenderOpts{showCursor: true}, formatSessionEntry)
	if !strings.Contains(out, "\n\n") {
		t.Fatalf("expected blank line between entries, got:\n%s", out)
	}
	if strings.Contains(out, "> ") {
		t.Fatalf("did not expect cursor chevron in list rows, got:\n%s", out)
	}
}

func TestRenderListFrameBottomAligned(t *testing.T) {
	items := sampleSessions()
	registry.SortSessions(items)
	visible := 12
	out := renderListFrame(items, 0, visible, 58, listRenderOpts{showCursor: true}, formatSessionEntry)
	lines := strings.Split(out, "\n")
	if len(lines) != visible {
		t.Fatalf("got %d lines, want %d", len(lines), visible)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "idle pane") {
		t.Fatalf("highest-priority idle session should be visible near bottom, got %q", joined)
	}
	if !strings.Contains(joined, "other") || !strings.Contains(joined, "go test") {
		t.Fatalf("tool_call session should remain visible, got %q", joined)
	}
}

func TestRenderListFrameFixedHeightWhenScrolling(t *testing.T) {
	items := sampleSessions()
	visible := 2
	out := renderListFrame(items, 2, visible, 58, listRenderOpts{showCursor: true}, formatSessionEntry)
	if strings.Count(out, "\n")+1 != visible {
		t.Fatalf("expected exactly %d lines, got:\n%s", visible, out)
	}
}

func TestClipLinesKeepsHead(t *testing.T) {
	in := strings.Join([]string{"one", "two", "three", "four"}, "\n")
	got := clipLines(in, 80, 2)
	want := "one\ntwo"
	if got != want {
		t.Fatalf("clipLines() = %q, want %q", got, want)
	}
}

func TestClipLinesPreservesANSI(t *testing.T) {
	in := "\x1b[31mred\x1b[0m\nplain"
	got := clipLines(in, 80, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
	}
	if !strings.Contains(lines[0], "\x1b[31m") {
		t.Fatalf("expected ansi preserved, got %q", lines[0])
	}
	if !strings.HasSuffix(lines[0], "\x1b[0m") {
		t.Fatalf("expected reset suffix on colored line, got %q", lines[0])
	}
}

func TestTruncateLineDoesNotWrap(t *testing.T) {
	long := strings.Repeat("x", 120)
	got := truncateLine(long, 20)
	if strings.Count(got, "\n") > 0 {
		t.Fatalf("truncateLine wrapped: %q", got)
	}
	if lipgloss.Width(got) > 20 {
		t.Fatalf("truncateLine width = %d, want <= 20", lipgloss.Width(got))
	}
}

func TestPadFrame(t *testing.T) {
	got := padFrame("a\nb", 4)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("len(lines) = %d, want 4", len(lines))
	}
	if lines[2] != "" || lines[3] != "" {
		t.Fatalf("expected trailing padding, got %#v", lines)
	}
}

func TestViewRendersSessionList(t *testing.T) {
	m := testModel(sampleSessions())
	m.width = 120
	m.height = 24
	m.syncInputWidth()

	out := viewContent(m)
	if !strings.Contains(out, "nixconfig") {
		t.Fatalf("expected rendered sessions, got:\n%s", out)
	}
}

func TestViewStableOnCursorMove(t *testing.T) {
	m := testModel(sampleSessions())
	m.width = 120
	m.height = 24
	m.syncInputWidth()

	before := strings.Split(viewContent(m), "\n")
	m = m.setCursor(1)
	after := strings.Split(viewContent(m), "\n")
	if len(before) != len(after) {
		t.Fatalf("cursor move changed frame height: %d -> %d", len(before), len(after))
	}
}

func TestViewNoPreviewWithoutSessions(t *testing.T) {
	m := testModel(nil)
	m.width = 120
	m.height = 24
	out := viewContent(m)
	if strings.Contains(out, "Loading preview") {
		t.Fatalf("did not expect preview without sessions, got:\n%s", out)
	}
}

func TestFormatAnchoredBodyBottomAligns(t *testing.T) {
	got := formatAnchoredBody(4, []string{"line1", "line2"})
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	if lines[0] != "" || lines[1] != "" {
		t.Fatalf("expected top padding, got %#v", lines)
	}
	if lines[2] != "line1" || lines[3] != "line2" {
		t.Fatalf("expected content at bottom, got %#v", lines)
	}
}

func TestFormatEmptyListMessageNoMatches(t *testing.T) {
	msg := formatEmptyListMessage(true)
	if !strings.Contains(msg, "No matching sessions") {
		t.Fatalf("expected no-match copy, got %q", msg)
	}
}

func TestViewFilterStaysInListColumn(t *testing.T) {
	m := testModel(sampleSessions())
	m.width = 120
	m.height = 24
	m.syncInputWidth()

	if !m.splitActive() {
		t.Fatal("expected split layout for wide terminal")
	}

	listWidth := m.contentWidth()
	out := viewContent(m)
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "> ") {
			continue
		}
		plain := strings.Map(func(r rune) rune {
			if r == '\x1b' {
				return -1
			}
			return r
		}, line)
		if idx := strings.Index(plain, "╰"); idx >= 0 && idx < listWidth {
			t.Fatalf("preview border intrudes into list column at %d (list=%d): %q", idx, listWidth, plain)
		}
		if lipgloss.Width(line[:min(len(line), listWidth*4)]) > 0 && strings.Index(plain, "> ") > listWidth {
			t.Fatalf("filter prompt outside list column: %q", plain)
		}
		return
	}
	t.Fatal("expected filter row in view output")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestLayoutWidthPassthrough(t *testing.T) {
	if got := layoutWidth(120); got != 120 {
		t.Fatalf("layoutWidth(120) = %d, want 120", got)
	}
}

func TestSplitActiveRequiresSessions(t *testing.T) {
	m := testModel(nil)
	m.width = 120
	if m.splitActive() {
		t.Fatal("split should be inactive with no sessions")
	}
	m.sessions = sampleSessions()
	if !m.splitActive() {
		t.Fatal("split should be active when sessions exist")
	}
}
