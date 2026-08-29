//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"errors"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestProcessNativeMemoryStatsTracksLifecycle(t *testing.T) {
	before := ProcessNativeMemoryStats()
	if !before.Supported || before.Capacity != maxInterruptLinearMemories {
		t.Fatalf("initial native memory stats = %#v", before)
	}
	jm, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	during := ProcessNativeMemoryStats()
	if during.Registered != before.Registered+1 || during.Active != before.Active+1 || during.Cached != before.Cached {
		_ = jm.Close()
		t.Fatalf("registered native memory stats = %#v, before %#v", during, before)
	}
	if during.PeakRegistered < during.Registered || during.ScanSpan == 0 {
		_ = jm.Close()
		t.Fatalf("registered native memory peak/scan stats = %#v", during)
	}
	if err := jm.Close(); err != nil {
		t.Fatal(err)
	}
	after := ProcessNativeMemoryStats()
	if after.Registered != before.Registered || after.Active != before.Active || after.Cached != before.Cached {
		t.Fatalf("closed native memory stats = %#v, before %#v", after, before)
	}
}

func TestProcessNativeMemoryStatsTracksCacheReuse(t *testing.T) {
	jm, err := AcquireJobMemoryGrowable(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	active := ProcessNativeMemoryStats()
	if err := ReleaseJobMemory(jm); err != nil {
		t.Fatal(err)
	}
	cached := ProcessNativeMemoryStats()
	if cached.Registered != active.Registered || cached.Active+1 != active.Active || cached.Cached != active.Cached+1 {
		t.Fatalf("cache transition = active %#v cached %#v", active, cached)
	}
	reused, err := AcquireJobMemoryGrowable(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	reactivated := ProcessNativeMemoryStats()
	if reactivated.Registered != cached.Registered || reactivated.Active != cached.Active+1 || reactivated.Cached+1 != cached.Cached {
		_ = ReleaseJobMemory(reused)
		t.Fatalf("reuse transition = cached %#v active %#v", cached, reactivated)
	}
	if err := ReleaseJobMemory(reused); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptLinearMemoryCapacityReturnsTypedError(t *testing.T) {
	var registered []uintptr
	defer func() {
		for index := len(registered) - 1; index >= 0; index-- {
			unregisterInterruptLinearMemory(registered[index])
		}
	}()
	for candidate := uintptr(1); ProcessNativeMemoryStats().Registered < maxInterruptLinearMemories; candidate++ {
		before := ProcessNativeMemoryStats().Registered
		if err := registerInterruptLinearMemory(candidate); err != nil {
			t.Fatalf("fill native memory registry at %d entries: %v", before, err)
		}
		if ProcessNativeMemoryStats().Registered != before {
			registered = append(registered, candidate)
		}
	}
	err := registerInterruptLinearMemory(^uintptr(0))
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("capacity error = %v, want ErrResourceLimit", err)
	}
	var limitErr *ResourceLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("capacity error type = %T, want *ResourceLimitError", err)
	}
	if limitErr.Scope != "process" || limitErr.Resource != "native memory mappings" || limitErr.Used != maxInterruptLinearMemories || limitErr.Requested != 1 || limitErr.Limit != maxInterruptLinearMemories {
		t.Fatalf("capacity error fields = %#v", limitErr)
	}
}

func TestJobMemoryLifecycleRegistersInterruptLinearMemory(t *testing.T) {
	jm, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	base := jm.LinMemBase()
	if !interruptLinearMemoryRegistered(base) {
		t.Fatal("created linear memory was not published to interrupt handler")
	}
	if err := jm.Close(); err != nil {
		t.Fatal(err)
	}
	if interruptLinearMemoryRegistered(base) {
		t.Fatal("closed linear memory remained published")
	}
}

func TestUnregisterWaitsForSignalReaders(t *testing.T) {
	const base = uintptr(3)
	if err := registerInterruptLinearMemory(base); err != nil {
		t.Fatal(err)
	}
	// Model a handler that acquired its reader slot before Close took the gate.
	atomic.StoreUint32(&interruptLinearMemoryState, 1)
	done := make(chan struct{})
	go func() {
		unregisterInterruptLinearMemory(base)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for atomic.LoadUint32(&interruptLinearMemoryState)&interruptLinearMemoryWriter == 0 && time.Now().Before(deadline) {
		goruntime.Gosched()
	}
	if atomic.LoadUint32(&interruptLinearMemoryState)&interruptLinearMemoryWriter == 0 {
		atomic.AddUint32(&interruptLinearMemoryState, ^uint32(0))
		<-done
		t.Fatal("unregister did not close the signal-reader gate")
	}
	select {
	case <-done:
		t.Fatal("unregister returned while a signal reader was active")
	default:
	}
	atomic.AddUint32(&interruptLinearMemoryState, ^uint32(0))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unregister did not resume after the signal reader left")
	}
	if interruptLinearMemoryRegistered(base) {
		t.Fatal("base remained registered after reader quiescence")
	}
}

func TestInterruptLinearMemoryRegistrationScansPastHoles(t *testing.T) {
	const first, second = uintptr(1), uintptr(2)
	if err := registerInterruptLinearMemory(first); err != nil {
		t.Fatal(err)
	}
	if err := registerInterruptLinearMemory(second); err != nil {
		t.Fatal(err)
	}
	unregisterInterruptLinearMemory(first)
	if err := registerInterruptLinearMemory(second); err != nil {
		t.Fatal(err)
	}
	count := 0
	for i := uint32(0); i < atomic.LoadUint32(&interruptLinearMemoryLimit); i++ {
		if atomic.LoadUintptr(&interruptLinearMemories[i]) == second {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("second base registrations = %d, want 1", count)
	}
	unregisterInterruptLinearMemory(second)
	if interruptLinearMemoryRegistered(second) {
		t.Fatal("second base remained registered")
	}
}

func interruptLinearMemoryRegistered(base uintptr) bool {
	for i := uint32(0); i < atomic.LoadUint32(&interruptLinearMemoryLimit); i++ {
		if atomic.LoadUintptr(&interruptLinearMemories[i]) == base {
			return true
		}
	}
	return false
}
