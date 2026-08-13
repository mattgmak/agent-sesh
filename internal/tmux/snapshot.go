package tmux

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultSnapshotTTL = 10 * time.Second
	// piScanTTL bounds how stale the tty→pi detection result may get when the
	// set of ttys to scan is unchanged. ps has a fixed ~20ms startup cost on
	// macOS, so this cache avoids re-paying it on every snapshot refresh.
	piScanTTL      = 15 * time.Second
	paneListFormat = "#{pane_id}\t#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_tty}\t#{pane_current_command}\t#{pane_start_command}\t#{pane_current_path}"
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

	piScanMu   sync.Mutex
	piScanKey  string
	piScanTTYs map[string]bool
	piScanAt   time.Time
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
	out, err := execOutput("tmux.list-panes", "tmux", "list-panes", "-a", "-F", paneListFormat)
	if err != nil {
		return nil, err
	}

	panes := make(map[string]PaneInfo)
	scanTTYs := make(map[string]struct{})
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
		info.HasPiAgent = isPiCommand(info.CurrentCommand, info.StartCommand)
		if !info.HasPiAgent {
			if tty := ttyBaseName(info.TTY); tty != "" {
				scanTTYs[tty] = struct{}{}
			}
		}
		panes[target] = info
	}

	// One scoped `ps -t` scan covering only the pane ttys whose command didn't
	// already prove pi, instead of a full-system `ps -e` scan. The result is
	// cached by tty set (see cachedPiAgentTTYs).
	piTTYs := cachedPiAgentTTYs(scanTTYs)

	for target, info := range panes {
		if !info.HasPiAgent {
			info.HasPiAgent = piTTYs[ttyBaseName(info.TTY)]
		}
		panes[target] = info
	}

	snap := &Snapshot{panes: panes, fetchedAt: time.Now()}
	snapshotMu.Lock()
	snapshotCache = snap
	snapshotMu.Unlock()
	return snap, nil
}

// cachedPiAgentTTYs returns the tty→pi map for the given ttys, reusing the
// previous ps scan when the tty set is unchanged and the result is still
// fresh. Because ps carries a fixed ~20ms startup cost regardless of the
// column requested, avoiding re-scans is the dominant optimization here.
func cachedPiAgentTTYs(scanTTYs map[string]struct{}) map[string]bool {
	if len(scanTTYs) == 0 {
		return nil
	}
	ttys := make([]string, 0, len(scanTTYs))
	for tty := range scanTTYs {
		ttys = append(ttys, tty)
	}
	sort.Strings(ttys)
	key := strings.Join(ttys, ",")

	piScanMu.Lock()
	defer piScanMu.Unlock()
	if piScanKey == key && time.Since(piScanAt) < piScanTTL {
		return piScanTTYs
	}
	result := piAgentTTYs(ttys)
	piScanKey = key
	piScanTTYs = result
	piScanAt = time.Now()
	return result
}

func detectPiAgent(info PaneInfo) bool {
	if isPiCommand(info.CurrentCommand, info.StartCommand) {
		return true
	}
	// The pane's foreground command may be the agent runtime (e.g. node for
	// pi) rather than `pi` itself, and the start command may be wrapped by
	// reattach-to-user-namespace or a login shell. The tty scan is the ground
	// truth: check every pane whose command doesn't already prove pi.
	return paneHasPiOnTTY(info.Target)
}

func isPiCommand(current, start string) bool {
	return isPiProcessLine(current) || isPiProcessLine(start)
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
