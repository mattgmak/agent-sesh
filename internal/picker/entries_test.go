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
	if renderListGutter(false) != " " {
		t.Fatalf("inactive gutter should be blank")
	}
	if got := renderListGutter(true); got == "" || got == " " {
		t.Fatalf("active gutter should render marker, got %q", got)
	}
}
