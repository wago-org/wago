//go:build linux && !tinygo && !wago_target_tinygo && (!wago_no_gc || (riscv64 && !wago_no_exception_handling))

package wago

import (
	"errors"
	"reflect"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestReferenceEntryCleanupGCSlotPanicUnlocksBeforeProducer(t *testing.T) {
	first := new(int)
	var state *gcPublicState
	var producerUnlocked bool
	owner := cleanupTestRootedProducer(t, func() {
		producerUnlocked = state.mu.TryLock()
		if producerUnlocked {
			state.mu.Unlock()
		}
	})
	state = owner.publicGCState()
	state.resultSlots().resultRootsMade, state.resultSlots().resultTokenCount = 1, 1
	state.resultSlots().resultTokens[0], state.resultSlots().resultRootSlots[0] = 23, 7
	var later int
	other := cleanupTestRootedProducer(t, func() { later++ })
	batch := referenceTokenEntries{
		gcResultReleaseBatch: gcResultReleaseBatch{gcRoots: []gcResultRoot{
			{token: 23, slot: 7, ownerIndex: 0, owner: owner},
			{token: 24, owner: other},
		}},
	}
	value, panicked := cleanupTestPanic(func() {
		releaseReferenceEntriesWith(batch, releaseFuncrefTokenMetadata, func(entry gcResultRoot) {
			releaseGCResultRootMetadataWith(entry, func(actual *Instance, slot uint32) error {
				if actual != owner || slot != 7 || state.mu.TryLock() {
					panic("slot seam not entered under expected GC-state lock")
				}
				panic(first)
			})
		}, (*gc.Collector).Close)
	})
	if !panicked || value != first || !producerUnlocked || later != 1 {
		t.Fatalf("panic=%v/%v producerUnlocked=%v later=%d", value, panicked, producerUnlocked, later)
	}
	cleanupTestConsumedProducer(t, owner)
	cleanupTestConsumedProducer(t, other)
	for _, entry := range batch.gcRoots {
		if entry.owner != nil || entry.token != 0 {
			t.Fatal("drain retained consumed GC-token metadata")
		}
	}
	// A panic before clearing is not success: the old slot metadata remains.
	if state.resultSlots().resultTokens[0] != 23 || state.resultSlots().resultTokenCount != 1 {
		t.Fatal("panicking GC slot phase reported successful metadata clearing")
	}
}

func TestReferenceEntryCleanupGCMatchAndIgnoredSlotError(t *testing.T) {
	var callbacks int
	owner := cleanupTestRootedProducer(t, func() { callbacks++ })
	if !owner.retainResourceRootForFinalization() {
		t.Fatal("second GC-token root")
	}
	state := owner.publicGCState()
	state.resultSlots().resultRootsMade, state.resultSlots().resultTokenCount = 2, 2
	state.resultSlots().resultTokens[0], state.resultSlots().resultRootSlots[0] = 31, 7
	state.resultSlots().resultTokens[1], state.resultSlots().resultRootSlots[1] = 32, 8
	batch := referenceTokenEntries{gcResultReleaseBatch: gcResultReleaseBatch{gcRoots: []gcResultRoot{
		{token: 31, slot: 7, ownerIndex: 0, owner: owner},
		{token: 99, slot: 8, ownerIndex: 1, owner: owner}, // stale token: no clear
	}}}
	var slots []uint32
	releaseReferenceEntriesWith(batch, releaseFuncrefTokenMetadata, func(entry gcResultRoot) {
		releaseGCResultRootMetadataWith(entry, func(actual *Instance, slot uint32) error {
			if actual != owner {
				t.Error("wrong GC token owner")
			}
			slots = append(slots, slot)
			return errors.New("diagnostic slot error retains existing ignored-error behavior")
		})
	}, (*gc.Collector).Close)
	if callbacks != 1 || !reflect.DeepEqual(slots, []uint32{7}) || state.resultSlots().resultTokenCount != 1 || state.resultSlots().resultTokens[0] != 0 || state.resultSlots().resultTokens[1] != 32 {
		t.Fatalf("callbacks=%d slots=%v count=%d tokens=%v", callbacks, slots, state.resultSlots().resultTokenCount, state.resultSlots().resultTokens[:2])
	}
	cleanupTestConsumedProducer(t, owner)
}
