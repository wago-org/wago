//go:build linux && (amd64 || arm64) && !tinygo

package wago

import "testing"

// An initially domain-free session must observe newly reachable GC topology on
// its next call, even if that topology currently contains no collector domains.
func TestPreparedSessionGCAdmissionRefresh(t *testing.T) {
	in := &Instance{refStore: &referenceStore{gcDomains: &gcDomainTopology{}}}
	state := &preparedSessionState{
		fn: &WasmFunc{in: in}, guardCalls: true,
		lease: preparedInvocationLease{state: &instancePluginState{invocationID: newInvocationID()}},
	}
	var lease gcInvocationLease
	if err := state.beginCall(&lease); err != nil {
		t.Fatal(err)
	}
	if lease.acquired || !state.active.Load() {
		t.Fatal("domain-free call did not preserve its active guard")
	}
	var rejected gcInvocationLease
	if err := state.beginCall(&rejected); err == nil {
		t.Fatal("active session admitted reentry")
	}
	state.endCall(&lease)
	in.executionFlags.Store(executionFlagDynamicGCDomain)
	lease = gcInvocationLease{}
	if err := state.beginCall(&lease); err != nil {
		t.Fatal(err)
	}
	if !lease.acquired || !lease.dynamic || lease.topology != in.refStore.gcDomains {
		state.endCall(&lease)
		t.Fatal("session omitted the newly reachable topology lease")
	}
	state.endCall(&lease)
	if state.active.Load() {
		t.Fatal("session retained its active guard after the GC call")
	}
}
