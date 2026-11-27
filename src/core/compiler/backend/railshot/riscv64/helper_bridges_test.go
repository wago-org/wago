//go:build linux && !tinygo

package riscv64

import (
	"slices"
	"testing"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestRV64SafepointHelperBridgeArities(t *testing.T) {
	for _, tc := range []struct {
		helper, admitted uint32
		want             int
	}{
		{gcStructAllocDefault, 0, 1},
		{gcStructAllocOne, 1, 1},
		{gcStructAllocOne, 2, 2},
		{gcStructAllocOne, 3, 3},
		{gcStructAllocOne, 4, 4},
		{gcStructAllocOne, 256, 256},
		{gcArrayAllocDefault, 0, 2},
		{gcArrayAllocUniform, 0, 3},
		{gcArrayAllocFixed, 2, 2},
		{gcArrayAllocFixed, 4, 4},
		{gcArrayAllocFixed, 5, 5},
		{gcArrayAllocFixed, 6, 6},
		{gcArrayAllocFixed, 256, 256},
		{gcArrayAllocData, 0, 4},
		{gcArrayAllocElem, 0, 4},
	} {
		got, ok := gcAllocHelperSlots(tc.helper, int(tc.admitted))
		wantSlots, wantOK := tc.want, buildfeatures.GC != 0
		if !wantOK {
			wantSlots = 0
		}
		if ok != wantOK || got != wantSlots {
			t.Fatalf("helper %d/%d slots = %d, %t; want %d, %t", tc.helper, tc.admitted, got, ok, wantSlots, wantOK)
		}
	}
	for _, tc := range []struct {
		helper, admitted uint32
	}{{gcStructGet, 0}, {gcStructAllocOne, 0}, {gcStructAllocOne, 257}, {gcArrayAllocFixed, 1}, {gcArrayAllocFixed, 257}} {
		if got, ok := gcAllocHelperSlots(tc.helper, int(tc.admitted)); ok {
			t.Fatalf("helper %d/%d unexpectedly admitted with %d slots", tc.helper, tc.admitted, got)
		}
	}
}

func TestRV64HelperBridgesFollowBuildFeatures(t *testing.T) {
	gc := buildfeatures.GC != 0
	exceptions := buildfeatures.ExceptionHandling != 0
	frames := gc || exceptions
	references := frames && buildfeatures.ReferenceTypes != 0
	bulk := references && buildfeatures.BulkMemoryOperations != 0
	cases := []struct {
		helper     uint32
		params     int
		results    int
		enabled    bool
		safepoints bool
	}{
		{gcExternGlobalSet, 2, 0, frames, true},
		{gcStructAllocDefault, 1, 1, gc, true},
		{gcStructSet, 4, 0, gc, true},
		{gcStructRefTest, 4, 1, gc, false},
		{gcFunctionSubtypeTest, 3, 1, buildfeatures.ReferenceSignatures, false},
		{gcStructRefCast, 4, 1, gc, false},
		{gcStructTableSet, 3, 0, frames, true},
		{gcAnyConvertExtern, 1, 1, gc, true},
		{gcArrayAllocData, 4, 1, gc, true},
		{gcArrayCopy, 7, 0, gc, false},
		{gcTableFill, 4, 0, references, true},
		{gcTableCopy, 5, 0, bulk, true},
		{gcTableGrow, 3, 1, references, true},
		{gcTableInit, 5, 0, bulk, true},
		{gcTableElemDrop, 1, 0, bulk, false},
		{gcExceptionPayload, 1, 1, exceptions, false},
	}
	var calls []directCall
	for _, tc := range cases {
		calls = append(calls, directCall{helper: tc.helper})
	}
	code, entries, err := appendInternalHelperBridges(nil, calls, MaxModuleBytes, true)
	if err != nil {
		t.Fatal(err)
	}
	offset := 0
	for _, tc := range cases {
		entry, ok := entries[helperBridge{helper: tc.helper}]
		if ok != tc.enabled {
			t.Fatalf("helper %d present = %t, want %t", tc.helper, ok, tc.enabled)
		}
		slots, safepointed := gcAllocHelperSlots(tc.helper, 0)
		if want := tc.enabled && tc.safepoints; safepointed != want || want && slots != tc.params {
			t.Fatalf("helper %d safepoint slots = %d, admitted = %t", tc.helper, slots, safepointed)
		}
		if !ok {
			continue
		}
		offset = (offset + 15) &^ 15
		dispatch, _ := shared.EncodeGCDispatch(tc.helper, 0)
		thunk := HostIndirectOwnedSyncThunk(gcStructDispatchBit|dispatch, tc.params, tc.results)
		if entry != offset || entry+len(thunk) > len(code) || !slices.Equal(thunk, code[entry:entry+len(thunk)]) {
			t.Fatalf("helper %d thunk mismatch at %d, expected offset %d", tc.helper, entry, offset)
		}
		offset += len(thunk)
	}
	if len(code) != offset {
		t.Fatalf("helper code span = %d, want %d", len(code), offset)
	}
}
