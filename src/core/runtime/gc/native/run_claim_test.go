package gc

import (
	"fmt"
	"testing"
)

func TestRunClaimsShareAsymmetricCapacity(t *testing.T) {
	for workers := 2; workers <= 8; workers++ {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			const objectSize = uint32(64)
			const objects = 1024
			runs := make([]throughputFreeSpan, 0, 2*workers+4)
			var off uint32
			for i := 0; i < workers; i++ {
				size := uint32(32768)
				if i%2 == 0 {
					size = 128
				}
				runs = append(runs, throughputFreeSpan{off: off, size: size})
				off += 32768
			}
			// Admission includes one whole block of slack per worker. Supply that
			// slack after the deliberately asymmetric first claims.
			for i := 0; i < workers+3; i++ {
				runs = append(runs, throughputFreeSpan{off: off, size: 32768})
				off += 32768
			}
			admitted, credit, need, err := planEvacuationRuns(runs, nil, uint64(objects*objectSize), objectSize, workers, evacuationLimit)
			if err != nil || credit < need {
				t.Fatalf("fixture was not admitted: credit=%d need=%d err=%v", credit, need, err)
			}
			claims := newRunClaimPlanner(admitted, workers)
			occupied := make(map[uint32]bool, objects)
			for i := 0; i < objects; i++ {
				dst, ok := claims.destination(i%workers, objectSize)
				if !ok {
					t.Fatalf("admitted asymmetric capacity refused object %d", i)
				}
				if dst%16 != 0 || occupied[dst] {
					t.Fatalf("unaligned or duplicate destination %#x", dst)
				}
				occupied[dst] = true
			}
		})
	}
}

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
