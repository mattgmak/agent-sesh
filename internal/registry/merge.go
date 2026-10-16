package registry

import (
	"strings"
	"time"
)

// MergeSessions combines session lists, keeping the newest row per pi session id
// and per tmux pane target.
func MergeSessions(lists ...[]Session) []Session {
	now := time.Now()

	flat := make([]Session, 0)
	for _, list := range lists {
		for _, session := range list {
			if strings.TrimSpace(session.TmuxTarget) == "" {
				continue
			}
			flat = append(flat, session)
		}
	}

	deduped, _ := dedupeBySessionID(flat, func() time.Time { return now })

	byTarget := make(map[string]Session, len(deduped))
	for _, session := range deduped {
		target := strings.TrimSpace(session.TmuxTarget)
		existing, ok := byTarget[target]
		if !ok || sessionUpdatedAfter(session, existing, now) {
			byTarget[target] = session
		}
	}

	out := make([]Session, 0, len(byTarget))
	for _, session := range byTarget {
		out = append(out, session)
	}
	return out
}

// dedupeBySessionID keeps the newest row per non-empty pi session id and returns
// the superseded rows. A session re-registered from another pane (or a reused
// pane) would otherwise leave its previous pane's row behind, so the picker
// renders the same session twice and one entry targets the wrong pane. Id-less
// rows cannot be attributed to a session, so they pass through untouched.
func dedupeBySessionID(sessions []Session, now func() time.Time) (kept, dropped []Session) {
	byID := make(map[string]int, len(sessions))
	kept = make([]Session, 0, len(sessions))
	for _, session := range sessions {
		id := strings.TrimSpace(session.ID)
		if id == "" {
			kept = append(kept, session)
			continue
		}
		idx, seen := byID[id]
		if !seen {
			byID[id] = len(kept)
			kept = append(kept, session)
			continue
		}
		if sessionUpdatedAfter(kept[idx], session, now()) {
			dropped = append(dropped, session)
			continue
		}
		dropped = append(dropped, kept[idx])
		kept[idx] = session
	}
	return kept, dropped
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
