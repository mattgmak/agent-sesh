package picker

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func viewContent(m model) string {
	return m.View().Content
}

func testModel(sessions []registry.Session) model {
	filter := textinput.New()
	filter.Prompt = "> "
	return model{sessions: sessions, filter: filter, height: 24, width: 80}
}

func sampleSessions() []registry.Session {
	return []registry.Session{
		{
			ID:          "1",
			TmuxTarget:  "%1",
			TmuxSession: "nixconfig",
			TmuxWindow:  "1",
			TmuxPane:    "1",
			CWD:         "/Users/you/NixConfig/agent-sesh",
			Branch:      "main",
			Title:       "agent-sesh specs",
			LastPrompt:  "implement agent picker",
			Status:      registry.StatusWorking,
			Agent:       "pi",
		},
		{
			ID:          "2",
			TmuxTarget:  "%2",
			TmuxSession: "other",
			CWD:         "/Users/you/other-project",
			Branch:      "dev",
			Title:       "other work",
			LastPrompt:  "run go tests",
			Status:      registry.StatusToolCall,
			ToolName:    "Shell: go test",
			Agent:       "pi",
		},
		{
			ID:          "3",
			TmuxTarget:  "%3",
			TmuxSession: "scratch",
			CWD:         "/tmp",
			Title:       "idle pane",
			LastPrompt:  "idle pane",
			Status:      registry.StatusIdle,
			Agent:       "pi",
		},
	}
}

func TestFilteredSessionsNoFilter(t *testing.T) {
	m := testModel(sampleSessions())
	if got := len(m.filteredSessions()); got != 3 {
		t.Fatalf("len(filteredSessions()) = %d, want 3", got)
	}
}

func TestFilteredSessionsByTitle(t *testing.T) {
	m := testModel(sampleSessions())
	m.filter.SetValue("other")
	got := m.filteredSessions()
	if len(got) != 1 || got[0].Title != "other work" {
		t.Fatalf("filtered by title: got %+v", got)
	}
}

func TestFilteredSessionsByStatus(t *testing.T) {
	m := testModel(sampleSessions())
	m.filter.SetValue("tool_call")
	got := m.filteredSessions()
	if len(got) != 1 || got[0].Status != registry.StatusToolCall {
		t.Fatalf("filtered by status: got %+v", got)
	}
}

func TestFilteredSessionsByToolName(t *testing.T) {
	m := testModel(sampleSessions())
	m.filter.SetValue("go test")
	got := m.filteredSessions()
	if len(got) != 1 || got[0].ToolName != "Shell: go test" {
		t.Fatalf("filtered by tool name: got %+v", got)
	}
}

func TestSelected(t *testing.T) {
	m := testModel(sampleSessions())
	m.cursor = 1

	session, ok := m.selected()
	if !ok || session.Title != "other work" {
		t.Fatalf("selected() = (%+v, %v), want other work", session, ok)
	}

	m.cursor = 99
	if _, ok := m.selected(); ok {
		t.Fatal("expected no selection past end of list")
	}
}

