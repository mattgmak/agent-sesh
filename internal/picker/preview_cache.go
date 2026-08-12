package picker

import (
	"sync"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

type previewCacheEntry struct {
	revision string
	content  string
	err      error
}

var previewCache sync.Map // tmux target -> previewCacheEntry

// previewRevision keys cached pane captures; refresh when agent activity status changes.
func previewRevision(session registry.Session) string {
	return string(session.Status) + "\x00" + session.ToolName
}

func getPreviewCache(target, revision string) (content string, err error, ok bool) {
	if target == "" || revision == "" {
		return "", nil, false
	}
	raw, ok := previewCache.Load(target)
	if !ok {
		return "", nil, false
	}
	entry := raw.(previewCacheEntry)
	if entry.revision != revision {
		return "", nil, false
	}
	return entry.content, entry.err, true
}

func getPreviewCacheAny(target string) (content string, revision string, ok bool, err error) {
	if target == "" {
		return "", "", false, nil
	}
	raw, ok := previewCache.Load(target)
	if !ok {
		return "", "", false, nil
	}
	entry := raw.(previewCacheEntry)
	return entry.content, entry.revision, true, entry.err
}

func setPreviewCache(target, revision, content string, err error) {
	if target == "" || revision == "" {
		return
	}
	previewCache.Store(target, previewCacheEntry{
		revision: revision,
		content:  content,
		err:      err,
	})
}

func invalidatePreviewCache(target string) {
	if target == "" {
		return
	}
	previewCache.Delete(target)
}
