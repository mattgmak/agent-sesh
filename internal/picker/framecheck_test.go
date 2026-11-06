package picker

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestFrameWidthHistogram reports the cell width of every line in a View() dump.
// A correct frame pads every line to one width, so the histogram has one entry.
// More than one entry means some lines overflow the pane and the terminal wraps
// them, which shifts every following row by one column.
//
//	AGENT_SESH_CAPDUMP=/tmp/myframe.txt go test -run FrameWidthHistogram ./internal/picker -v
func TestFrameWidthHistogram(t *testing.T) {
	path := os.Getenv("AGENT_SESH_CAPDUMP")
	if path == "" {
		t.Skip("AGENT_SESH_CAPDUMP not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	counts := map[int]int{}
	widest := map[int]string{}
	for i, line := range lines {
		w := ansi.StringWidth(line)
		counts[w]++
		if _, ok := widest[w]; !ok {
			widest[w] = fmt.Sprintf("line %d bytes=%d", i, len(line))
		}
	}
	var ws []int
	for w := range counts {
		ws = append(ws, w)
	}
	sort.Ints(ws)
	for _, w := range ws {
		t.Logf("width=%3d count=%3d first=%s", w, counts[w], widest[w])
	}
}