func TestSelectedEmpty(t *testing.T) {
	m := testModel(nil)
	if _, ok := m.selected(); ok {
		t.Fatal("expected no selection for empty sessions")
	}
}

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status registry.Status
		want   string
	}{
		{registry.StatusWorking, iconWorking},
		{registry.StatusHalted, iconHalted},
		{registry.StatusAwaitingInput, iconAwaitingInput},
		{registry.StatusToolCall, iconStatusToolCall},
		{registry.StatusIdle, iconIdle},
		{registry.Status("unknown"), iconIdle},
	}

	for _, tc := range tests {
		if got := statusIcon(tc.status); got != tc.want {
			t.Errorf("statusIcon(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestStatusIconsMatchNerdFontMDI(t *testing.T) {
	// Codepoints from nerd-fonts bin/scripts/lib/i_md.sh (MDI) / i_fa.sh (FA).
	want := map[string]rune{
		"idle":           '\U000F04B2', // nf-md (user choice)
		"working":        '\U000F09D1', // nf-md-brain
		"halted":         '\U000F0377', // nf-md (user choice)
		"awaiting_input": '\uF128',     // nf-fa-question
		"tool_call":      '\U000F0996', // nf-md-progress_clock
	}
	got := map[string]rune{
		"idle":           []rune(iconIdle)[0],
		"working":        []rune(iconWorking)[0],
		"halted":         []rune(iconHalted)[0],
		"awaiting_input": []rune(iconAwaitingInput)[0],
		"tool_call":      []rune(iconStatusToolCall)[0],
	}
	for name, expected := range want {
		if got[name] != expected {
			t.Errorf("%s icon = U+%04X, want U+%04X", name, got[name], expected)
		}
	}
}

func TestReconcileCursorKeepsSelectionByID(t *testing.T) {
	m := testModel(sampleSessions())
	m.cursor = 1
	m.selectedID = "2"

	m.sessions = []registry.Session{
		m.sessions[2],
		m.sessions[0],
		m.sessions[1],
	}

	m.reconcileCursor()
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (session id 2)", m.cursor)
	}
	if m.selectedID != "2" {
		t.Fatalf("selectedID = %q, want %q", m.selectedID, "2")
	}
}

func TestShortCWD(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/Users/you/proj", "/Users/you/proj"},
		{"/a/b/c/d/e", "…/c/d/e"},
		{"", ""},
		{"/one", "/one"},
	}

	for _, tc := range tests {
		if got := shortCWD(tc.in); got != tc.want {
			t.Errorf("shortCWD(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReloadStaysOpenWhenSessionsEmpty(t *testing.T) {
	t.Setenv("AGENT_SESH_DISABLE_DISCOVER", "1")
	m := testModel(sampleSessions())
	m.registry = t.TempDir() + "/missing.json"
	m.sessions = nil
	m = m.reload()
	if m.quitting {
		t.Fatal("expected reload with no sessions to keep picker open, not quit")
	}
}

func TestViewShowsSessionsInSplitMode(t *testing.T) {
	m := testModel(sampleSessions())
	m.width = 120
	m.height = 24
	m.syncInputWidth()

	out := viewContent(m)
	if !strings.Contains(out, "nixconfig") {
		t.Fatalf("expected session name in split view, got:\n%s", out)
	}
}

func TestPreviewLoadingOnlyBeforeFirstFetch(t *testing.T) {
	m := testModel(sampleSessions())
	m.width = 120
	m.height = 24
	m.syncInputWidth()

	out := viewContent(m)
	if !strings.Contains(out, "Loading preview") {
		t.Fatalf("expected loading before first preview, got:\n%s", out)
	}

	m.previewName = "1"
	m.previewContent = "\x1b[31mcolored\x1b[0m"
	m.previewPending = "2"

	out = viewContent(m)
	if strings.Contains(out, "Loading preview") {
		t.Fatalf("expected previous preview while pending fetch, got:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[31m") {
		t.Fatalf("expected colored preview content, got:\n%s", out)
	}
}

func TestBottomAlignedCursorMovesUpToNextSession(t *testing.T) {
	m := testModel(sampleSessions())
	registry.SortSessions(m.sessions)
	m.width = 80
	m.height = 24
	m.cursor = 0
	m.selectedID = m.sessions[0].ID
	m.reconcileCursor()

	before := viewContent(m)
	m = m.setCursor(m.cursor + 1)
	after := viewContent(m)

	session, ok := m.selected()
	if !ok || session.ID != m.sessions[1].ID {
		t.Fatalf("up should move to next sorted session, got %+v want id %q", session, m.sessions[1].ID)
	}
	if before == after {
		t.Fatal("expected cursor move to change rendered view")
	}
}

func TestCyclicCursorWrapsBothEnds(t *testing.T) {
	m := testModel(sampleSessions())
	registry.SortSessions(m.sessions)
	items := m.filteredSessions()
	n := len(items)
	if n < 2 {
		t.Fatal("need at least 2 sessions")
	}

	// Up past the last item wraps to the first.
	m.cursor = n - 1
	m.selectedID = items[n-1].ID
	m = m.setCursor(m.cursor + 1)
	if m.cursor != 0 || m.selectedID != items[0].ID {
		t.Fatalf("up from last: want cursor 0 / id %q, got %d / %q", items[0].ID, m.cursor, m.selectedID)
	}

	// Down past the first item wraps to the last.
	m = m.setCursor(m.cursor - 1)
	if m.cursor != n-1 || m.selectedID != items[n-1].ID {
		t.Fatalf("down from first: want cursor %d / id %q, got %d / %q", n-1, items[n-1].ID, m.cursor, m.selectedID)
	}
}

func TestFormatSessionLineMultiline(t *testing.T) {
	line := formatSessionLine(sampleSessions()[0], 120)
	if !strings.Contains(line, "\n") {
		t.Fatalf("expected multiline entry, got %q", line)
	}
	if !strings.Contains(line, iconSession) || !strings.Contains(line, "nixconfig") {
		t.Fatalf("expected tmux session on first line, got %q", line)
	}
	if !strings.Contains(line, iconPrompt) || !strings.Contains(line, "implement agent picker") {
		t.Fatalf("expected last prompt on second line, got %q", line)
	}
}

func TestFormatFzfInputNulSeparatedItems(t *testing.T) {
	sessions := sampleSessions()
	if len(sessions) < 2 {
		t.Fatal("need at least 2 sessions")
	}
	input := formatFzfInput(sessions)
	if !strings.Contains(input, "\x00") {
		t.Fatalf("expected NUL-separated items, got %q", input)
	}
	items := strings.Split(strings.TrimSuffix(input, "\x00"), "\x00")
	if len(items) != len(sessions) {
		t.Fatalf("expected %d items, got %d", len(sessions), len(items))
	}
	for i, item := range items {
		key := sessions[i].TmuxTarget
		if !strings.HasPrefix(item, key+"\t") {
			t.Fatalf("item %d: expected %q prefix, got %q", i, key+"\t", item)
		}
	}
}

func TestFormatFzfLineKeepsMultiline(t *testing.T) {
	s := sampleSessions()[0]
	line := formatFzfLine(s)
	if !strings.HasPrefix(line, s.TmuxTarget+"\t") {
		t.Fatalf("expected TmuxTarget\t prefix, got %q", line)
	}
	if !strings.Contains(line, "\n") {
		t.Fatalf("expected multiline entry intact (meta + prompt), got %q", line)
	}
	if !strings.Contains(line, "nixconfig") || !strings.Contains(line, "implement agent picker") {
		t.Fatalf("expected meta + prompt in item, got %q", line)
	}
}

func TestFormatFzfLineFallsBackToID(t *testing.T) {
	s := sampleSessions()[0]
	s.TmuxTarget = ""
	line := formatFzfLine(s)
	if !strings.HasPrefix(line, s.ID+"\t") {
		t.Fatalf("expected ID fallback prefix, got %q", line)
	}
}

func TestPreviewActiveRequiresWidth(t *testing.T) {
	if previewActive(10) {
		t.Fatal("preview should be inactive on narrow terminals")
	}
	if !previewActive(120) {
		t.Fatal("preview should be active on wide terminals")
	}
}

func TestFooterViewUsesFilterByDefault(t *testing.T) {
	m := testModel(sampleSessions())
	if !strings.Contains(m.footerView(), ">") {
		t.Fatalf("expected sesh-style prompt in footer, got %q", m.footerView())
	}
}

func TestFooterViewUsesRenameInRenameMode(t *testing.T) {
	m := testModel(sampleSessions())
	m.mode = modeRename
	m.rename = textinput.New()
	m.rename.Prompt = iconRename + " "
	if !strings.Contains(m.footerView(), iconRename) {
		t.Fatalf("expected rename prompt, got %q", m.footerView())
	}
}

func TestViewPutsFilterBelowSessions(t *testing.T) {
	m := testModel(sampleSessions())
	m.width = 120
	m.height = 24
	m.syncInputWidth()

	lines := strings.Split(strings.TrimSuffix(viewContent(m), "\n"), "\n")
	filterLine := -1
	sessionLine := -1
	for i, line := range lines {
		if strings.Contains(line, "nixconfig") && sessionLine < 0 {
			sessionLine = i
		}
		if strings.HasPrefix(strings.TrimSpace(line), ">") || strings.Contains(line, "> ") {
			filterLine = i
		}
	}
	if sessionLine < 0 {
		t.Fatal("expected session row in view")
	}
	if filterLine < 0 {
		t.Fatal("expected filter row in view")
	}
	if filterLine <= sessionLine {
		t.Fatalf("expected filter below sessions, session@%d filter@%d", sessionLine, filterLine)
	}
}
