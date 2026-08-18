package api

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPreviewCacheCoalescesAndEvicts(t *testing.T) {
	cache := newPreviewCache(5)
	var calls atomic.Int32
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			data, err := cache.getOrGenerate(context.Background(), "first", func() ([]byte, error) {
				calls.Add(1)
				return []byte("1234"), nil
			})
			if err != nil || string(data) != "1234" {
				t.Errorf("getOrGenerate() = %q, %v", data, err)
			}
		}()
	}
	wait.Wait()
	if calls.Load() != 1 {
		t.Fatalf("generator calls = %d, want 1", calls.Load())
	}

	if _, err := cache.getOrGenerate(context.Background(), "second", func() ([]byte, error) {
		return []byte("abc"), nil
	}); err != nil {
		t.Fatalf("second getOrGenerate() error = %v", err)
	}
	items, bytes := cache.size()
	if items != 1 || bytes != 3 {
		t.Fatalf("cache size = %d items and %d bytes", items, bytes)
	}
}
