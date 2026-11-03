//go:build wagodebug

package gc

import (
	"errors"
	"testing"
)

func TestMatureAllocRunMetadataFailureAndRetry(t *testing.T) {
	h := newMatureRunHeap(t)
	h.beginMatureAdmission(true)
	beforeBump := h.bump
	cleanup := armFailure(&h, failMatureMetadata, 0)
	_, ok, err := h.tryAllocRun(24, 160, spaceOld)
	cleanup()
	if !errors.Is(err, errInjectedFailure) || ok {
		t.Fatalf("metadata failure ok=%t err=%v", ok, err)
	}
	if h.bump != beforeBump || len(h.mature.regions) != 0 || len(h.mature.touched) != 0 {
		t.Fatal("metadata refusal changed allocator or mature transaction state")
	}
	h.restoreAllocTransaction(throughputAllocTransaction{mem: h.mem, bump: beforeBump})
	h.beginMatureAdmission(true)
	if _, ok, err = h.tryAllocRun(24, 160, spaceOld); err != nil || !ok {
		t.Fatalf("retry ok=%t err=%v", ok, err)
	}
	h.commitMatureAdmission()
	if h.mature.regions[0].live != 4096 || h.mature.regions[1].live != 1024 {
		t.Fatalf("retry occupancy = %d, %d", h.mature.regions[0].live, h.mature.regions[1].live)
	}
}

func TestMatureAllocRunTouchedGrowthFailureWithCoveredDescriptors(t *testing.T) {
	h := newMatureRunHeap(t)
	h.mature.regions = make([]matureRegion, 2)
	h.beginMatureAdmission(true)
	beforeBump := h.bump
	cleanup := armFailure(&h, failMatureMetadata, 0)
	_, ok, err := h.tryAllocRun(24, 160, spaceOld)
	cleanup()
	if !errors.Is(err, errInjectedFailure) || ok {
		t.Fatalf("touched growth failure ok=%t err=%v", ok, err)
	}
	if h.bump != beforeBump || len(h.mature.regions) != 2 || len(h.mature.touched) != 0 {
		t.Fatal("touched growth refusal changed allocator or mature transaction state")
	}

	h.restoreAllocTransaction(throughputAllocTransaction{mem: h.mem, bump: beforeBump})
	h.beginMatureAdmission(true)
	if _, ok, err = h.tryAllocRun(24, 160, spaceOld); err != nil || !ok {
		t.Fatalf("retry ok=%t err=%v", ok, err)
	}
	h.commitMatureAdmission()
	if h.mature.regions[0].live != 4096 || h.mature.regions[1].live != 1024 {
		t.Fatalf("retry occupancy = %d, %d", h.mature.regions[0].live, h.mature.regions[1].live)
	}
}
