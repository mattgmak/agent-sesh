package picker

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestDumpCaptureWidths reports every line of a capture file. It measures the
// line with ansi.StringWidth and, when the line is longer than the clip width,
// measures the clipped result, which must equal the width exactly.
//
//	AGENT_SESH_CAPDUMP=/tmp/cap.txt AGENT_SESH_CAPDUMP_W=151 go test -run DumpCaptureWidths ./internal/picker -v
func TestDumpCaptureWidths(t *testing.T) {
	path := os.Getenv("AGENT_SESH_CAPDUMP")
	if path == "" {
		t.Skip("AGENT_SESH_CAPDUMP not set")
	}
	width, err := strconv.Atoi(os.Getenv("AGENT_SESH_CAPDUMP_W"))
	if err != nil || width < 1 {
		t.Skip("AGENT_SESH_CAPDUMP_W must be a positive integer")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		raw := ansi.StringWidth(line)
		if raw <= width {
			t.Logf("line %2d bytes=%4d width=%3d", i, len(line), raw)
			continue
		}
		clipped := clipLine(line, width)
		got := ansi.StringWidth(clipped)
		flag := ""
		if got != width {
			flag = " MISMATCH"
		}
		t.Logf("line %2d bytes=%4d width=%3d clipped=%3d%s", i, len(line), raw, got, flag)
	}
}
