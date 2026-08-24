package registry

import (
	"strings"
	"time"
)

// MergeSessions combines session lists, keeping the newest row per tmux pane target.
func MergeSessions(lists ...[]Session) []Session {
	byTarget := make(map[string]Session)
	now := time.Now()
	for _, list := range lists {
		for _, session := range list {
			target := strings.TrimSpace(session.TmuxTarget)
			if target == "" {
				continue
			}
			existing, ok := byTarget[target]
			if !ok || sessionUpdatedAfter(session, existing, now) {
				byTarget[target] = session
			}
		}
	}
	out := make([]Session, 0, len(byTarget))
	for _, session := range byTarget {
		out = append(out, session)
	}
	return out
}

// filterPersistable drops picker-only discovered rows before writing the registry.
func filterPersistable(sessions []Session) []Session {
	out := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		if strings.HasPrefix(session.ID, "discovered:") {
			continue
		}
		out = append(out, session)
	}
	return out
}
