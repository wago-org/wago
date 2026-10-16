//go:build wagodebug

package gc

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
)

func TestNurseryGrowthWaitsForSuccessfulPromotion(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("automatic nursery sizing is measured on AMD64")
	}
	for _, point := range []failurePoint{failPromotionPlan, failPromotionDestination, failPromotionCommit} {
		t.Run(fmt.Sprint(point), func(t *testing.T) {
			c, err := NewCollector(Config{}, testTypes(t))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			roots := make([]Root, 512)
			for i := range roots {
				ref, err := c.NewArrayDefault(2, 16)
				if err != nil {
					t.Fatal(err)
				}
				roots[i] = Root(ref)
			}
			c.nurseryGrowthSurvived = defaultNursery / 2
			c.nurseryGrowthQuiet = 3
			c.tenuringThreshold = 1
			before := snapshotPromotionState(c)
			backing := sliceData(c.nursery)
			capacity := c.cfg.NurseryBytes
			cleanup := armFailure(c, point, 0)
			err = c.CollectMinor(stressRootSlots(roots))
			cleanup()
			if !errors.Is(err, errInjectedFailure) {
				t.Fatalf("promotion error = %v, want injected failure", err)
			}
			if c.cfg.NurseryBytes != capacity || sliceData(c.nursery) != backing || c.nurseryGrowthSurvived != defaultNursery/2 || c.nurseryGrowthQuiet != 3 {
				t.Fatal("failed promotion changed nursery growth state")
			}
			assertPromotionStateEqual(t, c, before)
		})
	}
}
