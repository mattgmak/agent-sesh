package registry

import (
	"sort"
	"time"
)

// StatusPriority ranks sessions for picker ordering. Lower values need attention
// sooner and sort before higher values.
func StatusPriority(status Status) int {
	switch status {
	case StatusHalted:
		return 0
	case StatusAwaitingInput:
		return 1
	case StatusUnknown:
		return 2
	case StatusIdle:
		return 3
	case StatusWorking:
		return 4
	case StatusToolCall:
		return 5
	default:
		return 3
	}
}

// SortSessions orders sessions by attention-needed status first, then MRU within
// the same status (newest last_prompt_at first).
func SortSessions(sessions []Session) {
	sort.SliceStable(sessions, func(i, j int) bool {
		pi := StatusPriority(sessions[i].Status)
		pj := StatusPriority(sessions[j].Status)
		if pi != pj {
			return pi < pj
		}
		return sessionPromptedAfter(sessions[i], sessions[j], time.Time{})
	})
}

func sessionPromptedAfter(a, b Session, now time.Time) bool {
	at, aOK := parseUpdatedAt(a.LastPromptAt, time.Time{})
	bt, bOK := parseUpdatedAt(b.LastPromptAt, time.Time{})
	switch {
	case aOK && bOK:
		return at.After(bt)
	case aOK:
		return true
	case bOK:
		return false
	default:
		return sessionUpdatedAfter(a, b, now)
	}
}
