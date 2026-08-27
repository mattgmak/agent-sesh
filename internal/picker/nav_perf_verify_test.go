package picker

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func benchSessions(n int, status registry.Status) []registry.Session {
	sessions := make([]registry.Session, n)
	for i := range sessions {
		sessions[i] = registry.Session{
			ID:          fmt.Sprintf("s%d", i),
			TmuxTarget:  fmt.Sprintf("%%%d", i+1),
			TmuxSession: fmt.Sprintf("sess-%d", i%5),
			TmuxWindow:  "1",
			TmuxPane:    "1",
			CWD:         "/Users/you/project",
			Branch:      "main",
			Title:       fmt.Sprintf("agent task %d with a moderately long title", i),
			LastPrompt:  "implement something useful for the picker performance investigation",
			Status:      status,
			ToolName:    "Shell: go test ./...",
			Agent:       "pi",
			Model:       "composer-2.5",
		}
	}
	registry.SortSessions(sessions)
	return sessions
}

func benchModel(n int, status registry.Status) model {
	m := testModel(benchSessions(n, status))
	m.width = 120
	m.height = 24
	m.syncInputWidth()
	m.previewTarget = "%1"
	m.previewContent = stringsRepeatLine(40)
	m.previewRevision = previewRevision(m.sessions[0])
	return m
}

func stringsRepeatLine(n int) string {
	out := make([]byte, 0, n*12)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("line %d output\n", i)...)
	}
	return string(out)
}

func BenchmarkRenderListFrame5(b *testing.B) {
	benchmarkRenderListFrame(b, 5)
}

func BenchmarkRenderListFrame30(b *testing.B) {
	benchmarkRenderListFrame(b, 30)
}

func BenchmarkRenderListFrame50(b *testing.B) {
	benchmarkRenderListFrame(b, 50)
}

func benchmarkRenderListFrame(b *testing.B, n int) {
	items := benchSessions(n, registry.StatusIdle)
	visible := 20
	width := 58
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderListFrame(items, i%n, visible, width, listRenderOpts{showCursor: true}, formatSessionEntry)
	}
}

func BenchmarkViewAfterCursorMove5(b *testing.B) {
	benchmarkViewAfterCursorMove(b, 5, registry.StatusIdle)
}

func BenchmarkViewAfterCursorMove30(b *testing.B) {
	benchmarkViewAfterCursorMove(b, 30, registry.StatusIdle)
}

func BenchmarkViewAfterCursorMove50(b *testing.B) {
	benchmarkViewAfterCursorMove(b, 50, registry.StatusIdle)
}

func BenchmarkViewAfterCursorMove30Working(b *testing.B) {
	benchmarkViewAfterCursorMove(b, 30, registry.StatusWorking)
}

func benchmarkViewAfterCursorMove(b *testing.B, n int, status registry.Status) {
	m := benchModel(n, status)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.cursor = i % n
		_ = viewContent(m)
	}
}

func BenchmarkUpdateDown30(b *testing.B) {
	m := benchModel(30, registry.StatusWorking)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = next.(model)
	}
}

func BenchmarkViewAfterCursorMove30Narrow(b *testing.B) {
	m := benchModel(30, registry.StatusIdle)
	m.width = 60 // previewActive false → no split pane
	m.syncInputWidth()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.cursor = i % 30
		_ = viewContent(m)
	}
}

func BenchmarkViewAfterCursorMove30Wide(b *testing.B) {
	benchmarkViewAfterCursorMove(b, 30, registry.StatusIdle)
}
