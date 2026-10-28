package tmux

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

const (
	defaultSnapshotTTL = 10 * time.Second
	// piScanTTL bounds how stale the tty→pi detection result may get when the
	// set of ttys to scan is unchanged. ps has a fixed ~20ms startup cost on
	// macOS, so this cache avoids re-paying it on every snapshot refresh.
	piScanTTL = 15 * time.Second
	// snapshotFileName holds the last snapshot for short-lived processes such
	// as the counts status line, which cannot reuse in-process memory.
	snapshotFileName = "pane-snapshot.json"
	paneListFormat   = "#{pane_id}\t#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_tty}\t#{pane_current_command}\t#{pane_start_command}\t#{pane_current_path}\t#{pane_pid}"
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

	// refreshMu guards refreshInFlight so one scan runs at a time; callers that
	// arrive mid-scan wait for and share that scan's result.
	refreshMu       sync.Mutex
	refreshInFlight *refreshCall
)

type refreshCall struct {
	done chan struct{}
	snap *Snapshot
	err  error
}

// InvalidateSnapshot drops the in-memory pane snapshot and its disk copy.
func InvalidateSnapshot() {
	snapshotMu.Lock()
	snapshotCache = nil
	snapshotMu.Unlock()
	if path, err := snapshotFilePath(); err == nil {
		_ = os.Remove(path)
	}
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

// refreshSnapshot runs the pane scan single-flight: concurrent callers wait for
// the in-flight scan instead of repeating its tmux/ps execs.
func refreshSnapshot() (*Snapshot, error) {
	refreshMu.Lock()
	if call := refreshInFlight; call != nil {
		refreshMu.Unlock()
		<-call.done
		return call.snap, call.err
	}
	call := &refreshCall{done: make(chan struct{})}
	refreshInFlight = call
	refreshMu.Unlock()

	call.snap, call.err = fetchSnapshot()

	refreshMu.Lock()
	refreshInFlight = nil
	refreshMu.Unlock()
	close(call.done)
	return call.snap, call.err
}

func fetchSnapshot() (*Snapshot, error) {
	out, err := execOutput("tmux.list-panes", "tmux", "list-panes", "-a", "-F", paneListFormat)
	if err != nil {
		return nil, err
	}

	panes := make(map[string]PaneInfo)
	scanTTYs := make(map[string]struct{})
	checkPIDs := make([]int, 0)
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
		if len(fields) >= 9 {
			if pid, err := strconv.Atoi(strings.TrimSpace(fields[8])); err == nil {
				info.ShellPID = pid
			}
		}
		info.HasPiAgent = isPiCommand(info.CurrentCommand, info.StartCommand)
		if !info.HasPiAgent {
			if tty := ttyBaseName(info.TTY); tty != "" {
				scanTTYs[tty] = struct{}{}
			}
			if info.ShellPID > 0 {
				checkPIDs = append(checkPIDs, info.ShellPID)
			}
		}
		panes[target] = info
	}

	// One scoped `ps -t` scan covering only the pane ttys whose command didn't
	// already prove pi, instead of a full-system `ps -e` scan. The result is
	// cached by tty set (see cachedPiAgentTTYs).
	piTTYs, err := cachedPiAgentTTYs(scanTTYs)
	if err != nil {
		return nil, err
	}
	piRoots, subagentRoots, err := agentPresenceInProcessTrees(checkPIDs, defaultProcessTreeDepth)
	if err != nil {
		return nil, err
	}

	for target, info := range panes {
		if !info.HasPiAgent {
			info.HasPiAgent = piTTYs[ttyBaseName(info.TTY)]
		}
		if !info.HasPiAgent && info.ShellPID > 0 && piRoots[info.ShellPID] {
			info.HasPiAgent = true
		}
		if info.ShellPID > 0 && subagentRoots[info.ShellPID] {
			info.IsSubagent = true
		}
		panes[target] = info
	}

	snap := &Snapshot{panes: panes, fetchedAt: time.Now()}
	snapshotMu.Lock()
	snapshotCache = snap
	snapshotMu.Unlock()
	return snap, nil
}

// CachedSnapshot returns the snapshot persisted by SaveSnapshot when it is
// younger than maxAge. It never execs tmux, so a status line can answer from
// it. A missing, unreadable, or stale file returns (nil, nil).
func CachedSnapshot(maxAge time.Duration) (*Snapshot, error) {
	path, err := snapshotFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file snapshotFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if len(file.Panes) == 0 || time.Since(file.FetchedAt) >= maxAge {
		return nil, nil
	}
	return &Snapshot{panes: file.Panes, fetchedAt: file.FetchedAt}, nil
}

// SaveSnapshot persists snap for short-lived processes that cannot reuse
// in-process memory. Concurrent writers are safe: the file is replaced by rename.
func SaveSnapshot(snap *Snapshot) error {
	if snap == nil {
		return nil
	}
	path, err := snapshotFilePath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(snapshotFile{FetchedAt: snap.fetchedAt, Panes: snap.panes})
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pane-snapshot-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

type snapshotFile struct {
	FetchedAt time.Time           `json:"fetched_at"`
	Panes     map[string]PaneInfo `json:"panes"`
}

// snapshotFilePath keeps the disk snapshot beside the registry.
func snapshotFilePath() (string, error) {
	registryPath, err := registry.DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(registryPath), snapshotFileName), nil
}

// cachedPiAgentTTYs returns the tty→pi map for the given ttys, reusing the
// previous ps scan when the tty set is unchanged and the result is still
// fresh. Because ps carries a fixed ~20ms startup cost regardless of the
// column requested, avoiding re-scans is the dominant optimization here.
func cachedPiAgentTTYs(scanTTYs map[string]struct{}) (map[string]bool, error) {
	if len(scanTTYs) == 0 {
		return nil, nil
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
		return piScanTTYs, nil
	}
	result, err := piAgentTTYs(ttys)
	if err != nil {
		return nil, err
	}
	piScanKey = key
	piScanTTYs = result
	piScanAt = time.Now()
	return result, nil
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

// authoritative reports whether the snapshot is fresh enough to trust a
// negative pane lookup without a per-target tmux exec.
func (s *Snapshot) authoritative() bool {
	return s != nil && time.Since(s.fetchedAt) < snapshotTTL
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

// PiPanes returns panes with pi agents, excluding pi-interactive-subagents
// surfaces (they run pi but must not surface as pi sessions).
func (s *Snapshot) PiPanes() []PaneInfo {
	if s == nil {
		return nil
	}
	out := make([]PaneInfo, 0)
	for _, info := range s.panes {
		if info.HasPiAgent && !info.IsSubagent {
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
