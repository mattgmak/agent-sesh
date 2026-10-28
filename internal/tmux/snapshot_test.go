package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsPiCommand(t *testing.T) {
	if !isPiCommand("pi", "") {
		t.Fatal("expected bare pi command")
	}
	if !isPiCommand("", "/usr/local/bin/pi --model sonnet") {
		t.Fatal("expected pi in start command")
	}
	if isPiCommand("lazygit", "") {
		t.Fatal("lazygit should not match")
	}
}

func TestTtyBaseName(t *testing.T) {
	cases := map[string]string{
		"/dev/ttys004": "ttys004",
		"ttys004":      "ttys004",
		"/dev/pts/0":   "pts/0",
		"  /dev/tty1 ": "tty1",
	}
	for in, want := range cases {
		if got := ttyBaseName(in); got != want {
			t.Fatalf("ttyBaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollectPiTTYs(t *testing.T) {
	out := "ttys001 /bin/ls\n" +
		"ttys004 /usr/local/bin/pi\n" +
		"ttys005 nu\n" +
		"?? /usr/bin/foo\n" +
		"ttys006 /nix/store/abc-pi/bin/runner\n"
	got := collectPiTTYs(out)
	if !got["ttys004"] {
		t.Fatal("expected ttys004 (bare pi) to be flagged")
	}
	if got["ttys001"] || got["ttys005"] || got["ttys006"] {
		t.Fatalf("unexpected pi ttys: %v", got)
	}
}

func TestSnapshotHasPane(t *testing.T) {
	snap := &Snapshot{
		panes: map[string]PaneInfo{
			"%1": {Target: "%1", Exists: true},
		},
	}
	if !snap.HasPane("%1") || snap.HasPane("%2") {
		t.Fatalf("unexpected HasPane results")
	}
}

func TestPiPanesFilters(t *testing.T) {
	snap := &Snapshot{
		panes: map[string]PaneInfo{
			"%1": {Target: "%1", HasPiAgent: true},
			"%2": {Target: "%2", HasPiAgent: false},
			// Subagent surfaces run pi (HasPiAgent) but must be excluded.
			"%3": {Target: "%3", HasPiAgent: true, IsSubagent: true},
		},
	}
	got := snap.PiPanes()
	if len(got) != 1 || got[0].Target != "%1" {
		t.Fatalf("PiPanes() = %+v", got)
	}
}

func TestCachedPiAgentTTYsEmpty(t *testing.T) {
	got, err := cachedPiAgentTTYs(nil)
	if err != nil || got != nil {
		t.Fatalf("nil set = (%v, %v), want (nil, nil)", got, err)
	}
	got, err = cachedPiAgentTTYs(map[string]struct{}{})
	if err != nil || got != nil {
		t.Fatalf("empty set = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestRefreshSnapshotErrorPreservesCaches(t *testing.T) {
	before := &Snapshot{
		panes:     map[string]PaneInfo{"%old": {Target: "%old", Exists: true}},
		fetchedAt: time.Now(),
	}
	snapshotMu.Lock()
	oldSnapshot := snapshotCache
	snapshotCache = before
	snapshotMu.Unlock()
	t.Cleanup(func() {
		snapshotMu.Lock()
		snapshotCache = oldSnapshot
		snapshotMu.Unlock()
	})

	oldPiScanKey := "old-tty"
	oldPiScanTTYs := map[string]bool{"old-tty": true}
	oldPiScanAt := time.Now()
	piScanMu.Lock()
	oldPiKey, oldPiTTYs, oldPiAt := piScanKey, piScanTTYs, piScanAt
	piScanKey, piScanTTYs, piScanAt = oldPiScanKey, oldPiScanTTYs, oldPiScanAt
	piScanMu.Unlock()
	t.Cleanup(func() {
		piScanMu.Lock()
		piScanKey, piScanTTYs, piScanAt = oldPiKey, oldPiTTYs, oldPiAt
		piScanMu.Unlock()
	})

	binDir := t.TempDir()
	writeFakeCommand(t, binDir, "tmux", `#!/bin/sh
printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' '%1' 'work' '2' '3' '/dev/ttys999' 'zsh' 'zsh' '/tmp/project'
`)
	writeFakeCommand(t, binDir, "ps", "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", binDir)

	if _, err := refreshSnapshot(); err == nil {
		t.Fatal("refreshSnapshot() succeeded despite ps failure")
	}
	snapshotMu.Lock()
	cached := snapshotCache
	snapshotMu.Unlock()
	if cached != before {
		t.Fatal("failed refresh replaced snapshot cache")
	}
	piScanMu.Lock()
	defer piScanMu.Unlock()
	if piScanKey != oldPiScanKey || !piScanTTYs["old-tty"] || !piScanAt.Equal(oldPiScanAt) {
		t.Fatalf("failed refresh changed pi scan cache: key=%q ttys=%v at=%v", piScanKey, piScanTTYs, piScanAt)
	}
}

func TestRefreshSnapshotSingleFlight(t *testing.T) {
	snapshotMu.Lock()
	oldSnapshot := snapshotCache
	snapshotMu.Unlock()
	t.Cleanup(func() {
		snapshotMu.Lock()
		snapshotCache = oldSnapshot
		snapshotMu.Unlock()
	})

	binDir := t.TempDir()
	callLog := filepath.Join(t.TempDir(), "calls")
	gate := filepath.Join(t.TempDir(), "gate")
	t.Setenv("AGENT_SESH_TEST_CALL_LOG", callLog)
	t.Setenv("AGENT_SESH_TEST_GATE", gate)
	// The pane runs pi, so neither the tty scan nor the process tree scan runs.
	writeFakeCommand(t, binDir, "tmux", `#!/bin/sh
printf 'tmux\n' >> "$AGENT_SESH_TEST_CALL_LOG"
while [ ! -f "$AGENT_SESH_TEST_GATE" ]; do :; done
printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' '%1' 'work' '2' '3' '/dev/ttys999' 'pi' 'pi' '/tmp/project'
`)
	t.Setenv("PATH", binDir)

	type result struct {
		snap *Snapshot
		err  error
	}
	results := make(chan result, 2)
	go func() {
		snap, err := refreshSnapshot()
		results <- result{snap, err}
	}()

	waitForFile(t, callLog, 2*time.Second)
	go func() {
		snap, err := refreshSnapshot()
		results <- result{snap, err}
	}()
	if err := os.WriteFile(gate, []byte("go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		select {
		case res := <-results:
			if res.err != nil {
				t.Fatalf("refreshSnapshot() error = %v", res.err)
			}
			if !res.snap.HasPane("%1") {
				t.Fatal("waiter did not share the in-flight snapshot")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("refreshSnapshot() did not return")
		}
	}

	data, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "tmux"); got != 1 {
		t.Fatalf("tmux exec calls = %d, want 1", got)
	}
}

func TestSnapshotDiskCacheRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	snap := &Snapshot{
		panes: map[string]PaneInfo{
			"%1": {Target: "%1", TTY: "/dev/ttys004", ShellPID: 42, Exists: true, HasPiAgent: true},
		},
		fetchedAt: time.Now(),
	}
	if err := SaveSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	got, err := CachedSnapshot(10 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasPane("%1") || !got.HasPiAgent("%1") {
		t.Fatalf("round trip lost pane: %+v", got)
	}
	if info, _ := got.PaneInfo("%1"); info.ShellPID != 42 || !got.authoritative() {
		t.Fatalf("round trip lost fields: %+v", info)
	}

	if stale, err := CachedSnapshot(0); err != nil || stale != nil {
		t.Fatalf("stale snapshot = (%v, %v), want (nil, nil)", stale, err)
	}

	InvalidateSnapshot()
	if after, err := CachedSnapshot(time.Minute); err != nil || after != nil {
		t.Fatalf("invalidated snapshot = (%v, %v), want (nil, nil)", after, err)
	}
}

func TestCachedSnapshotMissingFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	got, err := CachedSnapshot(time.Minute)
	if err != nil || got != nil {
		t.Fatalf("missing file = (%v, %v), want (nil, nil)", got, err)
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestCachedPiAgentTTYsReuse(t *testing.T) {
	// Pre-populate the cache as if a scan just ran for ttys004,ttys005.
	piScanMu.Lock()
	piScanKey = "ttys004,ttys005"
	piScanTTYs = map[string]bool{"ttys004": true}
	piScanAt = time.Now()
	piScanMu.Unlock()

	got, err := cachedPiAgentTTYs(map[string]struct{}{"ttys004": {}, "ttys005": {}})
	if err != nil {
		t.Fatal(err)
	}
	if !got["ttys004"] || got["ttys005"] {
		t.Fatalf("expected cached result reuse, got %v", got)
	}

	// Reset cache state so later tests aren't affected.
	piScanMu.Lock()
	piScanKey = ""
	piScanTTYs = nil
	piScanAt = time.Time{}
	piScanMu.Unlock()
}
