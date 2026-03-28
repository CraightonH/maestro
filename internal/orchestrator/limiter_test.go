package orchestrator

import "testing"

type stubLimiter struct {
	acquireResults []bool
	acquireCalls   int
	releaseCalls   int
}

func (s *stubLimiter) TryAcquire() bool {
	s.acquireCalls++
	if len(s.acquireResults) == 0 {
		return true
	}
	idx := s.acquireCalls - 1
	if idx >= len(s.acquireResults) {
		return s.acquireResults[len(s.acquireResults)-1]
	}
	return s.acquireResults[idx]
}

func (s *stubLimiter) Release() {
	s.releaseCalls++
}

func TestSemaphoreLimiterTryAcquireAndRelease(t *testing.T) {
	limiter := newSemaphoreLimiter(2)
	if !limiter.TryAcquire() || !limiter.TryAcquire() {
		t.Fatal("expected first two acquires to succeed")
	}
	if limiter.TryAcquire() {
		t.Fatal("expected third acquire to fail at capacity")
	}
	limiter.Release()
	if !limiter.TryAcquire() {
		t.Fatal("expected acquire to succeed after release")
	}
}

func TestCompositeLimiterReleasesOnSecondAcquireFailure(t *testing.T) {
	first := &stubLimiter{acquireResults: []bool{true}}
	second := &stubLimiter{acquireResults: []bool{false}}

	limiter := newCompositeLimiter(first, second)
	if limiter.TryAcquire() {
		t.Fatal("expected composite acquire to fail")
	}
	if first.acquireCalls != 1 || second.acquireCalls != 1 {
		t.Fatalf("acquire calls = %d/%d, want 1/1", first.acquireCalls, second.acquireCalls)
	}
	if first.releaseCalls != 1 {
		t.Fatalf("first release calls = %d, want 1", first.releaseCalls)
	}
	if second.releaseCalls != 0 {
		t.Fatalf("second release calls = %d, want 0", second.releaseCalls)
	}
}

func TestCompositeLimiterReleaseReleasesAll(t *testing.T) {
	first := &stubLimiter{}
	second := &stubLimiter{}

	limiter := newCompositeLimiter(first, second)
	if !limiter.TryAcquire() {
		t.Fatal("expected composite acquire to succeed")
	}
	limiter.Release()

	if first.releaseCalls != 1 || second.releaseCalls != 1 {
		t.Fatalf("release calls = %d/%d, want 1/1", first.releaseCalls, second.releaseCalls)
	}
}
