//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import "testing"

// This compile-only table isolation proof is available on the existing targets;
// it is deliberately absent from codec metadata and RV64 admission.
func TestPreparedCleanupImmutableTableReservation(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), callIndirectModule(2, 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !c.preparedIsolatedTables {
		t.Fatal("fixture lacks the actual immutable local-table proof")
	}
	in := preparedCleanupInstantiate(t, c, NewImports())
	if !in.preparedIsolatedEligible() {
		t.Fatal("immutable unexported table lost isolated admission")
	}
	fn, err := in.WasmFunc("caller")
	if err != nil {
		t.Fatal(err)
	}
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	isolated := preparedCallEnabled && preparedScalarFastEnabled && preparedPrivateEntryEnabled && preparedIsolatedEntryEnabled && !(codeProfileEnabled && in.boundaryProfile() != nil)
	if isolated && (!fn.isolatedFast || !s.state.fast || s.state.guardCalls) {
		t.Fatal("eligible immutable-table fast session gained an active-call guard")
	}
	for _, want := range []struct{ index, result uint64 }{{0, 13}, {1, 7}} {
		got, err := s.Invoke3(want.index, 10, 3)
		if err != nil || len(got) != 1 || got[0] != want.result {
			t.Fatal("actual immutable-table prepared result", got, err)
		}
	}
	s.Close()
	if !preparedCleanupReservationReleased(in) {
		t.Fatal("immutable-table session retained invocation authority")
	}
}
