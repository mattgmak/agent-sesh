package picker

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattgmak/agent-sesh/internal/registry"
	"github.com/mattgmak/agent-sesh/internal/tmux"
)

const discoveredPrefix = "discovered:"

func isDiscovered(session registry.Session) bool {
	return strings.HasPrefix(session.ID, discoveredPrefix)
}

func mergeDiscoveredSessions(registered []registry.Session, snap *tmux.Snapshot) []registry.Session {
	if os.Getenv("AGENT_SESH_DISABLE_DISCOVER") == "1" || snap == nil {
		return registered
	}
	known := make(map[string]struct{}, len(registered))
	for _, session := range registered {
		target := strings.TrimSpace(session.TmuxTarget)
		if target != "" {
			known[target] = struct{}{}
		}
	}

	out := append([]registry.Session(nil), registered...)
	for _, info := range snap.PiPanes() {
		target := strings.TrimSpace(info.Target)
		if target == "" {
			continue
		}
		if _, ok := known[target]; ok {
			continue
		}
		title := "pi session"
		if info.CurrentPath != "" {
			title = info.CurrentPath
		}
		out = append(out, registry.Session{
			ID:          discoveredPrefix + strings.TrimPrefix(target, "%"),
			TmuxTarget:  target,
			TmuxSession: info.SessionName,
			TmuxWindow:  info.WindowIndex,
			TmuxPane:    info.PaneIndex,
			CWD:         info.CurrentPath,
			Title:       title,
			Agent:       "pi",
			Status:      registry.StatusUnknown,
		})
	}

	return out
}

func sessionPaneLabel(session registry.Session) string {
	if window, pane, ok := sessionPaneCoords(session); ok {
		return fmt.Sprintf("%s.%s", window, pane)
	}
	return ""
}

func sessionPaneCoords(session registry.Session) (window, pane string, ok bool) {
	window = strings.TrimSpace(session.TmuxWindow)
	pane = strings.TrimSpace(session.TmuxPane)
	if window != "" && pane != "" {
		return window, pane, true
	}
	if snap, err := tmux.GetSnapshot(false); err == nil {
		if info, hit := snap.PaneInfo(session.TmuxTarget); hit {
			if window == "" {
				window = info.WindowIndex
			}
			if pane == "" {
				pane = info.PaneIndex
			}
			if window != "" && pane != "" {
				return window, pane, true
			}
		}
	}
	return "", "", false
}
