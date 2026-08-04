package wago

import "testing"

func TestInternalGCHelperKeepsNativeExecutionLease(t *testing.T) {
	in := &Instance{ctrl: make([]byte, 64)}
	ctrl := offHeapSlicePtr(in.ctrl)
	var helperObservedUnlocked bool
	in.hostCall = func(uintptr, uint32, []uint64, []uint64) {
		if nativeExecutionMu.TryLock() {
			helperObservedUnlocked = true
			nativeExecutionMu.Unlock()
		}
	}

	nativeExecutionMu.Lock()
	in.dispatchSynchronousHostCall(ctrl, gcStructDispatchBit|gcStructGet, nil, nil)
	nativeExecutionMu.Unlock()
	if helperObservedUnlocked {
		t.Fatal("internal GC helper released the native execution lease")
	}
}

func TestOrdinaryHostCallReleasesNativeExecutionLease(t *testing.T) {
	in := &Instance{ctrl: make([]byte, 64)}
	ctrl := offHeapSlicePtr(in.ctrl)
	var hostObservedUnlocked bool
	in.hostCall = func(uintptr, uint32, []uint64, []uint64) {
		if nativeExecutionMu.TryLock() {
			hostObservedUnlocked = true
			nativeExecutionMu.Unlock()
		}
	}

	nativeExecutionMu.Lock()
	in.dispatchSynchronousHostCall(ctrl, 0, nil, nil)
	nativeExecutionMu.Unlock()
	if !hostObservedUnlocked {
		t.Fatal("ordinary host call retained the native execution lease")
	}
}
