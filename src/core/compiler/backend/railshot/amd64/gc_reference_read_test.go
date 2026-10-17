//go:build amd64

package amd64

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDirectGCStructReferenceReadRetainsRoot(t *testing.T) {
	typ := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapEq), false))
	f := fixedArrayRootEmitter(typ, 0)
	f.m.Types[0].SubTypes[0].Final = true
	f.m.Types[0].SubTypes[0].Comp = wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{wasm.NewFieldType(wasm.StorageVal(typ), wasm.Var)}}
	f.policy = currentCodegenPolicy()
	if !f.emitDirectGCStructGet(0, 0, gcStructGet) {
		t.Fatal("final compact reference read must use the checked native resolver")
	}
	roots := f.rootsBottomToTop()
	if len(roots) != 1 || roots[0].st.typ != mtI64 || !roots[0].st.hasGCRoot() {
		t.Fatal("native read lost its compact reference root")
	}
	plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true}
	if !plan.SetLiveMasks([]uint64{0, 0}, 1, 1) {
		t.Fatal("invalid root plan")
	}
	f.gcFrameRoots = plan
	f.recordGCFrameSafepoint(0)
	seen := false
	plan.VisitSafepoints(func(_ int, offsets []uint32) bool {
		seen = len(offsets) == 1 && offsets[0] == uint32(f.spillOff(0))
		return true
	})
	if !plan.Exact || !seen {
		t.Fatal("later allocation omitted native reference read")
	}
}

func TestDirectGCStructReferenceReadLimits(t *testing.T) {
	for _, tc := range []struct {
		name          string
		heap          wasm.AbsHeapType
		final, direct bool
	}{
		{"eq", wasm.HeapEq, true, true}, {"any", wasm.HeapAny, true, true}, {"i31", wasm.HeapI31, true, true},
		{"struct", wasm.HeapStruct, true, true}, {"array", wasm.HeapArray, true, true},
		{"func", wasm.HeapFunc, true, false}, {"extern", wasm.HeapExtern, true, false}, {"open", wasm.HeapEq, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typ := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(tc.heap), false))
			makeEmitter := func() *fn {
				f := fixedArrayRootEmitter(typ, 0)
				f.m.Types[0].SubTypes[0].Final = tc.final
				f.m.Types[0].SubTypes[0].Comp = wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{wasm.NewFieldType(wasm.StorageVal(typ), wasm.Var)}}
				f.policy = currentCodegenPolicy()
				return f
			}
			if got := makeEmitter().emitDirectGCStructGet(0, 0, gcStructGet); got != tc.direct {
				t.Fatalf("native read=%v, want %v", got, tc.direct)
			}
			if makeEmitter().emitDirectGCStructSet(0, 0) {
				t.Fatal("reference write must not use the scalar write path")
			}
			if makeEmitter().emitDirectGCStructGet(0, 0, gcStructGetS) {
				t.Fatal("packed read cannot load a reference")
			}
		})
	}
}

// A field outside the signed native displacement range is intentionally
// unsupported by this read path. The getter helper remains the fallback.
func TestGCReferenceReadRejectsInvalidNativeOffset(t *testing.T) {
	typ := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapEq), false))
	f := fixedArrayRootEmitter(typ, 0)
	f.m.Types[0].SubTypes[0].Final = true
	f.m.Types[0].SubTypes[0].Comp = wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{wasm.NewFieldType(wasm.StorageVal(typ), wasm.Var)}}
	metadata, err := frontend.BuildGCTypeMetadata(f.m)
	if err != nil {
		t.Fatal(err)
	}
	f.gcTypeLayouts = metadata.Layouts
	f.gcTypeLayouts[0].FieldLayout[0].Offset = math.MaxUint32
	f.policy = currentCodegenPolicy()
	if f.emitDirectGCStructGet(0, 0, gcStructGet) {
		t.Fatal("invalid native displacement entered direct reference read")
	}
}
