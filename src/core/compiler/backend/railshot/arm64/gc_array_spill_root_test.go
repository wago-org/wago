//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func fixedArrayRootEmitter(typ wasm.ValType, count int) *fn {
	f := &fn{
		a: &a64.Asm{}, s: newStackWithCap(minStackArenaCap), sc: &scratch{},
		m: &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{
			Kind: wasm.CompArray, Array: wasm.NewFieldType(wasm.StorageVal(typ), wasm.Var),
		}}}}}},
		ft: &wasm.CompType{Kind: wasm.CompFunc}, gcArrayHelpers: true,
		syncHostSlots: maxSyncHostSlots, memSizeReg: regNone, globalCellReg: regNone,
	}
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: 17})
	for range count {
		f.pushValue(storage{kind: stConst, typ: mtOf(typ)})
	}
	return f
}

func TestFixedArraySpillResultRoot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typ   wasm.ValType
		count int
	}{{"scalar", wasm.I32, 65}, {"vector", wasm.V128, 33}} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixedArrayRootEmitter(tc.typ, tc.count)
			plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true}
			if !plan.SetLiveMasks([]uint64{0, 0, 0}, 2, 1) {
				t.Fatal("invalid test root plan")
			}
			f.gcFrameRoots = plan
			if err := f.emitGCArray(8, wasm.NewReader([]byte{0, byte(tc.count)})); err != nil {
				t.Fatal(err)
			}
			roots := f.rootsBottomToTop()
			if len(roots) != 2 || roots[0].st.hasGCRoot() || !roots[1].st.hasGCRoot() {
				t.Fatal("fixed-array lowering lost its result root")
			}
			f.pushValue(storage{kind: stConst, typ: mtI32})
			f.recordGCFrameSafepoint(1)
			seen := false
			plan.VisitSafepoints(func(index int, offsets []uint32) bool {
				if index == 1 {
					seen = len(offsets) == 1 && offsets[0] == uint32(f.spillOff(1))
				}
				return true
			})
			if !plan.Exact || !seen {
				t.Fatal("later safepoint omitted the fixed-array result")
			}
			offsets, ok := f.prepareGCFrameCallsite(1)
			if !ok || len(offsets) != 1 || offsets[0] != uint32(f.spillOff(1)) {
				t.Fatal("later callsite omitted the fixed-array result")
			}
		})
	}
}

func BenchmarkFixedArraySpillEmission(b *testing.B) {
	for _, tc := range []struct {
		name  string
		typ   wasm.ValType
		count int
	}{
		{"scalar", wasm.I32, 65}, {"vector", wasm.V128, 33},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				f := fixedArrayRootEmitter(tc.typ, tc.count)
				plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true}
				if !plan.SetLiveMasks([]uint64{0, 0, 0}, 2, 1) {
					b.Fatal("invalid test root plan")
				}
				f.gcFrameRoots = plan
				if err := f.emitGCArray(8, wasm.NewReader([]byte{0, byte(tc.count)})); err != nil {
					b.Fatal(err)
				}
				if len(f.a.B) == 0 {
					b.Fatal("no emission")
				}
			}
		})
	}
}
