package api

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	defaultConcurrentWorkLimit = 2
	defaultWorkWaitTimeout     = 2 * time.Second
)

var (
	diffWorkSlots    = newConcurrentWorkLimiter(defaultConcurrentWorkLimit, defaultWorkWaitTimeout)
	formatWorkSlots  = newConcurrentWorkLimiter(defaultConcurrentWorkLimit, defaultWorkWaitTimeout)
	previewWorkSlots = newConcurrentWorkLimiter(defaultConcurrentWorkLimit, defaultWorkWaitTimeout)
)

// WorkLimitConfig controls the number of concurrent expensive operations.
type WorkLimitConfig struct {
	DiffLimit    int
	FormatLimit  int
	PreviewLimit int
	WaitTimeout  time.Duration
}

// WorkLimitSnapshot reports the current work limit and active operation count.
type WorkLimitSnapshot struct {
	Limit  int
	Active int
}

// DefaultWorkLimitConfig returns limits suitable for local development.
func DefaultWorkLimitConfig() WorkLimitConfig {
	return WorkLimitConfig{
		DiffLimit:    defaultConcurrentWorkLimit,
		FormatLimit:  defaultConcurrentWorkLimit,
		PreviewLimit: defaultConcurrentWorkLimit,
		WaitTimeout:  defaultWorkWaitTimeout,
	}
}

// ConfigureWorkLimits updates all expensive-operation limits safely.
// Callers can update these limits while requests are active.
func ConfigureWorkLimits(config WorkLimitConfig) error {
	if config.DiffLimit < 1 || config.FormatLimit < 1 || config.PreviewLimit < 1 {
		return errors.New("work limits must be greater than zero")
	}
	if config.WaitTimeout < 0 {
		return errors.New("work wait timeout cannot be negative")
	}

	diffWorkSlots.configure(config.DiffLimit, config.WaitTimeout)
	formatWorkSlots.configure(config.FormatLimit, config.WaitTimeout)
	previewWorkSlots.configure(config.PreviewLimit, config.WaitTimeout)
	return nil
}

// WorkLimitStats returns snapshots for operational diagnostics.
func WorkLimitStats() map[string]WorkLimitSnapshot {
	return map[string]WorkLimitSnapshot{
		"diff":    diffWorkSlots.snapshot(),
		"format":  formatWorkSlots.snapshot(),
		"preview": previewWorkSlots.snapshot(),
	}
}

type concurrentWorkLimiter struct {
	mu          sync.Mutex
	limit       int
	active      int
	waitTimeout time.Duration
	notify      chan struct{}
}

func newConcurrentWorkLimiter(limit int, waitTimeout time.Duration) *concurrentWorkLimiter {
	return &concurrentWorkLimiter{
		limit:       limit,
		waitTimeout: waitTimeout,
		notify:      make(chan struct{}),
	}
}

func (l *concurrentWorkLimiter) configure(limit int, waitTimeout time.Duration) {
	l.mu.Lock()
	l.limit = limit
	l.waitTimeout = waitTimeout
	l.broadcastLocked()
	l.mu.Unlock()
}

func (l *concurrentWorkLimiter) acquire(ctx context.Context) bool {
	l.mu.Lock()
	waitTimeout := l.waitTimeout
	l.mu.Unlock()

	timer := time.NewTimer(waitTimeout)
	defer timer.Stop()

	for {
		if ctx.Err() != nil {
			return false
		}
		l.mu.Lock()
		if l.active < l.limit {
			l.active++
			l.mu.Unlock()
			return true
		}
		notify := l.notify
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-notify:
		}
	}
}

func (l *concurrentWorkLimiter) release() {
	l.mu.Lock()
	if l.active > 0 {
		l.active--
	}
	l.broadcastLocked()
	l.mu.Unlock()
}

func (l *concurrentWorkLimiter) snapshot() WorkLimitSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return WorkLimitSnapshot{Limit: l.limit, Active: l.active}
}

func (l *concurrentWorkLimiter) broadcastLocked() {
	close(l.notify)
	l.notify = make(chan struct{})
}

// acquireWorkSlot waits for capacity for an expensive operation.
func acquireWorkSlot(ctx context.Context, limiter *concurrentWorkLimiter) bool {
	return limiter.acquire(ctx)
}

// releaseWorkSlot returns capacity for an expensive operation.
func releaseWorkSlot(limiter *concurrentWorkLimiter) {
	limiter.release()
}
