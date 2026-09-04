package shared

import "testing"

func TestGCBarrierState(t *testing.T) {
	if GCBarrierNoBarrier.NeedsBarrier() {
		t.Fatal("no-barrier state requires a barrier")
	}
	for _, state := range []GCBarrierState{GCBarrierYoungParent, GCBarrierKnownOldChild, GCBarrierExistingCard, GCBarrierCardMark, GCBarrierSlowBarrier} {
		if !state.NeedsBarrier() {
			t.Fatalf("barrier state %d does not require a barrier", state)
		}
	}
}
