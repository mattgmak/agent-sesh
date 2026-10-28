package picker

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

const (
	benchTermWidth  = 200
	benchTermHeight = 50
	benchSessCount  = 40
)

// benchSessionSet builds a session list shaped like a busy real registry: mixed
// statuses, optional metadata, long prompts.
func benchSessionSet(n int) []registry.Session {
	statuses := []registry.Status{
		registry.StatusWorking,
		registry.StatusToolCall,
		registry.StatusIdle,
		registry.StatusAwaitingInput,
	}
	sessions := make([]registry.Session, n)
	for i := range sessions {
		sessions[i] = registry.Session{
			ID:            fmt.Sprintf("sess-%02d", i),
			TmuxTarget:    fmt.Sprintf("%%%d", i+1),
			TmuxSession:   fmt.Sprintf("work-%d", i%6),
			TmuxWindow:    "1",
			TmuxPane:      fmt.Sprintf("%d", i%3),
			CWD:           fmt.Sprintf("/Users/you/NixConfig/sub/project-%d", i%7),
			Branch:        []string{"main", "feat/picker-perf", "fix/layout"}[i%3],
			Title:         fmt.Sprintf("agent task %02d: reduce picker render cost", i),
			LastPrompt:    "investigate the navigation latency and make the list frame cheaper to render on every key press",
			Status:        statuses[i%len(statuses)],
			Agent:         "pi",
			Model:         []string{"composer-2.5", "gpt-5.2-codex", "cleanup"}[i%3],
			PiSessionName: fmt.Sprintf("pi-%02d", i),
		}
		if i%4 == 1 {
			sessions[i].ToolName = "Shell: go test ./..."
		}
	}
	registry.SortSessions(sessions)
	return sessions
}

func benchPreviewText(lines int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "pane line %03d: ok  github.com/mattgmak/agent-sesh/internal/picker\t0.0%ds\n", i, i%9)
	}
	return b.String()
}

// benchPickerModel mirrors a live picker with a loaded preview at a wide
// terminal size.
func benchPickerModel() model {
	sessions := benchSessionSet(benchSessCount)
	filter := textinput.New()
	filter.Prompt = "> "
	m := model{
		sessions:       sessions,
		filter:         filter,
		width:          benchTermWidth,
		height:         benchTermHeight,
		previewTarget:  sessions[0].TmuxTarget,
		previewContent: benchPreviewText(60),
	}
	m.previewKey = previewContentKey(m.previewContent)
	m.syncInputWidth()
	return m
}

func BenchmarkPickerView200x50(b *testing.B) {
	m := benchPickerModel()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkPickerNavKey200x50(b *testing.B) {
	m := benchPickerModel()
	msg := tea.KeyPressMsg{Code: tea.KeyTab}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next, _ := m.Update(msg)
		m = next.(model)
	}
}
