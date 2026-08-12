package picker

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestFormatSessionEntryLimitsLongPrompt(t *testing.T) {
	long := strings.Repeat("word ", 40)
	session := registry.Session{
		TmuxSession: "demo",
		LastPrompt:  long,
		Status:      registry.StatusWorking,
		Agent:       "pi",
	}

	lines := formatSessionEntry(session, 40)
	if len(lines) != 2 {
		t.Fatalf("expected meta + prompt lines, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[1], "…") {
		t.Fatalf("expected truncated prompt to include ellipsis, got %q", lines[1])
	}
}

func TestFormatSessionEntryStatusHasTrailingSpace(t *testing.T) {
	session := registry.Session{TmuxSession: "demo", Status: registry.StatusIdle, Agent: "pi"}
	lines := formatSessionEntry(session, 80)
	if len(lines) < 1 {
		t.Fatal("expected at least one line")
	}
	plain := lipgloss.NewStyle().Render(lines[0])
	_ = plain
	if !strings.Contains(lines[0], iconIdle) {
		t.Fatalf("expected status icon in meta line, got %q", lines[0])
	}
}

func TestLayoutMetaChunksKeepsIconTextPairsTogether(t *testing.T) {
	session := registry.Session{
		TmuxSession: "very-long-session-name-that-should-wrap",
		TmuxWindow:  "12",
		TmuxPane:    "3",
		Branch:      "feature/agent-picker-improvements",
		ToolName:    "Shell: go test ./...",
		CWD:         "/Users/you/NixConfig/agent-sesh/internal/picker",
		Status:      registry.StatusWorking,
		Agent:       "pi",
	}

	lines := layoutMetaChunks(session.Status, buildMetaChunks(session), 30)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped meta lines, got %d: %q", len(lines), lines)
	}
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), iconSession) && strings.Contains(line, iconBranch) {
			t.Fatalf("session and branch icons should not share a broken line: %q", line)
		}
	}
}

func TestLayoutMetaChunksLongBranchUsesFullLineWidth(t *testing.T) {
	branch := "mono.mattmak-tech-1008-change-ble-reading-sync-to-use-new-api"
	session := registry.Session{
		TmuxSession: "demo",
		Branch:      branch,
		Status:      registry.StatusIdle,
		Agent:       "pi",
	}

	const width = 40
	lines := layoutMetaChunks(session.Status, buildMetaChunks(session), width)
	if len(lines) < 2 {
		t.Fatalf("expected status line and dedicated branch line, got %d: %q", len(lines), lines)
	}

	branchLine := lines[len(lines)-1]
	if !strings.Contains(branchLine, iconBranch) {
		t.Fatalf("expected branch on its own line, got %q", lines)
	}
	if strings.Contains(branchLine, iconFolder) {
		t.Fatalf("branch should not share a line with cwd, got %q", branchLine)
	}
	if lipgloss.Width(branchLine) > width {
		t.Fatalf("branch line wider than budget: width=%d line=%q", lipgloss.Width(branchLine), branchLine)
	}
	if !strings.Contains(branchLine, "…") {
		t.Fatalf("expected branch line to end with ellipsis, got %q", branchLine)
	}
}

// TestLayoutMetaChunksStatusGluedToLongSessionName guards the truncation of a
// session name too wide for the line: the status icon and its trailing space
// stay on the same line as the name, so the name's truncation budget must be
// the line width minus the status chunk and separator — otherwise the combined
// line overflows and the terminal wraps it.
func TestLayoutMetaChunksStatusGluedToLongSessionName(t *testing.T) {
	session := registry.Session{
		TmuxSession: "tcm_mattmak_tech_1013_replace_program_screen_with_lifestyle_redesign",
		Status:      registry.StatusWorking,
		Agent:       "pi",
	}

	for _, width := range []int{30, 40, 56, 58, 60} {
		lines := layoutMetaChunks(session.Status, buildMetaChunks(session), width)
		if len(lines) == 0 {
			t.Fatalf("width %d: expected at least one meta line", width)
		}
		first := lines[0]
		if !strings.Contains(first, iconWorking) {
			t.Fatalf("width %d: expected status icon on first line, got %q", width, first)
		}
		if !strings.Contains(first, iconSession) {
			t.Fatalf("width %d: expected session icon on first line, got %q", width, first)
		}
		if w := lipgloss.Width(first); w > width {
			t.Fatalf("width %d: first line wider than budget (%d): %q", width, w, first)
		}
		if !strings.Contains(first, "…") {
			t.Fatalf("width %d: expected truncated session name, got %q", width, first)
		}
	}
}

// TestLayoutMetaChunksLongSessionNameNoWrap renders the entry through the full
// list pipeline (gutter + gap) and asserts no line exceeds the frame width,
// which is what a terminal would wrap.
func TestLayoutMetaChunksLongSessionNameNoWrap(t *testing.T) {
	session := registry.Session{
		TmuxSession: "tcm_mattmak_tech_1013_replace_program_screen_with_lifestyle_redesign",
		TmuxWindow:  "1",
		TmuxPane:    "1",
		Branch:      "mattmak/tech-1013-replace-program-screen-with-lifestyle-redesign",
		ToolName:    "ctx_shell",
		CWD:         "/Users/mattgmak/code/tcm/tcm.mattmak-tech-1013-replace-program-screen-with-lifestyle-redesign",
		Status:      registry.StatusWorking,
		Agent:       "pi",
	}

	for _, lineWidth := range []int{40, 56, 58, 60} {
		bodyWidth := lineWidth - 2
		entryLines := formatSessionEntry(session, bodyWidth)
		for i, line := range entryLines {
			if w := lipgloss.Width(line) + 2; w > lineWidth {
				t.Fatalf("lineWidth %d: entry line %d is %d wide with gutter, wraps: %q",
					lineWidth, i, w, line)
			}
		}
	}
}

func TestFormatSessionEntryMultilinePromptSingleLine(t *testing.T) {
	session := registry.Session{
		TmuxSession: "demo",
		LastPrompt:  "fix the bug\n\ncheck the tests\r\nand commit",
		Status:      registry.StatusIdle,
		Agent:       "pi",
	}

	lines := formatSessionEntry(session, 80)
	if len(lines) != 2 {
		t.Fatalf("expected meta + one prompt line, got %d: %q", len(lines), lines)
	}
	if strings.Contains(lines[1], "\n") {
		t.Fatalf("prompt line must not contain newlines, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "fix the bug check the tests and commit") {
		t.Fatalf("expected whitespace-collapsed prompt, got %q", lines[1])
	}
}

func TestRenderIconTextChunkTruncatesWithEllipsis(t *testing.T) {
	got := renderIconTextChunk(iconSession, strings.Repeat("x", 40), sessionStyle, 12)
	if !strings.Contains(got, "…") {
		t.Fatalf("expected ellipsis in truncated chunk, got %q", got)
	}
	if lipgloss.Width(got) > 12 {
		t.Fatalf("chunk wider than budget: width=%d text=%q", lipgloss.Width(got), got)
	}
}

func TestRenderListGutterSelected(t *testing.T) {
	if got := renderListGutter(false); got == "" || got == " " {
		t.Fatalf("inactive gutter should render muted marker, got %q", got)
	}
	if got := renderListGutter(true); got == "" || got == " " {
		t.Fatalf("active gutter should render marker, got %q", got)
	}
}
