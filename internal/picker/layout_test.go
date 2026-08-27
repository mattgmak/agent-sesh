package picker

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestRenderListFrameEmpty(t *testing.T) {
	out := renderListFrame(nil, 0, 5, 58, listRenderOpts{}, formatSessionEntry)
	if out == "" {
		t.Fatal("expected empty list message")
	}
}

func TestRenderListFrameFixedHeightWhenEmpty(t *testing.T) {
	out := renderListFrame([]registry.Session{}, 0, 3, 58, listRenderOpts{}, formatSessionEntry)
	if got := strings.Count(out, "\n") + 1; got != 3 {
		t.Fatalf("expected 3 lines in empty frame, got %d:\n%s", got, out)
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

func TestClipLinesKeepsTail(t *testing.T) {
	in := strings.Join([]string{"one", "two", "three", "four"}, "\n")
	got := clipLines(in, 80, 2)
	want := "three\nfour"
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
		t.Fatalf("expected ANSI reset suffix, got %q", lines[0])
	}
}

func TestClipLinesTruncatesWideANSI(t *testing.T) {
	in := "\x1b[31m" + strings.Repeat("A", 40) + "\x1b[0m"
	got := clipLines(in, 10, 1)
	wide := lipgloss.Width(got)
	if wide > 10 {
		t.Fatalf("clipLines wide=%d, want <=10: %q", wide, got)
	}
}

func TestClipLinesEmpty(t *testing.T) {
	if got := clipLines("", 80, 5); got != "" {
		t.Fatalf("clipLines empty = %q, want empty", got)
	}
	if got := clipLines("x", 0, 5); got != "" {
		t.Fatalf("clipLines zero width = %q, want empty", got)
	}
	if got := clipLines("x", 80, 0); got != "" {
		t.Fatalf("clipLines zero rows = %q, want empty", got)
	}
}

func TestListWindowBasic(t *testing.T) {
	offset, end := listWindow(0, 0, 5, 3)
	if offset != 0 || end != 3 {
		t.Fatalf("listWindow(0,0,5,3) = %d,%d, want 0,3", offset, end)
	}
}

func TestListWindowCursorPastEnd(t *testing.T) {
	offset, end := listWindow(10, 0, 5, 3)
	if offset != 2 || end != 5 {
		t.Fatalf("listWindow(10,0,5,3) = %d,%d, want 2,5", offset, end)
	}
}

func TestListWindowCursorBeforeOffset(t *testing.T) {
	offset, end := listWindow(1, 3, 8, 3)
	if offset != 1 || end != 4 {
		t.Fatalf("listWindow(1,3,8,3) = %d,%d, want 1,4", offset, end)
	}
}

func TestListWindowCursorPastVisibleEnd(t *testing.T) {
	offset, end := listWindow(5, 2, 8, 3)
	if offset != 3 || end != 6 {
		t.Fatalf("listWindow(5,2,8,3) = %d,%d, want 3,6", offset, end)
	}
}

func TestListWindowEmpty(t *testing.T) {
	offset, end := listWindow(0, 0, 0, 3)
	if offset != 0 || end != 0 {
		t.Fatalf("listWindow(empty) = %d,%d, want 0,0", offset, end)
	}
}

func TestRenderItemRangeSmallList(t *testing.T) {
	lo, hi := renderItemRange(5, 2, 3)
	if lo != 0 || hi != 5 {
		t.Fatalf("renderItemRange(5,2,3) = %d,%d, want 0,5", lo, hi)
	}
}

func TestRenderItemRangeLargeList(t *testing.T) {
	lo, hi := renderItemRange(50, 25, 20)
	if lo > 25 || hi <= 25 {
		t.Fatalf("renderItemRange(50,25,20) = %d,%d, cursor 25 should be inside [%d,%d)", lo, hi, lo, hi)
	}
	if hi-lo > 24 {
		t.Fatalf("renderItemRange(50,25,20) span=%d, want <=24", hi-lo)
	}
}

func TestTruncateANSILine(t *testing.T) {
	got := truncateLine("hello world", 5)
	if lipgloss.Width(got) > 5 {
		t.Fatalf("truncateLine width=%d > 5: %q", lipgloss.Width(got), got)
	}
}

func TestRenderPreviewPaneEmptyContent(t *testing.T) {
	out := renderPreviewPane("", 40, 10, nil, false)
	if !strings.Contains(out, "No preview") {
		t.Fatalf("expected 'No preview' message, got %q", out)
	}
}

func TestRenderPreviewPaneLoading(t *testing.T) {
	out := renderPreviewPane("", 40, 10, nil, true)
	if !strings.Contains(out, "Loading") {
		t.Fatalf("expected loading message, got %q", out)
	}
}

func TestPadFrame(t *testing.T) {
	got := padFrame("one", 3)
	if strings.Count(got, "\n")+1 != 3 {
		t.Fatalf("padFrame(one, 3) lines=%d, want 3:\n%s", strings.Count(got, "\n")+1, got)
	}
}

func TestPadFrameTruncates(t *testing.T) {
	got := padFrame("one\ntwo\nthree\nfour", 2)
	if strings.Count(got, "\n")+1 != 2 {
		t.Fatalf("padFrame truncate lines=%d, want 2:\n%s", strings.Count(got, "\n")+1, got)
	}
}
