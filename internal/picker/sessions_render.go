package picker

import (
	"strings"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

// sessionsRenderKey fingerprints session fields that affect picker rendering.
// Registry writes that only touch bookkeeping (updated_at, model, etc.) are ignored.
func sessionsRenderKey(sessions []registry.Session) string {
	var b strings.Builder
	for _, session := range sessions {
		writeSessionRenderFields(&b, session)
		b.WriteByte('\x1e')
	}
	return b.String()
}

func writeSessionRenderFields(b *strings.Builder, session registry.Session) {
	b.WriteString(session.ID)
	b.WriteByte('\x00')
	b.WriteString(session.TmuxTarget)
	b.WriteByte('\x00')
	b.WriteString(string(session.Status))
	b.WriteByte('\x00')
	b.WriteString(session.ToolName)
	b.WriteByte('\x00')
	b.WriteString(session.Title)
	b.WriteByte('\x00')
	b.WriteString(session.LastPrompt)
	b.WriteByte('\x00')
	b.WriteString(session.Branch)
	b.WriteByte('\x00')
	b.WriteString(session.Agent)
	b.WriteByte('\x00')
	b.WriteString(session.CWD)
	b.WriteByte('\x00')
	b.WriteString(session.TmuxSession)
	b.WriteByte('\x00')
	b.WriteString(session.TmuxWindow)
	b.WriteByte('\x00')
	b.WriteString(session.TmuxPane)
}

func (m *model) syncSessionsRenderKey() {
	m.sessionsRenderKey = sessionsRenderKey(m.sessions)
}

// applySessionsIfChanged swaps the session list only when rendered output would change.
func (m *model) applySessionsIfChanged(next []registry.Session) bool {
	key := sessionsRenderKey(next)
	if key == m.sessionsRenderKey {
		return false
	}
	m.sessionsRenderKey = key
	m.sessions = next
	return true
}
