package picker

import (
	"strings"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

// mergeSessionsIncremental merges a full reload into the current list without
// replacing stable session ids or dropping the cursor target unexpectedly.
func mergeSessionsIncremental(current, loaded []registry.Session) []registry.Session {
	if len(loaded) == 0 {
		return nil
	}
	if len(current) == 0 {
		out := append([]registry.Session(nil), loaded...)
		registry.SortSessions(out)
		return out
	}

	loadedByTarget := make(map[string]registry.Session, len(loaded))
	for _, session := range loaded {
		target := strings.TrimSpace(session.TmuxTarget)
		if target != "" {
			loadedByTarget[target] = session
		}
	}

	out := make([]registry.Session, 0, len(loaded))
	seen := make(map[string]struct{}, len(loaded))

	for _, cur := range current {
		target := strings.TrimSpace(cur.TmuxTarget)
		if target == "" {
			continue
		}
		loadedSession, ok := loadedByTarget[target]
		if !ok {
			continue
		}
		out = append(out, mergeStableSession(cur, loadedSession))
		seen[target] = struct{}{}
	}

	for target, session := range loadedByTarget {
		if _, ok := seen[target]; ok {
			continue
		}
		out = append(out, session)
	}

	registry.SortSessions(out)
	return out
}

func mergeStableSession(current, loaded registry.Session) registry.Session {
	merged := loaded
	switch {
	case isDiscovered(current) && !isDiscovered(loaded):
		merged.ID = loaded.ID
	case isDiscovered(current):
		merged.ID = current.ID
	case strings.TrimSpace(current.ID) != "":
		merged.ID = current.ID
	}
	if merged.TmuxSession == "" {
		merged.TmuxSession = current.TmuxSession
	}
	if merged.TmuxWindow == "" {
		merged.TmuxWindow = current.TmuxWindow
	}
	if merged.TmuxPane == "" {
		merged.TmuxPane = current.TmuxPane
	}
	if merged.CWD == "" {
		merged.CWD = current.CWD
	}
	return merged
}
