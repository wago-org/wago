//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestLinearIntegerTreeSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	// (7*x + 11*y)*4 + (13*x - 17*y) + 19*x = 60*x + 27*y.
	body := []byte{0, 0x03, 0x7f, 0x20, 0, 0x41, 7, 0x6c, 0x20, 1, 0x41, 11, 0x6c, 0x6a, 0x41, 2, 0x74, 0x20, 0, 0x41, 13, 0x6c, 0x20, 1, 0x41, 17, 0x6c, 0x6b, 0x6a, 0x20, 0, 0x41, 19, 0x6c, 0x6a, 0x0b, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	c.CodeImage.Close()
	var off ModuleStats
	rollback, err := CompileModuleWith(m, CompileOptions{Stats: &off, Optimizations: map[string]bool{"linear-integer-tree": false}})
	if err != nil {
		t.Fatal(err)
	}
	rollback.CodeImage.Close()
	if off.Funcs[0].Peephole["linear-integer-tree"] != 0 {
		t.Fatal("disabled optimization selected")
	}
	if stats.Funcs[0].CodeBytes >= off.Funcs[0].CodeBytes {
		t.Fatalf("on=%d off=%d", stats.Funcs[0].CodeBytes, off.Funcs[0].CodeBytes)
	}

	if got := stats.Funcs[0].Peephole["linear-integer-tree"]; got == 0 {
		t.Fatal("linear tree not selected")
	}
	for _, x := range []uint64{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
		for _, y := range []uint64{0, 1, 0xffffffff} {
			want := uint64(uint32(60*uint32(x) + 27*uint32(y)))
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, x, y)
			if err != nil || got != want {
				t.Fatalf("x=%x y=%x got=%x err=%v want=%x", x, y, got, err, want)
			}
		}
	}
}

func TestLinearIntegerTreeProofRejectsEffectsAndUnknownSources(t *testing.T) {
	for _, kind := range []storageKind{stReg, stSlot, stGlobalRef, stGlobReg, stMemRef} {
		f := fn{s: newStack(), policy: currentCodegenPolicy()}
		e := f.pushValue(storage{kind: kind, typ: mtI32})
		w := linearIntegerTree{f: &f, budget: 64}
		if w.collect(e, 0) {
			t.Fatalf("zero scale accepted observable/unknown kind %v", kind)
		}
	}
	for _, op := range []wOp{opDivS, opDivU, opRemS, opShrS, opShrU, opAnd, opMul} {
		f := fn{s: newStack(), policy: currentCodegenPolicy()}
		f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
		f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 1})
		f.pushBinOp(op, mtI32)
		w := linearIntegerTree{f: &f, budget: 64}
		if w.collect(f.s.back(), 0) {
			t.Fatalf("zero scale accepted %v", op)
		}
	}
	f := fn{s: newStack(), policy: currentCodegenPolicy()}
	w := linearIntegerTree{f: &f, budget: 64}
	for i := uint32(0); i < 9; i++ {
		e := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: i})
		if got := w.collect(e, 1); got != (i < 8) {
			t.Fatalf("index%d admitted=%t", i, got)
		}
	}
	w = linearIntegerTree{f: &f, budget: 0}
	if w.collect(f.s.back(), 1) {
		t.Fatal("exhausted proof admitted")
	}
}

func TestLinearIntegerTreePressureFallsBackBeforeEmission(t *testing.T) {
	for _, blocker := range []string{"pinned", "local", "reserved"} {
		t.Run(blocker, func(t *testing.T) {
			f := gpClassEmitter()
			f.policy = currentCodegenPolicy()
			f.hasLoop = true
			blocked := allAllocatableGP().remove(X0).remove(X1)
			switch blocker {
			case "pinned":
				f.pinned = blocked
			case "local":
				f.pinnedLocalMask = blocked
			case "reserved":
				f.reserved = blocked
			}
			// Six repeated terms need a result and coefficient register while
			// X0 remains borrowed. Only X1 is available for both temporaries.
			for i := 0; i < 6; i++ {
				f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: X0, idx: 0})
				f.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
				f.pushBinOp(opMul, mtI32)
				if i != 0 {
					f.pushBinOp(opAdd, mtI32)
				}
			}
			root, pins := f.s.back(), f.pinned
			if got := f.tryLinearIntegerTree(root, regNone); got != regNone {
				t.Fatalf("pressure selected register %d", got)
			}
			if len(f.a.B) != 0 || f.pinned != pins || f.s.back() != root || !root.isDeferred() {
				t.Fatal("pressure fallback changed emission, pins, or deferred tree")
			}
		})
	}
}

func TestLinearIntegerTreePressureAccountsForLocalLoads(t *testing.T) {
	f := gpClassEmitter()
	f.policy = currentCodegenPolicy()
	f.hasLoop = true
	f.reserved = allAllocatableGP().remove(X0).remove(X1)
	for i := 0; i < 6; i++ {
		f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
		f.pushBinOp(opMul, mtI32)
		if i != 0 {
			f.pushBinOp(opAdd, mtI32)
		}
	}
	root := f.s.back()
	if got := f.tryLinearIntegerTree(root, regNone); got != regNone {
		t.Fatalf("pressure selected register %d", got)
	}
	if len(f.a.B) != 0 || f.pinned != 0 || f.s.back() != root || !root.isDeferred() {
		t.Fatal("pressure fallback changed emission, pins, or deferred tree")
	}
}
