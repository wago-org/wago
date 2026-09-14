package wago

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestPreparedLeaseOwnerTracksMigration(t *testing.T) {
	var mu sync.Mutex
	var migrated atomic.Bool
	in := &Instance{}
	in.executionFlags.Store(executionFlagIndependent | executionFlagNativeControlShared)
	a := hostLoopActivation{root: in, entryNativeMu: &mu, preparedMigration: &migrated}
	if a.localNativeMu() != &mu {
		t.Fatal("pending publication changed the held lease owner")
	}
	migrated.Store(true)
	if a.localNativeMu() != nil {
		t.Fatal("completed migration retained the local owner")
	}
}

func TestPreparedLeaseEndCallRetiresRevokedOwner(t *testing.T) {
	in := &Instance{}
	pluginState := in.ensurePluginState()
	var mu sync.Mutex
	mu.Lock()
	state := preparedSessionState{
		fn: &PreparedFunction{in: in}, host: true, guardCalls: true,
		entry: executionLease{local: &mu},
	}
	state.active.Store(true)
	state.hostGate.active = &state.active
	pluginState.preparedHostGate = &state.hostGate
	in.executionFlags.Store(executionFlagIndependent | executionFlagNativeControlShared)
	state.endCall(gcInvocationLease{})
	if state.active.Load() || state.host || pluginState.preparedHostGate != nil {
		t.Fatal("call end retained a revoked publication gate")
	}
	if !mu.TryLock() {
		t.Fatal("call end retained the local mutex")
	}
	mu.Unlock()
}

func TestNativeLeaseMigrationInvalidatesSharedContext(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		in := &Instance{c: &Compiled{}}
		var mu sync.Mutex
		var migrated atomic.Bool
		a := hostLoopActivation{root: in, entryNativeMu: &mu, preparedMigration: &migrated}
		nativeExecutionMu.Lock()
		epoch := nativeExecutionEpoch
		nativeExecutionMu.Unlock()
		var changed bool
		if prepared {
			changed = a.reacquirePreparedRootNative(&mu)
		} else {
			changed = reacquireRootNative(in, &mu)
		}
		gotEpoch := nativeExecutionEpoch
		nativeExecutionMu.Unlock()
		if !changed || gotEpoch != epoch+1 {
			t.Fatalf("prepared=%v: migration=%v, epoch=%d, want %d", prepared, changed, gotEpoch, epoch+1)
		}
		if prepared && (a.entryNativeMu != nil || !migrated.Load()) {
			t.Fatal("prepared migration did not record its shared owner")
		}
		if !mu.TryLock() {
			t.Fatal("migration retained the local mutex")
		}
		mu.Unlock()
	}
}
