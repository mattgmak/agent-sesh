package picker

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestWidthDisagreement compares the two width functions the picker uses:
// ansi.StringWidth, which drives ansi.Truncate in clipLine, and lipgloss.Width,
// which drives every block layout. It also reports the width of each
// non-ASCII rune in the file so a single mis-measured glyph shows up.
//
//	AGENT_SESH_CAPDUMP=/tmp/cap.txt go test -run WidthDisagreement ./internal/picker -v
func TestWidthDisagreement(t *testing.T) {
	path := os.Getenv("AGENT_SESH_CAPDUMP")
	if path == "" {
		t.Skip("AGENT_SESH_CAPDUMP not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	bad := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		a := ansi.StringWidth(line)
		l := lipgloss.Width(line)
		if a == l {
			continue
		}
		bad++
		t.Logf("line %2d ansi=%d lipgloss=%d", i, a, l)
	}
	if bad == 0 {
		t.Log("no line shows an ansi vs lipgloss width disagreement")
	}
	seen := map[rune]struct{}{}
	var out []string
	for _, line := range lines {
		for _, r := range ansi.Strip(line) {
			if r < 0x80 || r == utf8.RuneError {
				continue
			}
			if _, ok := seen[r]; ok {
				continue
			}
			seen[r] = struct{}{}
			out = append(out, string(r))
		}
	}
	for _, s := range out {
		r := []rune(s)[0]
		t.Logf("rune %U bytes=%d ansi=%d lipgloss=%d", r, len(s), ansi.StringWidth(s), lipgloss.Width(s))
	}
}
