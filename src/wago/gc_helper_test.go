package wago

import "testing"

func TestGCHelperDispatcherFamilies(t *testing.T) {
	for _, helper := range []uint32{
		gcStructAllocDefault,
		gcStructReserveDead,
		gcStructSetNoBarrier,
	} {
		if !gcHelperUsesStructDispatcher(helper) {
			t.Fatalf("helper %d classified as array, want struct", helper)
		}
	}
	for _, helper := range []uint32{
		gcArrayAllocDefault,
		gcArrayCheckFixed,
		gcArraySetNoBarrier,
	} {
		if gcHelperUsesStructDispatcher(helper) {
			t.Fatalf("helper %d classified as struct, want array", helper)
		}
	}
}
