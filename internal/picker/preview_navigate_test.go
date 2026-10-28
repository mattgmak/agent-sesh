package picker

import (
	"testing"
)

func TestSchedulePreviewNavigateDefersFetch(t *testing.T) {
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
	m.cursor = 1
	m.selectedID = sessions[1].ID
	m.selectedTarget = sessions[1].TmuxTarget

	// Last capture of that pane still shows while the debounce runs.
	storePreviewCapture(sessions[1].TmuxTarget, "stale body")

	cmd := (&m).schedulePreviewNavigate()
	if cmd == nil {
		t.Fatal("expected deferred fetch tick")
	}
	if m.previewContent != "stale body" {
		t.Fatalf("expected stale preview, got %q", m.previewContent)
	}
	if m.previewPending != sessions[1].TmuxTarget {
		t.Fatalf("expected pending target %q, got %q", sessions[1].TmuxTarget, m.previewPending)
	}
}

func TestSchedulePreviewNavigateFetchesImmediatelyWithoutCache(t *testing.T) {
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
	m.previewTarget = "%other-pane"
	m.previewContent = "old pane body"

	cmd := (&m).schedulePreviewNavigate()
	if cmd == nil {
		t.Fatal("expected immediate fetch command")
	}
	if m.previewContent != "" {
		t.Fatalf("expected cleared preview, got %q", m.previewContent)
	}
	if m.previewPending != sessions[0].TmuxTarget {
		t.Fatalf("pending = %q", m.previewPending)
	}
}

func TestSchedulePreviewNavigateSameTargetSkipsFetch(t *testing.T) {
	sessions := sampleSessions()
	m := testModel(sessions)
	m.width = 120
	m.height = 24
	m.syncInputWidth()
	m.previewTarget = sessions[0].TmuxTarget
	m.previewContent = "fresh body"

	cmd := (&m).schedulePreviewNavigate()
	if cmd != nil {
		t.Fatal("expected a cursor move inside one pane to skip the capture")
	}
	if m.previewContent != "fresh body" {
		t.Fatalf("preview = %q", m.previewContent)
	}
}

func TestPreviewRefreshMsgStartsImmediateCapture(t *testing.T) {
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
	m.previewTarget = "%other-pane"
	storePreviewCapture(sessions[0].TmuxTarget, "stale body")
	if cmd := (&m).schedulePreviewNavigate(); cmd == nil {
		t.Fatal("expected deferred preview fetch")
	}
	debounceSeq := m.previewSeq

	updated, cmd := m.Update(previewRefreshMsg{
		seq:    debounceSeq,
		id:     sessions[0].ID,
		target: sessions[0].TmuxTarget,
	})
	got := updated.(model)
	if cmd == nil {
		t.Fatal("expected immediate capture after the debounce")
	}
	if got.previewPending != sessions[0].TmuxTarget {
		t.Fatalf("previewPending = %q, want %q", got.previewPending, sessions[0].TmuxTarget)
	}
	if got.previewSeq <= debounceSeq {
		t.Fatalf("previewSeq = %d, want greater than %d", got.previewSeq, debounceSeq)
	}
}
