package tmux

import (
	"strings"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

// RegistrySanitizeOptions returns sanitize opts that drop missing panes and panes
// without a live pi agent. Pass a snapshot when one is already available.
func RegistrySanitizeOptions(snap *Snapshot) registry.SanitizeOptions {
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
		if snap != nil && snap.HasPane(target) {
			return true
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
				return ok && info.HasPiAgent
			}
			return PaneHasPiAgent(target)
		default:
			return false
		}
	}
}
