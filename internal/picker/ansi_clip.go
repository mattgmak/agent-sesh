package picker

import (
	"hash/maphash"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// truncateANSI shortens visible width without stripping embedded SGR sequences.
func truncateANSI(line string, width int) string {
	if width < 1 {
		return ""
	}
	// Cell width never exceeds byte length, so a line that fits in bytes needs
	// no width scan at all.
	if len(line) <= width {
		return line
	}
	return ansi.Truncate(line, width, "")
}

// clipLine clips one line with a single width scan. ansi.Truncate returns the
// line unchanged when it already fits, so it replaces a separate width check.
func clipLine(line string, width int) string {
	if width < 1 {
		return ""
	}
	if len(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	return ensureResetSuffix(line)
}

// ensureResetSuffix keeps capture-pane colors from bleeding into chrome.
func ensureResetSuffix(line string) string {
	if strings.Contains(line, "\x1b") && !strings.HasSuffix(line, "\x1b[0m") {
		return line + "\x1b[0m"
	}
	return line
}

// memoSlots bounds every memo to two revisions, so at most one stale capture
// stays reachable and memory stays flat.
const memoSlots = 2

type memoDims struct {
	width  int
	height int
}

type memoEntry struct {
	text  string
	aux   string
	out   string
	key   uint64
	dims  memoDims
	tag   uint8
	valid bool
}

type renderMemo struct {
	mu      sync.Mutex
	entries [memoSlots]memoEntry
	next    int
}

var (
	memoSeed = maphash.MakeSeed()
	// clipMemo caches the clipped preview text per content and size.
	clipMemo renderMemo
	// paneMemo caches the rendered preview pane, border included, so a frame
	// over unchanged content skips layout and lipgloss work altogether.
	paneMemo renderMemo
)

// memoKey mixes content, size, tag and any auxiliary key into one hash. Hits
// verify the stored text, aux and dims, so a collision costs a compare only.
func memoKey(text string, dims memoDims, tag uint8, aux string) uint64 {
	key := maphash.String(memoSeed, text)
	key ^= uint64(dims.width) << 48
	key ^= uint64(dims.height) << 32
	key ^= uint64(tag) << 24
	return key ^ maphash.String(memoSeed, aux)
}

func (m *renderMemo) get(text, aux string, key uint64, dims memoDims, tag uint8) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.entries {
		entry := &m.entries[i]
		if !entry.valid || entry.key != key || entry.dims != dims || entry.tag != tag {
			continue
		}
		if entry.text == text && entry.aux == aux {
			return entry.out, true
		}
	}
	return "", false
}

func (m *renderMemo) put(text, aux string, key uint64, dims memoDims, tag uint8, out string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[m.next] = memoEntry{text: text, aux: aux, out: out, key: key, dims: dims, tag: tag, valid: true}
	m.next = (m.next + 1) % memoSlots
}

func (m *renderMemo) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = [memoSlots]memoEntry{}
	m.next = 0
}

func resetRenderMemos() {
	clipMemo.reset()
	paneMemo.reset()
}

// clipLines clips captured pane text to width x rows. Results are memoised per
// content hash and size, so repeated frames over unchanged content do no work.
func clipLines(text string, width, rows int) string {
	if width < 1 || rows < 1 {
		return ""
	}
	dims := memoDims{width: width, height: rows}
	key := memoKey(text, dims, memoTagClip, "")
	if out, ok := clipMemo.get(text, "", key, dims, memoTagClip); ok {
		return out
	}
	out := clipLinesUncached(text, width, rows)
	clipMemo.put(text, "", key, dims, memoTagClip, out)
	return out
}

func clipLinesUncached(text string, width, rows int) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > rows {
		// Keep the tail (bottom) of the capture so the preview shows the
		// agent TUI's full interface — status bar, tool output, input —
		// from the bottom-most content upward.
		lines = lines[len(lines)-rows:]
	}

	for i, line := range lines {
		lines[i] = clipLine(line, width)
	}
	return strings.Join(lines, "\n")
}
