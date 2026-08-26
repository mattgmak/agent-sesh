package picker

import (
	"testing"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

func TestPreviewLoadedMsgIgnoresStalePaneInSameWindow(t *testing.T) {
	sessions := []registry.Session{
		{
			ID:         "pane-a",
			TmuxTarget: "%pane-a",
			TmuxWindow: "1",
			TmuxPane:   "0",
			Status:     registry.StatusWorking,
			ToolName:   "Shell",
		},
		{
			ID:         "pane-b",
			TmuxTarget: "%pane-b",
			TmuxWindow: "1",
			TmuxPane:   "1",
			Status:     registry.StatusWorking,
			ToolName:   "Shell",
		},
	}
	for _, s := range sessions {
		invalidatePreviewCache(s.TmuxTarget)
	}

	m := testModel(sessions)
	m.width = 120
	m.height = 24
	m.syncInputWidth()
	m.selectedID = "pane-b"
	m.selectedTarget = "%pane-b"
	m.reconcileCursor()

	rev := previewRevision(sessions[1])
	setPreviewCache(sessions[1].TmuxTarget, rev, "pane-b preview", nil)

	// Simulate an in-flight capture for pane-a that shares seq with the cache hit below.
	m.previewName = "pane-a"
	m.previewTarget = "%pane-a"
	m.previewContent = "pane-a preview"
	m.previewSeq = 7

	cmd := (&m).schedulePreview()
	if cmd != nil {
		t.Fatal("expected cache hit to skip tmux fetch")
	}
	if m.previewTarget != "%pane-b" || m.previewContent != "pane-b preview" {
		t.Fatalf("cache hit preview = target=%q content=%q, want pane-b", m.previewTarget, m.previewContent)
	}
	if m.previewSeq != 8 {
		t.Fatalf("expected previewSeq bump on pane switch, got %d", m.previewSeq)
	}

	// Stale capture with the pre-switch seq is ignored after the bump.
	stale := previewLoadedMsg{
		seq:      7,
		id:       "pane-a",
		target:   "%pane-a",
		revision: rev,
		content:  "stale pane-a preview",
	}
	updated, _ := m.Update(stale)
	um := updated.(model)
	if um.previewTarget != "%pane-b" || um.previewContent != "pane-b preview" {
		t.Fatalf("stale preview overwrote selection: target=%q content=%q", um.previewTarget, um.previewContent)
	}

	// Same-seq capture for a non-selected pane is also ignored.
	updated, _ = um.Update(previewLoadedMsg{
		seq:      um.previewSeq,
		id:       "pane-a",
		target:   "%pane-a",
		revision: rev,
		content:  "stale pane-a preview",
	})
	um = updated.(model)
	if um.previewTarget != "%pane-b" || um.previewContent != "pane-b preview" {
		t.Fatalf("same-seq stale preview overwrote selection: target=%q content=%q", um.previewTarget, um.previewContent)
	}
}

func TestPreviewLoadedMsgIgnoresStalePaneWithDuplicateSessionID(t *testing.T) {
	sharedID := "01a03cb5-f691-7c0e-b6b3-38977963e573"
	sessions := []registry.Session{
		{
			ID:         sharedID,
			TmuxTarget: "%20",
			TmuxWindow: "2",
			TmuxPane:   "1",
			Status:     registry.StatusWorking,
			Title:      "pane one",
		},
		{
			ID:         sharedID,
			TmuxTarget: "%21",
			TmuxWindow: "2",
			TmuxPane:   "2",
			Status:     registry.StatusWorking,
			Title:      "pane two",
		},
	}
	for _, s := range sessions {
		invalidatePreviewCache(s.TmuxTarget)
	}

	m := testModel(sessions)
	m.width = 120
	m.height = 24
	m.syncInputWidth()
	m.selectedTarget = "%21"
	m.reconcileCursor()

	rev := previewRevision(sessions[1])
	setPreviewCache("%21", rev, "pane-two preview", nil)

	m.previewTarget = "%20"
	m.previewContent = "pane-one preview"
	m.previewSeq = 3

	cmd := (&m).schedulePreview()
	if cmd != nil {
		t.Fatal("expected cache hit to skip tmux fetch")
	}
	if m.previewTarget != "%21" || m.previewContent != "pane-two preview" {
		t.Fatalf("preview = target=%q content=%q, want %%21", m.previewTarget, m.previewContent)
	}

	updated, _ := m.Update(previewLoadedMsg{
		seq:      m.previewSeq,
		id:       sharedID,
		target:   "%20",
		revision: rev,
		content:  "stale pane-one preview",
	})
	um := updated.(model)
	if um.previewTarget != "%21" || um.previewContent != "pane-two preview" {
		t.Fatalf("duplicate-id stale preview overwrote selection: target=%q content=%q", um.previewTarget, um.previewContent)
	}
}
