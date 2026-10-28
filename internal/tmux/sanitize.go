package tmux

import (
	"strings"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

// RegistrySanitizeOptions returns sanitize opts that drop missing panes and panes
// without a live pi agent. Pass a snapshot when one is already available.
func RegistrySanitizeOptions(snap *Snapshot) registry.SanitizeOptions {
	if snap == nil {
		// Warm one snapshot so every row is answered from it. Otherwise each
		// dead row pays three tmux/ps execs in the live-probe fallbacks.
		if fresh, err := GetSnapshot(true); err == nil {
			snap = fresh
		}
	}
	return registry.SanitizeOptions{
		PaneExists: paneExistsChecker(snap),
		HasAgent:   hasAgentChecker(snap),
	}
}

func paneExistsChecker(snap *Snapshot) func(string) bool {
	return func(target string) bool {
		target = strings.TrimSpace(target)
		if target == "" {
			return false
		}
		if snap != nil {
			if snap.HasPane(target) {
				return true
			}
			// A fresh snapshot is authoritative: a missing pane is dead, so no
			// per-row tmux exec is needed to confirm it.
			if snap.authoritative() {
				return false
			}
		}
		return PaneExists(target)
	}
}

func hasAgentChecker(snap *Snapshot) func(string, string) bool {
	return func(target, agent string) bool {
		switch strings.TrimSpace(agent) {
		case "", "pi":
			target = strings.TrimSpace(target)
			if target == "" {
				return false
			}
			if snap != nil {
				info, ok := snap.PaneInfo(target)
				// Subagent surfaces run pi too; drop them from registry/counts.
				return ok && info.HasPiAgent && !info.IsSubagent
			}
			return PaneHasPiAgent(target) && !PaneIsSubagent(target)
		default:
			return false
		}
	}
}
