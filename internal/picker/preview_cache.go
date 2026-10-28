package picker

import "sync"

type previewCacheEntry struct {
	key     uint64
	content string
}

// previewCache holds one capture for the selected target. A busy pane keeps
// printing while its agent status stays "working", so Status and ToolName
// cannot key the cache. The captured bytes key it instead: same bytes keep the
// cached render, new bytes replace it.
var previewCache struct {
	sync.Mutex
	target string
	entry  previewCacheEntry
	valid  bool
}

// previewContentKey hashes a capture with FNV-1a. Identical pane text gives the
// same key, so the render stays valid only while the text is unchanged.
func previewContentKey(content string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	key := uint64(offset64)
	for i := 0; i < len(content); i++ {
		key ^= uint64(content[i])
		key *= prime64
	}
	return key
}

// cachedPreview returns the stored capture of target. One target only, so the
// cache cannot grow across a long picker session.
func cachedPreview(target string) (content string, key uint64, ok bool) {
	if target == "" {
		return "", 0, false
	}
	previewCache.Lock()
	defer previewCache.Unlock()
	if !previewCache.valid || previewCache.target != target {
		return "", 0, false
	}
	return previewCache.entry.content, previewCache.entry.key, true
}

// storePreviewCapture replaces the cache entry and returns the content key.
func storePreviewCapture(target, content string) uint64 {
	key := previewContentKey(content)
	if target == "" {
		return key
	}
	previewCache.Lock()
	if previewCache.valid && previewCache.target == target && previewCache.entry.key == key {
		// Same bytes: the cached render still serves, nothing to store.
		previewCache.Unlock()
		return key
	}
	previewCache.target = target
	previewCache.entry = previewCacheEntry{key: key, content: content}
	previewCache.valid = true
	previewCache.Unlock()
	return key
}

func invalidatePreviewCache(target string) {
	if target == "" {
		return
	}
	previewCache.Lock()
	if previewCache.target == target {
		previewCache.target = ""
		previewCache.entry = previewCacheEntry{}
		previewCache.valid = false
	}
	previewCache.Unlock()
}
