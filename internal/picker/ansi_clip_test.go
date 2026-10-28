package picker

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// legacyTruncateANSI is the pre-cache path: a standalone width check followed
// by ansi.Truncate, which scans the line again.
func legacyTruncateANSI(line string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(line) <= width {
		return line
	}
	return ansi.Truncate(line, width, "")
}

// legacyClipLines mirrors clipLines before the width-scan collapse and memo.
func legacyClipLines(text string, width, rows int) string {
	if width < 1 || rows < 1 {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	for i, line := range lines {
		lines[i] = ensureResetSuffix(legacyTruncateANSI(line, width))
	}
	return strings.Join(lines, "\n")
}

func clipCases() []struct {
	name  string
	text  string
	width int
	rows  int
} {
	tail := "\t\x1b[1mbold tail\x1b[0m"
	return []struct {
		name  string
		text  string
		width int
		rows  int
	}{
		{"empty", "", 40, 5},
		{"plain short", "hello world", 40, 5},
		{"plain long", strings.Repeat("abcdefghij", 12), 20, 2},
		{"ansi short", "\x1b[31mred\x1b[0m\n\x1b[32mgreen\x1b[0m", 40, 2},
		{"ansi long", "\x1b[31m" + strings.Repeat("A", 60) + "\x1b[0m", 10, 1},
		{"ansi no reset", "\x1b[35m" + strings.Repeat("x", 50), 12, 1},
		{"wide runes", strings.Repeat("漢字テスト", 8), 15, 3},
		{"emoji", strings.Repeat("👩‍💻🚀", 9), 11, 2},
		{"tabs", "a\tb\tc\n" + strings.Repeat("tab\t", 20), 9, 2},
		{"combining", "é\u0301e\u0301\u0301 " + strings.Repeat("é", 30), 13, 2},
		{"rows clipped", strings.Join([]string{"one", "two", "three", "four"}, "\n"), 80, 2},
		{"crlf", "\x1b[31mred\r\nplain\r\n" + strings.Repeat("z", 30), 16, 3},
		{"exact width", strings.Repeat("m", 20), 20, 1},
		{"width one", "\x1b[31m" + strings.Repeat("W", 8) + "\x1b[0m", 1, 1},
		{"only ansi", "\x1b[31m\x1b[0m\x1b[1m", 4, 1},
		{"zwj split", "👨‍👩‍👧‍👦" + strings.Repeat("=", 12), 6, 1},
		{"tail marker", "keep\n" + tail, 12, 2},
	}
}

func TestClipLinesMatchesLegacyPath(t *testing.T) {
	for _, tc := range clipCases() {
		t.Run(tc.name, func(t *testing.T) {
			resetRenderMemos()
			first := clipLines(tc.text, tc.width, tc.rows)
			want := legacyClipLines(tc.text, tc.width, tc.rows)
			if first != want {
				t.Fatalf("cold clipLines mismatch\n got: %q\nwant: %q", first, want)
			}
			if cached := clipLines(tc.text, tc.width, tc.rows); cached != want {
				t.Fatalf("cached clipLines mismatch\n got: %q\nwant: %q", cached, want)
			}
		})
	}
}

func TestClipLinesZeroDims(t *testing.T) {
	resetRenderMemos()
	for _, tc := range []struct {
		width, rows int
	}{{0, 5}, {40, 0}, {-3, 4}, {4, -1}} {
		if got := clipLines("x", tc.width, tc.rows); got != "" {
			t.Fatalf("clipLines(%d,%d) = %q, want empty", tc.width, tc.rows, got)
		}
	}
}

func TestClipLinesCacheRespectsDims(t *testing.T) {
	resetRenderMemos()
	text := "\x1b[32m" + strings.Repeat("q", 40) + "\x1b[0m"
	narrow := clipLines(text, 10, 1)
	wide := clipLines(text, 30, 1)
	if narrow == wide {
		t.Fatalf("expected different clips per width, both %q", narrow)
	}
	if got := clipLines(text, 10, 1); got != narrow {
		t.Fatalf("width 10 clip changed after width 30 lookup: %q", got)
	}
	if got := clipLines(text, 30, 1); got != wide {
		t.Fatalf("width 30 clip changed after reuse: %q", got)
	}
}

func TestClipLinesCacheRespectsContent(t *testing.T) {
	resetRenderMemos()
	a := clipLines("alpha", 20, 1)
	b := clipLines("beta", 20, 1)
	if a != "alpha" || b != "beta" {
		t.Fatalf("content change reused stale clip: %q / %q", a, b)
	}
}

func TestClipLinesCacheStaysBounded(t *testing.T) {
	resetRenderMemos()
	for _, text := range []string{"one", "two", "three", "four"} {
		clipLines(text, 20, 1)
	}
	clipMemo.mu.Lock()
	used := 0
	for _, entry := range clipMemo.entries {
		if entry.valid {
			used++
		}
	}
	clipMemo.mu.Unlock()
	if used > memoSlots {
		t.Fatalf("cache holds %d entries, want <= %d", used, memoSlots)
	}
}

func TestRenderPreviewPaneMatchesUncached(t *testing.T) {
	texts := []string{"", "   ", legacyPlainPreview(), benchClipText(), "\x1b[31m" + strings.Repeat("w", 200)}
	for _, text := range texts {
		for _, size := range []struct{ cols, rows int }{{40, 10}, {140, 46}, {81, 12}} {
			for _, err := range []error{nil, errPreviewStub{}} {
				resetRenderMemos()
				got := renderPreviewPane(text, size.cols, size.rows, err, false)
				want := renderPreviewPaneUncached(text, size.cols, size.rows, err, false)
				if got != want {
					t.Fatalf("pane mismatch cols=%d rows=%d err=%v", size.cols, size.rows, err)
				}
				if again := renderPreviewPane(text, size.cols, size.rows, err, false); again != want {
					t.Fatalf("cached pane mismatch cols=%d rows=%d err=%v", size.cols, size.rows, err)
				}
				loading := renderPreviewPane(text, size.cols, size.rows, nil, true)
				if loading != renderPreviewPaneUncached(text, size.cols, size.rows, nil, true) {
					t.Fatalf("loading pane mismatch cols=%d rows=%d", size.cols, size.rows)
				}
			}
		}
	}
}

type errPreviewStub struct{}

func (errPreviewStub) Error() string { return "tmux capture failed" }

func legacyPlainPreview() string {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString("plain preview line ")
		b.WriteString(strings.Repeat("y", 30+i))
		b.WriteString("\n")
	}
	return b.String()
}

func TestTruncateANSIMatchesLegacy(t *testing.T) {
	for _, tc := range clipCases() {
		for _, line := range strings.Split(tc.text, "\n") {
			for _, width := range []int{1, 5, tc.width, tc.width * 2} {
				got := truncateANSI(line, width)
				want := legacyTruncateANSI(line, width)
				if got != want {
					t.Fatalf("truncateANSI(%q, %d) = %q, want %q", line, width, got, want)
				}
			}
		}
	}
}

func benchClipText() string {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString("\x1b[38;5;250mpane line ")
		b.WriteString(strings.Repeat("x", 40+i))
		b.WriteString("\x1b[0m\n")
	}
	return b.String()
}

func BenchmarkClipLinesLegacy160x46(b *testing.B) {
	text := benchClipText()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = legacyClipLines(text, 136, 46)
	}
}

func BenchmarkClipLinesCold160x46(b *testing.B) {
	text := benchClipText()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resetRenderMemos()
		_ = clipLines(text, 136, 46)
	}
}

func BenchmarkClipLinesCached160x46(b *testing.B) {
	text := benchClipText()
	resetRenderMemos()
	_ = clipLines(text, 136, 46)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = clipLines(text, 136, 46)
	}
}
