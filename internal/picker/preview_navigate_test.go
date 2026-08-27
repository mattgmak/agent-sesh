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
