//go:build wagodebug

package gc

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestRollupEvacuationFailureKeepsCards(t *testing.T) {
	for _, shift := range []uint8{0, 6, 12} {
		for _, point := range []failurePoint{failPromotionDestination, failPromotionCommit} {
			t.Run(fmt.Sprintf("shift=%d/point=%d", shift, point), func(t *testing.T) {
				c := newTestCollector(t, Config{NurseryBytes: 16384, NurseryRegionShift: shift, ThroughputHeapBytes: 1 << 20, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
				parent := regionStruct(t, c, 1, EmptyRoots{})
				roots := RefSliceRoots{parent}
				if err := c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				child := regionStruct(t, c, 0, roots)
				if err := c.StructSet(child, 1, I64Value(42)); err != nil {
					t.Fatal(err)
				}
				if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
					t.Fatal(err)
				}
				slotChild := regionStruct(t, c, 0, roots)
				for i := 0; i <= 4096; i++ {
					c.NewGlobalSlot(Null())
					c.NewTableSlot(Null())
				}
				if err := c.SetGlobalSlot(4096, slotChild); err != nil {
					t.Fatal(err)
				}
				if err := c.SetTableSlot(64, slotChild); err != nil {
					t.Fatal(err)
				}
				entries := append([]handleEntry(nil), c.handles...)
				source := append([]byte(nil), c.nursery...)
				remembered := append([]uint32(nil), c.remembered...)
				cleanup := armFailure(c, point, 1)
				err := c.CollectMinor(roots)
				cleanup()
				if !errors.Is(err, errInjectedFailure) {
					t.Fatal("missing failure", err)
				}
				if !reflect.DeepEqual(entries, c.handles) || !reflect.DeepEqual(source, c.nursery) || !reflect.DeepEqual(remembered, c.remembered) {
					t.Fatal("failed evacuation changed source/remembered owners")
				}
				if c.CardCount() == 0 || c.entry(parent).cardSlot == 0 {
					t.Fatal("failure cleared cards")
				}
				if shift != 0 {
					stage5Occupancy(t, &c.throughput, 32, 0)
				}
				if err = c.Verify(roots); err != nil {
					t.Fatal(err)
				}
				if err = c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				if c.CardCount() != 0 || c.RememberedCount() != 0 || c.entry(parent).cardSlot != 0 {
					t.Fatal("successful evacuation retained cards")
				}
				if c.GlobalSlot(4096) != slotChild || c.entry(slotChild).young() {
					t.Fatal("lost persistent root")
				}
				v, err := c.StructGet(child, 1)
				if err != nil || v.I64() != 42 {
					t.Fatal("lost remembered child", err)
				}
				if shift != 0 {
					stage5Occupancy(t, &c.throughput, 96, 0)
				}
			})
		}
	}
}

func TestRollupLargeAdmissionRetry(t *testing.T) {
	for _, shift := range []uint8{6, 12} {
		for _, point := range []failurePoint{failMatureMetadata, failBackingGrowth, failHandlePublication} {
			t.Run(fmt.Sprintf("shift=%d/point=%d", shift, point), func(t *testing.T) {
				c := stage4Collector(t, shift)
				if !c.PrepareNativeAllocation(32) {
					t.Fatal("small reserve")
				}
				var target any = &c.throughput
				if point == failHandlePublication {
					target = c
				}
				cleanup := armFailure(target, point, 0)
				_, err := c.NewArrayDefault(2, 2048)
				cleanup()
				if !errors.Is(err, errInjectedFailure) {
					t.Fatal("missing failure", err)
				}
				if c.nativeStructAlloc.Count != 0 || c.throughput.bump != 0 || len(c.throughput.mem) != 0 || c.throughput.mature.admission {
					t.Fatal("failed LOS retained reservation")
				}
				stage5Occupancy(t, &c.throughput, 0, 0)
				large, err := c.NewArrayDefault(2, 2048)
				if err != nil {
					t.Fatal(err)
				}
				if c.entry(large).off != 0 {
					t.Fatal("retry lost extent")
				}
				stage5Occupancy(t, &c.throughput, 8208, 0)
				if err = c.Verify(RefSliceRoots{large}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
