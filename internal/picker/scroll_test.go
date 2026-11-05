package picker

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestRailGeometry(t *testing.T) {
	tests := []struct {
		name    string
		total   int
		visible int
		top     int
		thumb   int
		start   int
	}{
		{name: "no window", total: 10, visible: 0, top: 0, thumb: 0, start: 0},
		{name: "everything fits", total: 4, visible: 10, top: 0, thumb: 10, start: 0},
		{name: "long list at top", total: 100, visible: 10, top: 0, thumb: 1, start: 0},
		{name: "long list middle", total: 100, visible: 10, top: 45, thumb: 1, start: 4},
		{name: "long list bottom", total: 100, visible: 10, top: 90, thumb: 1, start: 9},
		{name: "half scrolled", total: 20, visible: 10, top: 5, thumb: 5, start: 2},
		{name: "one row window", total: 30, visible: 1, top: 5, thumb: 1, start: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thumb, start := railGeometry(tt.total, tt.visible, tt.top)
			if thumb != tt.thumb || start != tt.start {
				t.Fatalf("got thumb=%d start=%d, want thumb=%d start=%d", thumb, start, tt.thumb, tt.start)
			}
		})
	}
}

func TestScrollRailCells(t *testing.T) {
	rail := scrollRail(100, 5, 2)
	if len(rail) != 5 {
		t.Fatalf("rail height = %d, want 5", len(rail))
	}
	for i, cell := range rail {
		if w := lipgloss.Width(cell); w != 1 {
			t.Fatalf("cell %d width = %d, want 1 (%q)", i, w, cell)
		}
	}
	if got := strings.Count(strings.Join(rail, ""), railThumbGlyph); got != 1 {
		t.Fatalf("thumb rows = %d, want 1", got)
	}
	if got := strings.Count(strings.Join(rail, ""), railTrackGlyph); got != 4 {
		t.Fatalf("track rows = %d, want 4", got)
	}

	fits := strings.Join(scrollRail(3, 5, 0), "")
	if strings.Contains(fits, railTrackGlyph) {
		t.Fatalf("a list that fits must not show track: %q", fits)
	}
	if got := strings.Count(fits, railThumbGlyph); got != 5 {
		t.Fatalf("thumb rows = %d, want 5", got)
	}
	if scrollRail(10, 0, 0) != nil {
		t.Fatal("a zero-height window must have no rail")
	}
}

func TestRenderListFrameRailKeepsWidth(t *testing.T) {
	items := []registry.Session{
		{ID: "aaaa1111", TmuxTarget: "main:1.1", Agent: "pi", Branch: "main", Status: registry.StatusIdle},
		{ID: "bbbb2222", TmuxTarget: "main:1.2", Agent: "pi", Branch: "work", Status: registry.StatusWorking},
	}
	const width = 60
	out := renderListFrame(items, 0, 8, width, listRenderOpts{showCursor: true, scrollRail: true}, formatSessionEntry)
	lines := strings.Split(out, "\n")
	if len(lines) != 8 {
		t.Fatalf("frame height = %d, want 8\n%s", len(lines), out)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Fatalf("line %d width = %d, want %d\n%q", i, got, width, line)
		}
	}
	if !strings.Contains(out, railThumbGlyph) {
		t.Fatal("frame has no thumb")
	}
}

func TestRenderListFrameWithoutRail(t *testing.T) {
	items := []registry.Session{
		{ID: "aaaa1111", TmuxTarget: "main:1.1", Agent: "pi", Branch: "main", Status: registry.StatusIdle},
	}
	out := renderListFrame(items, 0, 4, 60, listRenderOpts{showCursor: true}, formatSessionEntry)
	if strings.Contains(out, railThumbGlyph) || strings.Contains(out, railTrackGlyph) {
		t.Fatalf("rail appeared without opts.scrollRail:\n%s", out)
	}
}
