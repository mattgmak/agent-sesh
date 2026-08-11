package tmux

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	defaultSnapshotTTL = 10 * time.Second
	paneListFormat     = "#{pane_id}\t#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_tty}\t#{pane_current_command}\t#{pane_start_command}\t#{pane_current_path}"
)

// Snapshot is a cached tmux list-panes result used to avoid per-pane subprocess storms.
type Snapshot struct {
	panes     map[string]PaneInfo
	fetchedAt time.Time
}

var (
	snapshotMu    sync.Mutex
	snapshotCache *Snapshot
	snapshotTTL   = defaultSnapshotTTL
)

// InvalidateSnapshot drops the in-memory pane snapshot.
func InvalidateSnapshot() {
	snapshotMu.Lock()
	snapshotCache = nil
	snapshotMu.Unlock()
}

// GetSnapshot returns a cached pane snapshot, refreshing when stale or forced.
func GetSnapshot(force bool) (*Snapshot, error) {
	snapshotMu.Lock()
	if !force && snapshotCache != nil && time.Since(snapshotCache.fetchedAt) < snapshotTTL {
		snap := snapshotCache
		snapshotMu.Unlock()
		return snap, nil
	}
	snapshotMu.Unlock()
	return refreshSnapshot()
}

func refreshSnapshot() (*Snapshot, error) {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", paneListFormat).Output()
	if err != nil {
		return nil, err
	}

	panes := make(map[string]PaneInfo)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 8 {
			continue
		}
		target := fields[0]
		info := PaneInfo{
			Target:         target,
			PaneID:         fields[0],
			SessionName:    fields[1],
			WindowIndex:    fields[2],
			PaneIndex:      fields[3],
			TTY:            fields[4],
			CurrentCommand: fields[5],
			StartCommand:   fields[6],
			CurrentPath:    fields[7],
			Exists:         true,
		}
		info.HasPiAgent = detectPiAgent(info)
		panes[target] = info
	}

	snap := &Snapshot{panes: panes, fetchedAt: time.Now()}
	snapshotMu.Lock()
	snapshotCache = snap
	snapshotMu.Unlock()
	return snap, nil
}

func detectPiAgent(info PaneInfo) bool {
	if isPiCommand(info.CurrentCommand, info.StartCommand) {
		return true
	}
	if !needsDeepPiCheck(info.CurrentCommand) {
		return false
	}
	return paneHasPiOnTTY(info.Target)
}

func isPiCommand(current, start string) bool {
	return isPiProcessLine(current) || isPiProcessLine(start)
}

func needsDeepPiCheck(current string) bool {
	current = strings.TrimSpace(current)
	if current == "" {
		return true
	}
	base := current
	if idx := strings.LastIndex(current, "/"); idx >= 0 {
		base = current[idx+1:]
	}
	base = strings.Fields(base)[0]
	switch base {
	case "bash", "zsh", "sh", "fish", "nu", "dash", "ksh", "tcsh":
		return true
	default:
		return false
	}
}

// HasPane reports whether target exists in the snapshot.
func (s *Snapshot) HasPane(target string) bool {
	if s == nil {
		return false
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	_, ok := s.panes[target]
	return ok
}

// PaneInfo returns cached pane metadata when present.
func (s *Snapshot) PaneInfo(target string) (PaneInfo, bool) {
	if s == nil {
		return PaneInfo{}, false
	}
	info, ok := s.panes[strings.TrimSpace(target)]
	return info, ok
}

// HasPiAgent reports whether the pane runs pi according to the snapshot.
func (s *Snapshot) HasPiAgent(target string) bool {
	info, ok := s.PaneInfo(target)
	return ok && info.HasPiAgent
}

// PiPanes returns panes with pi agents from the snapshot.
func (s *Snapshot) PiPanes() []PaneInfo {
	if s == nil {
		return nil
	}
	out := make([]PaneInfo, 0)
	for _, info := range s.panes {
		if info.HasPiAgent {
			out = append(out, info)
		}
	}
	return out
}

// PaneIDs returns all pane ids from the snapshot.
func (s *Snapshot) PaneIDs() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.panes))
	for id := range s.panes {
		out = append(out, id)
	}
	return out
}
