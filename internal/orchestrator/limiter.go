package orchestrator

import "sync"

type dispatchLimiter interface {
	TryAcquire() (bool, string)
	Release()
}

type semaphoreLimiter struct {
	mu       sync.Mutex
	capacity int
	inUse    int
	label    string
}

type compositeLimiter struct {
	limiters []dispatchLimiter
}

func newSemaphoreLimiter(capacity int) *semaphoreLimiter {
	return newNamedSemaphoreLimiter(capacity, "limiter")
}

func newNamedSemaphoreLimiter(capacity int, label string) *semaphoreLimiter {
	if capacity < 1 {
		capacity = 1
	}
	return &semaphoreLimiter{capacity: capacity, label: label}
}

func newCompositeLimiter(limiters ...dispatchLimiter) dispatchLimiter {
	filtered := make([]dispatchLimiter, 0, len(limiters))
	for _, limiter := range limiters {
		if limiter != nil {
			filtered = append(filtered, limiter)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return &compositeLimiter{limiters: filtered}
}

func (l *semaphoreLimiter) TryAcquire() (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inUse >= l.capacity {
		return false, l.label
	}
	l.inUse++
	return true, ""
}

func (l *semaphoreLimiter) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inUse > 0 {
		l.inUse--
	}
}

func (l *semaphoreLimiter) SetCapacity(capacity int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if capacity < 1 {
		capacity = 1
	}
	l.capacity = capacity
}

func (l *compositeLimiter) TryAcquire() (bool, string) {
	acquired := make([]dispatchLimiter, 0, len(l.limiters))
	for _, limiter := range l.limiters {
		acquiredOK, blockedBy := limiter.TryAcquire()
		if acquiredOK {
			acquired = append(acquired, limiter)
			continue
		}
		for i := len(acquired) - 1; i >= 0; i-- {
			acquired[i].Release()
		}
		if blockedBy == "" {
			blockedBy = "limiter"
		}
		return false, blockedBy
	}
	return true, ""
}

func (l *compositeLimiter) Release() {
	for i := len(l.limiters) - 1; i >= 0; i-- {
		l.limiters[i].Release()
	}
}
