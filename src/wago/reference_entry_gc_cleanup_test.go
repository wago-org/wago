//go:build linux && !tinygo && !wago_target_tinygo && (!wago_no_gc || (riscv64 && !wago_no_exception_handling))

package wago

import (
	"reflect"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestReferenceEntryCleanupCollectorFailureCannotStrandBatch(t *testing.T) {
	first, later := new(int), new(int)
	var events []int
	owner := cleanupTestRootedProducer(t, func() { events = append(events, 1); panic(later) })
	initial, last := &gc.Collector{}, &gc.Collector{}
	// The diagnostic close failure is injected before real Close. The fixture
	// explicitly owns both nil-storage collectors until these cleanup defers.
	defer initial.Close()
	defer last.Close()
	batch := referenceTokenEntries{
		funcrefs:       []*funcrefTokenEntry{{token: 1, owner: owner}},
		gcReleaseBatch: gcReleaseBatch{collectors: []*gc.Collector{last}},
	}
	value, panicked := cleanupTestPanic(func() {
		releaseLifetimeEntriesWith(initial, batch, func(collector *gc.Collector) {
			if collector == initial {
				events = append(events, 0)
				panic(first)
			}
			if collector != last {
				panic("unexpected collector unit")
			}
			events = append(events, 2)
			collector.Close()
		})
	})
	if !panicked || value != first || !reflect.DeepEqual(events, []int{0, 1, 2}) {
		t.Fatalf("panic=%v/%v events=%v", value, panicked, events)
	}
	cleanupTestConsumedProducer(t, owner)
	if batch.funcrefs[0] != nil || batch.collectors[0] != nil {
		t.Fatal("enclosing failure left the claimed batch unpublished but undrained")
	}
}
