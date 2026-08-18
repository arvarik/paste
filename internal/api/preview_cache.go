package api

import (
	"container/list"
	"context"
	"sync"
	"sync/atomic"
)

type previewCacheEntry struct {
	key  string
	data []byte
}

type previewCacheCall struct {
	done chan struct{}
	data []byte
	err  error
}

type previewCache struct {
	mu       sync.Mutex
	maxBytes int64
	used     int64
	entries  map[string]*list.Element
	lru      *list.List
	inflight map[string]*previewCacheCall
}

var generatedPreviews atomic.Pointer[previewCache]

func init() {
	generatedPreviews.Store(newPreviewCache(64 << 20))
}

func newPreviewCache(maxBytes int64) *previewCache {
	return &previewCache{
		maxBytes: maxBytes,
		entries:  make(map[string]*list.Element),
		lru:      list.New(),
		inflight: make(map[string]*previewCacheCall),
	}
}

// ConfigurePreviewCache replaces the generated image cache.
func ConfigurePreviewCache(maxBytes int64) {
	if maxBytes < 1 {
		maxBytes = 1
	}
	generatedPreviews.Store(newPreviewCache(maxBytes))
}

func (c *previewCache) getOrGenerate(ctx context.Context, key string, generate func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	if element, ok := c.entries[key]; ok {
		c.lru.MoveToFront(element)
		data := element.Value.(previewCacheEntry).data
		c.mu.Unlock()
		return data, nil
	}
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			return call.data, call.err
		}
	}
	call := &previewCacheCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	data, err := generate()

	c.mu.Lock()
	call.data = data
	call.err = err
	delete(c.inflight, key)
	if err == nil && int64(len(data)) <= c.maxBytes {
		copyOfData := append([]byte(nil), data...)
		element := c.lru.PushFront(previewCacheEntry{key: key, data: copyOfData})
		c.entries[key] = element
		c.used += int64(len(copyOfData))
		for c.used > c.maxBytes && c.lru.Len() > 0 {
			oldest := c.lru.Back()
			entry := oldest.Value.(previewCacheEntry)
			delete(c.entries, entry.key)
			c.used -= int64(len(entry.data))
			c.lru.Remove(oldest)
		}
	}
	close(call.done)
	c.mu.Unlock()
	return data, err
}

func (c *previewCache) size() (items int, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries), c.used
}
