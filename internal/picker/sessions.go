package picker

import (
	"strings"

	"github.com/mattgmak/agent-sesh/internal/registry"
	"github.com/mattgmak/agent-sesh/internal/tmux"
)

func sanitizeOptsFromSnapshot(snap *tmux.Snapshot) registry.SanitizeOptions {
	return tmux.RegistrySanitizeOptions(snap)
}

func loadSessionsFast(path string) ([]registry.Session, error) {
	defer profileStart("loadSessionsFast")()
	sessions, err := registry.Load(path)
	if err != nil {
		return nil, err
	}
	// Skip tmux sanitize here — Init() runs loadSessionsFull immediately with
	// one shared snapshot. Sanitizing with a nil snapshot forces a cold pane
	// scan that loadSessionsFull would repeat.
	registry.SortSessions(sessions)
	return sessions, nil
}

func loadSessionsFromSnapshot(path string, snap *tmux.Snapshot, discover bool) ([]registry.Session, error) {
	sessions, err := registry.Load(path)
	if err != nil {
		return nil, err
	}
	sanitized, pruned := registry.Sanitize(sessions, sanitizeOptsFromSnapshot(snap))
	if registry.ShouldPersistSanitize(sessions, sanitized, pruned) {
		_ = registry.Save(path, sanitized)
	}
	out := sanitized
	if discover {
		out = mergeDiscoveredSessions(sanitized, snap)
	}
	registry.SortSessions(out)
	return enrichAllFromSnapshot(out, snap), nil
}

func loadSessionsFull(path string) ([]registry.Session, error) {
	defer profileStart("loadSessionsFull")()
	snap, err := tmux.GetSnapshot(false)
	if err != nil {
		return nil, err
	}
	return loadSessionsFromSnapshot(path, snap, true)
}

func reloadRegistry(path string, current []registry.Session) ([]registry.Session, error) {
	defer profileStart("reload")()
	fresh, err := registry.Load(path)
	if err != nil {
		return nil, err
	}
	snap, err := tmux.GetSnapshot(false)
	if err != nil {
		return nil, err
	}
	// Registry on disk can still list panes where pi exited; sanitize before
	// merge so periodic reloads do not resurrect pruned rows.
	sanitized, _ := registry.Sanitize(fresh, sanitizeOptsFromSnapshot(snap))
	next := refreshSessionsFromRegistry(current, sanitized, snap)
	return enrichAllFromSnapshot(next, snap), nil
}

func reloadDiscovery(path string, current []registry.Session) ([]registry.Session, error) {
	defer profileStart("reloadDiscovery")()
	tmux.InvalidateSnapshot()
	snap, err := tmux.GetSnapshot(true)
	if err != nil {
		return current, err
	}
	loaded, err := loadSessionsFromSnapshot(path, snap, true)
	if err != nil {
		return current, err
	}
	return mergeSessionsIncremental(current, loaded), nil
}

func sanitizeAndPersist(path string, sessions []registry.Session) []registry.Session {
	loaded, err := loadSessionsFull(path)
	if err != nil {
		return sessions
	}
	return loaded
}

func needsEnrich(session registry.Session) bool {
	return strings.TrimSpace(session.TmuxSession) == "" ||
		strings.TrimSpace(session.CWD) == "" ||
		strings.TrimSpace(session.TmuxWindow) == "" ||
		strings.TrimSpace(session.TmuxPane) == ""
}

func enrichSessionFromSnapshot(session registry.Session, snap *tmux.Snapshot) registry.Session {
	if !needsEnrich(session) {
		return session
	}
	info, ok := snap.PaneInfo(session.TmuxTarget)
	if !ok {
		return session
	}
	return applyPaneInfo(session, info)
}

func applyPaneInfo(session registry.Session, info tmux.PaneInfo) registry.Session {
	if session.TmuxSession == "" && info.SessionName != "" {
		session.TmuxSession = info.SessionName
	}
	if session.TmuxWindow == "" && info.WindowIndex != "" {
		session.TmuxWindow = info.WindowIndex
	}
	if session.TmuxPane == "" && info.PaneIndex != "" {
		session.TmuxPane = info.PaneIndex
	}
	if session.CWD == "" && info.CurrentPath != "" {
		session.CWD = info.CurrentPath
	}
	return session
}

func enrichAllFromSnapshot(sessions []registry.Session, snap *tmux.Snapshot) []registry.Session {
	if len(sessions) == 0 || snap == nil {
		return sessions
	}
	out := append([]registry.Session(nil), sessions...)
	for i := range out {
		if needsEnrich(out[i]) {
			out[i] = enrichSessionFromSnapshot(out[i], snap)
		}
	}
	return out
}

func mergeRegistryFields(dst, src registry.Session) registry.Session {
	dst.Status = src.Status
	dst.ToolName = src.ToolName
	dst.UpdatedAt = src.UpdatedAt
	dst.LastPrompt = src.LastPrompt
	dst.LastPromptAt = src.LastPromptAt
	dst.Branch = src.Branch
	dst.Title = src.Title
	dst.Model = src.Model
	if src.CWD != "" {
		dst.CWD = src.CWD
	}
	if src.TmuxSession != "" {
		dst.TmuxSession = src.TmuxSession
	}
	if src.TmuxWindow != "" {
		dst.TmuxWindow = src.TmuxWindow
	}
	if src.TmuxPane != "" {
		dst.TmuxPane = src.TmuxPane
	}
	return dst
}

func paneHasLivePiAgent(target string, snap *tmux.Snapshot) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if snap != nil {
		return snap.HasPiAgent(target)
	}
	return tmux.PaneHasPiAgent(target)
}

// refreshSessionsFromRegistry updates live status fields from an existing
// tmux snapshot.
func refreshSessionsFromRegistry(current, fresh []registry.Session, snap *tmux.Snapshot) []registry.Session {
	defer profileStart("refreshSessionsFromRegistry")()

	freshByTarget := make(map[string]registry.Session, len(fresh))
	for _, session := range fresh {
		target := strings.TrimSpace(session.TmuxTarget)
		if target != "" {
			freshByTarget[target] = session
		}
	}

	out := make([]registry.Session, 0, len(current)+len(fresh))
	seen := make(map[string]struct{}, len(current))

	for _, session := range current {
		target := strings.TrimSpace(session.TmuxTarget)
		if target == "" {
			continue
		}
		if freshSession, ok := freshByTarget[target]; ok {
			session = mergeRegistryFields(session, freshSession)
			out = append(out, session)
			seen[target] = struct{}{}
			continue
		}
		if isDiscovered(session) && paneHasLivePiAgent(target, snap) {
			out = append(out, session)
		}
	}

	for target, session := range freshByTarget {
		if _, ok := seen[target]; ok {
			continue
		}
		out = append(out, session)
	}
	registry.SortSessions(out)
	return out
}
