package picker

import (
	"testing"
)

func TestSchedulePreviewNavigateDefersFetch(t *testing.T) {
	sessions := sampleSessions()
	for _, s := range sessions {
		invalidatePreviewCache(s.TmuxTarget)
	}

	m := testModel(sessions)
	m.width = 120
	m.height = 24
	m.syncInputWidth()
	m.cursor = 1
	m.selectedID = sessions[1].ID
	m.selectedTarget = sessions[1].TmuxTarget

	// Stale preview from another revision still shown immediately.
	setPreviewCache(sessions[1].TmuxTarget, "old-rev", "stale body", nil)

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

	m := testModel(sessions)
	m.width = 120
	m.height = 24
	m.syncInputWidth()
	m.previewTarget = sessions[0].TmuxTarget
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

func TestSchedulePreviewNavigateExactCacheSkipsFetch(t *testing.T) {
	session := sampleSessions()[0]
	invalidatePreviewCache(session.TmuxTarget)
	rev := previewRevision(session)
	setPreviewCache(session.TmuxTarget, rev, "fresh body", nil)

	m := testModel(sampleSessions())
	m.width = 120
	m.height = 24
	m.syncInputWidth()

	cmd := (&m).schedulePreviewNavigate()
	if cmd != nil {
		t.Fatal("expected exact cache hit to skip deferred fetch")
	}
	if m.previewContent != "fresh body" {
		t.Fatalf("preview = %q", m.previewContent)
	}
}

func TestPreviewRefreshRevisionMismatchRefetches(t *testing.T) {
	sessions := sampleSessions()
	session := sessions[1]
	invalidatePreviewCache(session.TmuxTarget)
	t.Cleanup(func() { invalidatePreviewCache(session.TmuxTarget) })

	m := testModel(sessions)
	m.width = 120
	m.height = 24
	m.syncInputWidth()
	m.cursor = 1
	m.selectedID = session.ID
	m.selectedTarget = session.TmuxTarget

	setPreviewCache(session.TmuxTarget, "old-revision", "stale body", nil)
	if cmd := (&m).schedulePreviewNavigate(); cmd == nil {
		t.Fatal("expected deferred preview fetch")
	}
	debounceSeq := m.previewSeq
	debounceRevision := m.previewRevision

	m.sessions[m.cursor].Status = "working"
	m.sessions[m.cursor].ToolName = "Shell"

	updated, cmd := m.Update(previewRefreshMsg{
		seq:      debounceSeq,
		id:       session.ID,
		target:   session.TmuxTarget,
		rev:      debounceRevision,
	})
	got := updated.(model)
	if cmd == nil {
		t.Fatal("expected immediate fetch for current revision")
	}
	if got.previewPending != session.TmuxTarget {
		t.Fatalf("previewPending = %q, want %q", got.previewPending, session.TmuxTarget)
	}
	if got.previewSeq <= debounceSeq {
		t.Fatalf("previewSeq = %d, want greater than %d", got.previewSeq, debounceSeq)
	}
}
