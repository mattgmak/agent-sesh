package picker

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

// stubCapture replaces the tmux capture with a scripted sequence of pane text.
func stubCapture(t *testing.T, contents ...string) *int {
	t.Helper()
	calls := new(int)
	prev := capturePane
	capturePane = func(string, int) (string, error) {
		i := *calls
		if i >= len(contents) {
			i = len(contents) - 1
		}
		*calls++
		return contents[i], nil
	}
	t.Cleanup(func() { capturePane = prev })
	return calls
}

func runPreviewCmd(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a capture command")
	}
	updated, _ := m.Update(cmd())
	*m = updated.(model)
}

func previewTestModel(t *testing.T) model {
	t.Helper()
	sessions := sampleSessions()
	for _, s := range sessions {
		invalidatePreviewCache(s.TmuxTarget)
	}
	t.Cleanup(func() {
		for _, s := range sessions {
			invalidatePreviewCache(s.TmuxTarget)
		}
	})
	m := testModel(sessions)
	m.width = 120
	m.height = 24
	m.syncInputWidth()
	return m
}

func TestPreviewContentKeyTracksBytes(t *testing.T) {
	sameBytes := "pane " + "body"
	if previewContentKey("pane body") != previewContentKey(sameBytes) {
		t.Fatal("identical captures must share a key")
	}
	if previewContentKey("pane body") == previewContentKey("pane body ") {
		t.Fatal("new bytes must get a new key")
	}
}

func TestPreviewCacheKeepsOneTarget(t *testing.T) {
	invalidatePreviewCache("%test-cache-a")
	invalidatePreviewCache("%test-cache-b")
	t.Cleanup(func() {
		invalidatePreviewCache("%test-cache-a")
		invalidatePreviewCache("%test-cache-b")
	})

	storePreviewCapture("%test-cache-a", "pane a")
	if content, key, ok := cachedPreview("%test-cache-a"); !ok || content != "pane a" || key != previewContentKey("pane a") {
		t.Fatalf("cache miss: ok=%v content=%q key=%d", ok, content, key)
	}

	storePreviewCapture("%test-cache-b", "pane b")
	if _, _, ok := cachedPreview("%test-cache-a"); ok {
		t.Fatal("cache must keep only the current target")
	}
	if content, _, ok := cachedPreview("%test-cache-b"); !ok || content != "pane b" {
		t.Fatalf("cache lost the current target: ok=%v content=%q", ok, content)
	}
}

func TestPreviewCacheServesIdenticalCapture(t *testing.T) {
	calls := stubCapture(t, "pane v1", "pane v1", "pane v2")
	m := previewTestModel(t)

	runPreviewCmd(t, &m, (&m).schedulePreview())
	if m.previewContent != "pane v1" {
		t.Fatalf("first capture = %q", m.previewContent)
	}
	key := m.previewKey
	if key != previewContentKey("pane v1") {
		t.Fatalf("previewKey = %d, want the capture hash", key)
	}

	// Second capture returns the same bytes: the cached render still serves.
	m.previewLastFetch = time.Time{}
	cached, cachedKey, ok := cachedPreview(m.sessions[0].TmuxTarget)
	if !ok || cachedKey != key {
		t.Fatalf("cache entry = key=%d ok=%v, want key=%d", cachedKey, ok, key)
	}
	runPreviewCmd(t, &m, (&m).schedulePreview())
	if *calls != 2 {
		t.Fatalf("capture calls = %d, want 2", *calls)
	}
	if m.previewKey != key || m.previewContent != cached {
		t.Fatalf("identical capture changed preview state: key=%d content=%q", m.previewKey, m.previewContent)
	}

	// Third capture returns new bytes: the cached render is invalidated.
	m.previewLastFetch = time.Time{}
	runPreviewCmd(t, &m, (&m).schedulePreview())
	if m.previewContent != "pane v2" {
		t.Fatalf("previewContent = %q, want the new bytes", m.previewContent)
	}
	if m.previewKey == key {
		t.Fatal("new bytes kept the old content key")
	}
}

func TestBusyPaneRefetchesWhileStatusIsUnchanged(t *testing.T) {
	// Status and tool name stay "working" between both polls. The old
	// Status+ToolName key froze the preview here.
	calls := stubCapture(t, "spinner frame 1", "spinner frame 2")
	m := previewTestModel(t)

	runPreviewCmd(t, &m, (&m).schedulePreview())
	if m.previewContent != "spinner frame 1" {
		t.Fatalf("first capture = %q", m.previewContent)
	}
	if m.sessions[0].Status != registry.StatusWorking || m.sessions[0].ToolName != "" {
		t.Fatalf("test needs an unchanged working session, got %+v", m.sessions[0])
	}

	m.previewLastFetch = time.Time{}
	cmd := (&m).schedulePreview()
	if cmd == nil {
		t.Fatal("busy pane froze: no capture after an unchanged status")
	}
	runPreviewCmd(t, &m, cmd)
	if m.previewContent != "spinner frame 2" {
		t.Fatalf("previewContent = %q, want the second frame", m.previewContent)
	}
	if *calls != 2 {
		t.Fatalf("capture calls = %d, want 2", *calls)
	}
}

