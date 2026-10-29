package registry

import (
	"sort"
	"time"
)

// StatusPriority ranks sessions for picker ordering. The renderer draws index 0
// at the bottom, so a low rank sits low on screen. The active group holds the
// highest ranks and stays on top.
func StatusPriority(status Status) int {
	switch status {
	case StatusAwaitingInput:
		return 0
	case StatusHalted:
		return 1
	case StatusUnknown:
		return 2
	case StatusIdle:
		return 3
	case StatusBgJobsRunning:
		return 4
	case StatusSubagentsRunning:
		return 5
	case StatusToolCall:
		return 6
	case StatusWorking:
		return 7
	default:
		return 3
	}
}

// SortSessions orders sessions by priority rank, then MRU within the same rank
// (newest last_prompt_at first). It ranks the resolved status, so a halted row
// with a live counter joins the active group.
func SortSessions(sessions []Session) {
	sort.SliceStable(sessions, func(i, j int) bool {
		pi := StatusPriority(sessions[i].DisplayStatus())
		pj := StatusPriority(sessions[j].DisplayStatus())
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
