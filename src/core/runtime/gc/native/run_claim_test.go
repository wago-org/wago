package gc

import "testing"

func TestRunClaimsRespectCapacityAndTailBounds(t *testing.T) {
	runs := []throughputFreeSpan{{off: 16, size: 80}, {off: 32768, size: 128}, {off: 65536, size: 128}}
	claims := newRunClaimPlanner(runs, 2)
	for i, want := range []uint32{16, 32768, 65536, 32832, 65600} {
		got, ok := claims.destination(i%2, 64)
		if !ok || got != want {
			t.Fatalf("destination %d = %#x/%t, want %#x", i, got, ok, want)
		}
	}
	if _, ok := claims.destination(0, 64); ok {
		t.Fatal("truly insufficient capacity was accepted")
	}
}