func TestSchedulePreviewRunsOneCaptureAtATime(t *testing.T) {
	stubCapture(t, "pane body")
	m := previewTestModel(t)

	cmd := (&m).schedulePreview()
	if cmd == nil {
		t.Fatal("expected a capture command")
	}
	if second := (&m).schedulePreview(); second != nil {
		t.Fatal("second capture queued while one was in flight")
	}
	if second := (&m).schedulePreviewNavigate(); second != nil {
		t.Fatal("navigate capture queued while one was in flight")
	}
}

func TestStaleCaptureReleasesPendingSlot(t *testing.T) {
	stubCapture(t, "pane one", "pane two")
	m := previewTestModel(t)

	if cmd := (&m).schedulePreview(); cmd == nil {
		t.Fatal("expected the first capture")
	}
	staleSeq := m.previewSeq

	// The user moves to the second pane while the first capture is in flight.
	m = m.setCursor(1)
	if cmd := (&m).schedulePreview(); cmd != nil {
		t.Fatal("queued a second capture while one was in flight")
	}

	updated, cmd := m.Update(previewLoadedMsg{
		seq:     staleSeq,
		id:      m.sessions[0].ID,
		target:  m.sessions[0].TmuxTarget,
		content: "pane one",
	})
	got := updated.(model)
	if cmd == nil {
		t.Fatal("stale capture left the preview stuck")
	}
	if got.previewPending != m.sessions[1].TmuxTarget {
		t.Fatalf("previewPending = %q, want %q", got.previewPending, m.sessions[1].TmuxTarget)
	}
}

func TestPreviewLiveRefreshCadence(t *testing.T) {
	if previewLiveRefreshInterval != 500*time.Millisecond {
		t.Fatalf("live interval = %v, want 500ms", previewLiveRefreshInterval)
	}
	if previewBusyRefreshInterval != 100*time.Millisecond {
		t.Fatalf("busy interval = %v, want 100ms", previewBusyRefreshInterval)
	}

	m := previewTestModel(t)
	if got := m.previewRefreshInterval(); got != previewLiveRefreshInterval {
		t.Fatalf("idle cadence = %v, want %v", got, previewLiveRefreshInterval)
	}
	m.previewPending = m.sessions[0].TmuxTarget
	if got := m.previewRefreshInterval(); got != previewBusyRefreshInterval {
		t.Fatalf("busy cadence = %v, want %v", got, previewBusyRefreshInterval)
	}
}

func TestPreviewCacheSkipsStoreOnCaptureError(t *testing.T) {
	target := "%test-preview-error"
	invalidatePreviewCache(target)
	t.Cleanup(func() { invalidatePreviewCache(target) })

	prev := capturePane
	capturePane = func(string, int) (string, error) { return "", context.DeadlineExceeded }
	t.Cleanup(func() { capturePane = prev })

	m := previewTestModel(t)
	runPreviewCmd(t, &m, (&m).fetchPreview(1, m.sessions[0].ID, target))
	if _, _, ok := cachedPreview(target); ok {
		t.Fatal("capture error was stored in the preview cache")
	}
}

func TestRefreshSessionsFromRegistry(t *testing.T) {
	current := []registry.Session{{
		ID:          "1",
		TmuxTarget:  "%1",
		TmuxSession: "nixconfig",
		Status:      registry.StatusIdle,
	}}
	fresh := []registry.Session{{
		ID:         "1",
		TmuxTarget: "%1",
		Status:     registry.StatusWorking,
		ToolName:   "Shell",
		UpdatedAt:  "2026-01-02T12:00:00Z",
	}}

	got := refreshSessionsFromRegistry(current, fresh, nil)
	if len(got) != 1 {
		t.Fatalf("expected one session, got %d", len(got))
	}
	if got[0].Status != registry.StatusWorking || got[0].ToolName != "Shell" {
		t.Fatalf("unexpected merged session: %+v", got[0])
	}
	if got[0].TmuxSession != "nixconfig" {
		t.Fatalf("expected tmux metadata preserved, got %+v", got[0])
	}
}
