package orchestrator

import "testing"

type stubLimiter struct {
	acquireResults []bool
	blockedBy      string
	acquireCalls   int
	releaseCalls   int
}

func (s *stubLimiter) TryAcquire() (bool, string) {
	s.acquireCalls++
	if len(s.acquireResults) == 0 {
		return true, ""
	}
	idx := s.acquireCalls - 1
	if idx >= len(s.acquireResults) {
		ok := s.acquireResults[len(s.acquireResults)-1]
		if ok {
			return true, ""
		}
		return false, s.blockedBy
	}
	ok := s.acquireResults[idx]
	if ok {
		return true, ""
	}
	return false, s.blockedBy
}

func (s *stubLimiter) Release() {
	s.releaseCalls++
}

func TestSemaphoreLimiterTryAcquireAndRelease(t *testing.T) {
	limiter := newSemaphoreLimiter(2)
	if ok, _ := limiter.TryAcquire(); !ok {
		t.Fatal("expected first acquire to succeed")
	}
	if ok, _ := limiter.TryAcquire(); !ok {
		t.Fatal("expected first two acquires to succeed")
	}
	if ok, blockedBy := limiter.TryAcquire(); ok || blockedBy != "limiter" {
		t.Fatal("expected third acquire to fail at capacity")
	}
	limiter.Release()
	if ok, _ := limiter.TryAcquire(); !ok {
		t.Fatal("expected acquire to succeed after release")
	}
}

func TestCompositeLimiterReleasesOnSecondAcquireFailure(t *testing.T) {
	first := &stubLimiter{acquireResults: []bool{true}}
	second := &stubLimiter{acquireResults: []bool{false}, blockedBy: "agent"}

	limiter := newCompositeLimiter(first, second)
	if ok, blockedBy := limiter.TryAcquire(); ok || blockedBy != "agent" {
		t.Fatalf("expected composite acquire to fail with agent blocker, got ok=%v blockedBy=%q", ok, blockedBy)
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
	if ok, _ := limiter.TryAcquire(); !ok {
		t.Fatal("expected composite acquire to succeed")
	}
	limiter.Release()

	if first.releaseCalls != 1 || second.releaseCalls != 1 {
		t.Fatalf("release calls = %d/%d, want 1/1", first.releaseCalls, second.releaseCalls)
	}
}
