package wago

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestMemoryWaitMismatchAndZeroTimeoutDoNotRetainState(t *testing.T) {
	m, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&m.UnsafeBytes()[0])), 7)
	if got, err := m.wait32(context.Background(), 0, 8, -1); err != nil || got != memoryWaitNotEqual {
		t.Fatalf("mismatch = %d, %v", got, err)
	}
	if got, err := m.wait32(context.Background(), 0, 7, 0); err != nil || got != memoryWaitTimedOut {
		t.Fatalf("zero timeout = %d, %v", got, err)
	}
	assertNoMemoryWaiters(t, m)
}

func TestMemoryNotifyUsesIdentityOffsetAndCount(t *testing.T) {
	a, _ := NewSharedMemory(1, 1)
	b, _ := NewSharedMemory(1, 1)
	defer a.Close()
	defer b.Close()

	type result struct {
		code uint32
		err  error
	}
	waits := make([]chan result, 3)
	for i, tc := range []struct {
		m      *Memory
		offset uint64
	}{{a, 0}, {a, 0}, {b, 0}} {
		waits[i] = make(chan result, 1)
		go func(out chan result, m *Memory, offset uint64) {
			code, err := m.wait32(context.Background(), offset, 0, -1)
			out <- result{code, err}
		}(waits[i], tc.m, tc.offset)
	}
	waitForMemoryWaiters(t, a, 2)
	waitForMemoryWaiters(t, b, 1)
	if got, err := a.notify(4, 10); err != nil || got != 0 {
		t.Fatalf("wrong offset notify = %d, %v", got, err)
	}
	if got, err := a.notify(0, 1); err != nil || got != 1 {
		t.Fatalf("counted notify = %d, %v", got, err)
	}
	select {
	case got := <-waits[0]:
		if got.code != memoryWaitNotified || got.err != nil {
			t.Fatalf("first waiter = %+v", got)
		}
	case got := <-waits[1]:
		if got.code != memoryWaitNotified || got.err != nil {
			t.Fatalf("second waiter = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("notify did not wake one waiter")
	}
	if got, _ := a.notify(0, ^uint32(0)); got != 1 {
		t.Fatalf("remaining notify = %d", got)
	}
	if got, _ := b.notify(0, ^uint32(0)); got != 1 {
		t.Fatalf("identity notify = %d", got)
	}
}

func TestMemoryWaitTimeoutCancelAndCloseReclaimState(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		m, _ := NewSharedMemory(1, 1)
		defer m.Close()
		if got, err := m.wait32(context.Background(), 0, 0, int64(time.Millisecond)); err != nil || got != memoryWaitTimedOut {
			t.Fatalf("wait = %d, %v", got, err)
		}
		assertNoMemoryWaiters(t, m)
	})
	t.Run("cancel", func(t *testing.T) {
		m, _ := NewSharedMemory(1, 1)
		defer m.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := m.wait64(ctx, 0, 0, -1); !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error = %v", err)
		}
		assertNoMemoryWaiters(t, m)
	})
	t.Run("close", func(t *testing.T) {
		m, _ := NewSharedMemory(1, 1)
		out := make(chan error, 1)
		go func() { _, err := m.wait32(context.Background(), 0, 0, -1); out <- err }()
		waitForMemoryWaiters(t, m, 1)
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-out:
			if !errors.Is(err, errMemoryWaitClosed) {
				t.Fatalf("wait error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("close did not wake waiter")
		}
		assertNoMemoryWaiters(t, m)
	})
}

func waitForMemoryWaiters(t *testing.T, m *Memory, want uint32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s := m.state.Load()
		s.mu.Lock()
		got := uint32(0)
		if ws := s.waiterStateLocked(false); ws != nil {
			got = ws.active
		}
		s.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("active waiter count did not reach %d", want)
}

func assertNoMemoryWaiters(t *testing.T, m *Memory) {
	t.Helper()
	s := m.state.Load()
	s.mu.Lock()
	defer s.mu.Unlock()
	if ws := s.waiterStateLocked(false); ws != nil {
		t.Fatalf("retained waiter state: %+v", ws)
	}
}
