//go:build arm64

package arm64

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestBackendPrimitiveHelpers(t *testing.T) {
	m := maskOf(X0, X5, X19)
	if !m.has(X5) || m.has(X1) || m.count() != 3 {
		t.Fatalf("mask behavior = has(X5)=%v has(X1)=%v count=%d", m.has(X5), m.has(X1), m.count())
	}
	if got, ok := m.firstIn([]Reg{X2, X19, X0}); !ok || got != X19 {
		t.Fatalf("firstIn = %v, %v", got, ok)
	}
	if m.remove(X5).union(maskOf(X1)).count() != 3 {
		t.Fatal("mask remove/union lost register membership")
	}
	if !isScratchGP(X1) || isScratchGP(X19) || gpAllocPos(X19) < 0 || gpAllocPos(SP) != -1 {
		t.Fatal("allocator register classification is inconsistent")
	}
	if entryStrideShift(false) != 5 || entryStrideShift(true) != 3 {
		t.Fatal("table entry stride shift is incorrect")
	}
	if fsize(false) != 4 || fsize(true) != 8 || mtOf2(false) != mtF32 || mtOf2(true) != mtF64 || mtOfInt(false) != mtI32 || mtOfInt(true) != mtI64 {
		t.Fatal("floating-point and integer type helpers are inconsistent")
	}
	if floatBits(1.5, false) != 0x3fc00000 || floatBits(1.5, true) != 0x3ff8000000000000 {
		t.Fatal("float bit conversion changed")
	}
	for _, v := range []int64{-0x1000, -0xfff, 0, 0xfff, 0x1000} {
		want := v >= -0xfff && v <= 0xfff
		if fitsAddSubImm12(v) != want {
			t.Fatalf("fitsAddSubImm12(%d) = %v, want %v", v, fitsAddSubImm12(v), want)
		}
	}
	if !fitsImm32(-1<<31) || !fitsImm32(1<<31-1) || fitsImm32(1<<31) || memRefFoldable(storage{}, false) || memRefFoldable(storage{}, true) {
		t.Fatal("immediate or memory-reference fold classification changed")
	}
	if got := zeroStorage(mtI64); got.kind != stConst || got.typ != mtI64 || got.cval != 0 {
		t.Fatalf("zero storage = %#v", got)
	}
	f := &fn{locals: []localDef{{reg: X2}, {reg: Reg(3), isFloat: true}}, activeLoopPins: []loopPin{{local: 0, reg: X12}}}
	if reg, isFloat, ok := f.pinReg(0); !ok || reg != X12 || isFloat {
		t.Fatalf("loop pin = %v, %v, %v", reg, isFloat, ok)
	}
	if reg, isFloat, ok := f.pinReg(1); !ok || reg != Reg(3) || !isFloat {
		t.Fatalf("float pin = %v, %v, %v", reg, isFloat, ok)
	}
	if _, _, ok := f.pinReg(-1); ok {
		t.Fatal("negative local index was pinned")
	}

	first, err := readSingleTableIndex(wasm.NewReader([]byte{0x81, 0x01}))
	if err != nil || first != 129 {
		t.Fatalf("single table index = %d, %v", first, err)
	}
	left, right, err := readTablePairIndexes(wasm.NewReader([]byte{2, 3}))
	if err != nil || left != 2 || right != 3 {
		t.Fatalf("table index pair = %d, %d, %v", left, right, err)
	}
}

func TestCondenseDeferredFloatCompareValue(t *testing.T) {
	for _, tc := range []struct {
		op  wOp
		typ machineType
		dst Reg
	}{
		{opGtS, mtF32, regNone},
		{opLeS, mtF64, X5},
	} {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		f.pushValue(storage{kind: stConst, typ: tc.typ, cval: int64(floatBits(1, tc.typ == mtF64))})
		f.pushValue(storage{kind: stConst, typ: tc.typ, cval: int64(floatBits(2, tc.typ == mtF64))})
		f.pushFCompare(tc.op, tc.typ == mtF64)
		node := f.s.back()
		got := f.condenseFCompareValue(node, tc.dst)
		if tc.dst != regNone && got != tc.dst {
			t.Fatalf("compare result = %v, want %v", got, tc.dst)
		}
		if node.kind != ekValue || node.st.typ != mtI32 || node.op != opNone || len(f.a.B) == 0 {
			t.Fatalf("condensed node = %#v, code = %d bytes", node, len(f.a.B))
		}
	}
}

func TestCrossInstanceAndMixedRegisterCallLowering(t *testing.T) {
	cross := &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{}, memSizeReg: regNone}
	intResult := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I64}}
	if err := cross.emitCrossInstanceCall(ImportBinding{CrossInstance: true, CalleeLinMem: 0x1000, CalleeEntry: 0x2000}, intResult); err != nil {
		t.Fatalf("cross-instance lowering: %v", err)
	}
	if cross.depth() != 1 || cross.s.back().st.typ != mtI64 || len(cross.a.B) == 0 {
		t.Fatalf("cross-instance result stack/code = depth %d, top %#v, code %d", cross.depth(), cross.s.back(), len(cross.a.B))
	}
	for _, results := range [][]wasm.ValType{{wasm.F64}, {wasm.V128}} {
		f := &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{}, memSizeReg: regNone}
		if err := f.emitCrossInstanceCall(ImportBinding{CrossInstance: true, CalleeLinMem: 0x3000, CalleeEntry: 0x4000}, &wasm.CompType{Kind: wasm.CompFunc, Results: results}); err != nil {
			t.Fatalf("cross-instance %v lowering: %v", results, err)
		}
		if f.depth() != 1 || len(f.a.B) == 0 {
			t.Fatalf("cross-instance %v result depth/code = %d/%d", results, f.depth(), len(f.a.B))
		}
	}

	mixed := &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{}, memSizeReg: regNone}
	mixed.pushValue(storage{kind: stConst, typ: mtF64, cval: int64(floatBits(3.5, true))})
	mixed.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
	floatResult := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.F64, wasm.I32}, Results: []wasm.ValType{wasm.F64}}
	mixed.emitMixedRegisterCall(4, floatResult)
	if mixed.depth() != 1 || mixed.s.back().st.typ != mtF64 || len(mixed.relocs) != 1 || len(mixed.a.B) == 0 {
		t.Fatalf("mixed call result stack/code = depth %d, top %#v, relocs %d, code %d", mixed.depth(), mixed.s.back(), len(mixed.relocs), len(mixed.a.B))
	}

	registerArgs := &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{}, memSizeReg: regNone}
	floatArg := registerArgs.pushValue(storage{kind: stReg, typ: mtF32, reg: 3})
	intArg := registerArgs.pushValue(storage{kind: stReg, typ: mtI64, reg: X4})
	registerArgs.fregUser[3], registerArgs.regUser[X4] = floatArg, intArg
	twoIntResults := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.F32, wasm.I64}, Results: []wasm.ValType{wasm.I64, wasm.I32}}
	registerArgs.emitMixedRegisterCall(5, twoIntResults)
	if registerArgs.depth() != 2 || len(registerArgs.relocs) != 1 || len(registerArgs.a.B) == 0 {
		t.Fatalf("mixed register-arg result stack/code = depth %d, relocs %d, code %d", registerArgs.depth(), len(registerArgs.relocs), len(registerArgs.a.B))
	}
}

func TestLoopRegionPinLifecycle(t *testing.T) {
	saved := loopRegionPinsEnabled
	loopRegionPinsEnabled = true
	t.Cleanup(func() { loopRegionPinsEnabled = saved })
	f := &fn{
		a:         &a64.Asm{},
		localType: []machineType{mtI32, mtI64, mtF32, mtI32},
		localSlot: []int{0, 1, 2, 3},
		locals: []localDef{
			{reg: regNone, state: lsConstZero},
			{reg: regNone, state: lsMem},
			{reg: regNone, isFloat: true, state: lsMem},
			{reg: X5, state: lsMem},
		},
		nLocals: 4,
	}
	fr := &ctrlFrame{kind: cfLoop, loopSetLocals: map[uint32]bool{0: true, 1: true, 2: true, 3: true}}
	f.activateLoopPins(fr)
	if len(fr.loopPins) != 2 || fr.loopPins[0].local != 0 || fr.loopPins[1].local != 1 ||
		!f.pinnedLocalMask.has(X12) || !f.pinnedLocalMask.has(X13) || len(f.a.B) == 0 {
		t.Fatalf("activated loop pins = %#v, mask = %#v, code = %d", fr.loopPins, f.pinnedLocalMask, len(f.a.B))
	}
	if f.locals[0].state != lsReg || f.locals[1].state != lsStackReg {
		t.Fatalf("pin states = %v, %v", f.locals[0].state, f.locals[1].state)
	}
	f.ctrl = []ctrlFrame{{}, *fr}
	before := len(f.a.B)
	f.storeLoopPinsLeaving(-1)
	if len(f.a.B) <= before {
		t.Fatal("leaving loop did not store loop pins")
	}
	f.releaseLoopPins(fr)
	if f.pinnedLocalMask.has(X12) || f.pinnedLocalMask.has(X13) || f.locals[0].state != lsMem || f.locals[1].state != lsMem {
		t.Fatalf("released loop pins left mask/state = %#v, %v, %v", f.pinnedLocalMask, f.locals[0].state, f.locals[1].state)
	}

	blocked := &ctrlFrame{kind: cfLoop, loopHasCall: true, loopSetLocals: map[uint32]bool{0: true}}
	f.activateLoopPins(blocked)
	if len(blocked.loopPins) != 0 {
		t.Fatal("call-containing loop received region pins")
	}
}

func TestConstantDivisionLoweringHelpers(t *testing.T) {
	savedUnsigned, savedSigned := magicDivEnabled, magicDivSignedEnabled
	t.Cleanup(func() { magicDivEnabled, magicDivSignedEnabled = savedUnsigned, savedSigned })
	magicDivEnabled, magicDivSignedEnabled = true, true
	for _, tc := range []struct {
		c      int64
		wide   bool
		signed bool
		want   bool
	}{
		{0, false, false, false}, {1, true, true, false}, {-1, false, true, false},
		{8, false, false, true}, {-8, true, true, true}, {7, true, false, true}, {7, false, true, true},
	} {
		if got := strengthReducible(tc.c, tc.wide, tc.signed); got != tc.want {
			t.Fatalf("strengthReducible(%d, %v, %v) = %v, want %v", tc.c, tc.wide, tc.signed, got, tc.want)
		}
	}
	magicDivEnabled = false
	if strengthReducible(7, true, false) {
		t.Fatal("unsigned non-power-of-two remained reducible with magic disabled")
	}
	magicDivSignedEnabled = false
	if strengthReducible(7, true, true) {
		t.Fatal("signed non-power-of-two remained reducible with magic disabled")
	}
	magicDivEnabled, magicDivSignedEnabled = true, true

	for _, tc := range []struct {
		d       uint64
		wide    bool
		wantRem bool
	}{
		{1, false, false}, {1, true, true}, {8, false, false}, {8, true, true}, {7, false, false}, {7, true, true},
	} {
		f := &fn{a: &a64.Asm{}}
		f.divConstUnsigned(X0, tc.d, tc.wide, tc.wantRem)
		if !(tc.d == 1 && !tc.wantRem) && len(f.a.B) == 0 {
			t.Fatalf("unsigned division emitted no code for %#v", tc)
		}
	}
	for _, tc := range []struct {
		d       int64
		wide    bool
		wantRem bool
	}{
		{8, false, false}, {-8, true, false}, {8, true, true}, {7, false, false}, {-7, true, true},
	} {
		f := &fn{a: &a64.Asm{}}
		f.divConstSigned(X1, tc.d, tc.wide, tc.wantRem)
		if len(f.a.B) == 0 {
			t.Fatalf("signed division emitted no code for %#v", tc)
		}
	}
	for _, wide := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			f := &fn{a: &a64.Asm{}}
			if got := f.magicMulHigh(X2, 0xaaaaaaaaaaaaaaab, wide, signed); got == regNone || len(f.a.B) == 0 {
				t.Fatalf("magic high wide=%v signed=%v = %v, code=%d", wide, signed, got, len(f.a.B))
			}
		}
	}
	for _, tc := range []struct {
		imm  int64
		wide bool
	}{{0xff, false}, {0x0123456789abcdef, true}} {
		f := &fn{a: &a64.Asm{}}
		f.andImm(X3, tc.imm, tc.wide)
		if len(f.a.B) == 0 {
			t.Fatalf("and immediate emitted no code for %#v", tc)
		}
	}
}

func TestTableEntryAddressLowering(t *testing.T) {
	f := &fn{a: &a64.Asm{}}
	f.tableEntryAddr(X0, X1)
	if got := len(f.a.B); got != 12 {
		t.Fatalf("table entry address code = %d bytes, want three instructions", got)
	}
	extern := &fn{a: &a64.Asm{}, m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapExtern)}}}}}
	if !extern.tableIsExternref(0) || extern.tableIsExternref(1) {
		t.Fatal("table reference-kind classification changed")
	}
	extern.typedTableEntryAddr(X2, X3, 0)
	if got := len(extern.a.B); got != 12 {
		t.Fatalf("externref table entry address code = %d bytes, want three instructions", got)
	}
	for _, externref := range []bool{false, true} {
		entry := &fn{a: &a64.Asm{}}
		entry.entryArrayAddr(X4, X5, externref)
		if got := len(entry.a.B); got != 8 {
			t.Fatalf("entry array address externref=%v = %d bytes, want two instructions", externref, got)
		}
	}
	for _, tableIdx := range []uint32{0, 1} {
		checked := &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{Tables: []wasm.Table{
			{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc)}},
			{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapExtern)}},
		}}}
		entry, table := checked.checkedTableEntryAddr(X0, tableIdx)
		if entry != X0 || table == regNone || len(checked.a.B) == 0 {
			t.Fatalf("checked table address %d = entry %v table %v code %d", tableIdx, entry, table, len(checked.a.B))
		}
	}
}

func TestAddFoldImmWrapper(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    int64
		wide bool
		ok   bool
	}{
		{"i32-add", 7, false, true},
		{"i64-add", 7, true, true},
		{"i32-sub", -7, false, true},
		{"i64-sub", -7, true, true},
		{"positive-overflow", 0x1000, true, false},
		{"negative-overflow", -0x1000, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fn{a: &a64.Asm{}}
			if got := f.addFoldImm(X0, tc.v, tc.wide); got != tc.ok {
				t.Fatalf("addFoldImm(%d, wide=%v) = %v, want %v", tc.v, tc.wide, got, tc.ok)
			}
			if tc.ok && len(f.a.B) != 4 {
				t.Fatalf("addFoldImm emitted %d bytes", len(f.a.B))
			}
			if !tc.ok && len(f.a.B) != 0 {
				t.Fatalf("unfoldable immediate emitted %d bytes", len(f.a.B))
			}
		})
	}
}

func TestReferenceAndTableSizeLoweringHelpers(t *testing.T) {
	t.Run("ref.null", func(t *testing.T) {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		if err := f.refNull(wasm.NewReader([]byte{0x70})); err != nil {
			t.Fatal(err)
		}
		if f.depth() != 1 || f.s.back().st.kind != stConst || f.s.back().st.cval != 0 {
			t.Fatalf("ref.null stack = %#v", f.s.back().st)
		}
	})
	t.Run("ref.func", func(t *testing.T) {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		if err := f.refFunc(wasm.NewReader([]byte{3})); err != nil {
			t.Fatal(err)
		}
		if f.depth() != 1 || f.s.back().st.typ != mtI64 || len(f.a.B) == 0 {
			t.Fatalf("ref.func stack/code = %#v / %d bytes", f.s.back().st, len(f.a.B))
		}
	})
	t.Run("ref.is_null", func(t *testing.T) {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: 0})
		f.refIsNull()
		if f.depth() != 1 || f.s.back().st.typ != mtI32 || len(f.a.B) == 0 {
			t.Fatalf("ref.is_null stack/code = %#v / %d bytes", f.s.back().st, len(f.a.B))
		}
	})
	t.Run("ref.eq", func(t *testing.T) {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: 1})
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: 2})
		f.refEq()
		if f.depth() != 1 || f.s.back().st.typ != mtI32 || len(f.a.B) == 0 {
			t.Fatalf("ref.eq stack/code = %#v / %d bytes", f.s.back().st, len(f.a.B))
		}
	})
	t.Run("table.size", func(t *testing.T) {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		if err := f.tableSize(wasm.NewReader([]byte{0})); err != nil {
			t.Fatal(err)
		}
		if f.depth() != 1 || f.s.back().st.typ != mtI32 || len(f.a.B) == 0 {
			t.Fatalf("table.size stack/code = %#v / %d bytes", f.s.back().st, len(f.a.B))
		}
	})
	for _, tc := range []struct {
		name string
		call func(*fn) error
	}{
		{"ref.null-immediate", func(f *fn) error { return f.refNull(wasm.NewReader(nil)) }},
		{"ref.func-index", func(f *fn) error { return f.refFunc(wasm.NewReader(nil)) }},
		{"table.size-index", func(f *fn) error { return f.tableSize(wasm.NewReader(nil)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(&fn{a: &a64.Asm{}, s: newStack()}); err == nil {
				t.Fatal("malformed immediate was accepted")
			}
		})
	}
}

func TestSkipRefHeapTypeImmediate(t *testing.T) {
	for _, data := range [][]byte{{0x70}, {0x80, 0x01}} {
		if err := skipRefHeapTypeImmediate(wasm.NewReader(data)); err != nil {
			t.Fatalf("skip %#x: %v", data, err)
		}
	}
	if err := skipRefHeapTypeImmediate(wasm.NewReader([]byte{0x80})); err == nil {
		t.Fatal("truncated heap type immediate accepted")
	}
}

func TestDiscardSimpleStorageForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   storage
		ok   bool
	}{
		{"constant", storage{kind: stConst, typ: mtI32}, true},
		{"local-reference", storage{kind: stLocalRef, typ: mtI32}, true},
		{"local-register", storage{kind: stLocalReg, typ: mtI32}, true},
		{"global-register", storage{kind: stGlobReg, typ: mtI32}, true},
		{"slot", storage{kind: stSlot, typ: mtI32}, true},
		{"memory-reference", storage{kind: stMemRef, typ: mtI32}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fn{s: newStack()}
			e := f.pushValue(tc.st)
			if got := f.discardSimple(e); got != tc.ok {
				t.Fatalf("discardSimple = %v, want %v", got, tc.ok)
			}
		})
	}
	f := &fn{s: newStack()}
	e := f.pushValue(storage{kind: stReg, typ: mtI32, reg: X0})
	f.regUser[X0] = e
	if !f.discardSimple(e) || f.regUser[X0] != nil {
		t.Fatal("discardSimple did not release a register value")
	}
	if (&fn{s: newStack()}).discardSimple(&elem{kind: ekDeferred}) {
		t.Fatal("discardSimple discarded a deferred node")
	}
}

func TestSpillIfUsedRegisterValue(t *testing.T) {
	f := &fn{a: &a64.Asm{}, s: newStack()}
	e := f.pushValue(storage{kind: stReg, typ: mtI32, reg: X0})
	f.regUser[X0] = e
	f.spillIfUsed(X0)
	if f.regUser[X0] != nil || e.st.kind != stSlot || e.st.typ != mtI32 || len(f.a.B) == 0 {
		t.Fatalf("spill state = %#v, code=%d", e.st, len(f.a.B))
	}
	f.spillIfUsed(X0)
}

func TestSpillFRegisterValues(t *testing.T) {
	for _, typ := range []machineType{mtF32, mtV128} {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		e := f.pushValue(storage{kind: stReg, typ: typ, reg: X0})
		f.fregUser[X0] = e
		f.spillF(e)
		if f.fregUser[X0] != nil || e.st.kind != stSlot || e.st.typ != typ || len(f.a.B) == 0 {
			t.Fatalf("spillF(%v) state = %#v, code=%d", typ, e.st, len(f.a.B))
		}
	}
}

func TestSIMDLoadExtensionSplatAndZeroLowering(t *testing.T) {
	memarg := []byte{0, 0} // alignment and offset, both zero.
	newLoadFn := func() *fn {
		f := &fn{a: &a64.Asm{}, s: newStack(), guardMode: true}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
		return f
	}
	for sub := uint32(1); sub <= 6; sub++ {
		f := newLoadFn()
		if err := f.v128LoadExtend(wasm.NewReader(memarg), sub); err != nil {
			t.Fatalf("load extend subopcode %d: %v", sub, err)
		}
		if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
			t.Fatalf("load extend subopcode %d: stack/code = %#v / %d", sub, f.s.back().st, len(f.a.B))
		}
	}
	for _, tc := range []struct {
		sub  uint32
		size int
	}{{7, 1}, {8, 2}, {9, 4}, {10, 8}} {
		if got := simdLoadSplatSize(tc.sub); got != tc.size {
			t.Fatalf("splat size %d = %d, want %d", tc.sub, got, tc.size)
		}
		f := newLoadFn()
		if err := f.v128LoadSplat(wasm.NewReader(memarg), tc.sub); err != nil {
			t.Fatalf("load splat subopcode %d: %v", tc.sub, err)
		}
		if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
			t.Fatalf("load splat subopcode %d: stack/code = %#v / %d", tc.sub, f.s.back().st, len(f.a.B))
		}
	}
	for _, tc := range []struct {
		sub  uint32
		size int
	}{{92, 4}, {93, 8}} {
		if got := simdLoadZeroSize(tc.sub); got != tc.size {
			t.Fatalf("zero size %d = %d, want %d", tc.sub, got, tc.size)
		}
		f := newLoadFn()
		if err := f.v128LoadZero(wasm.NewReader(memarg), tc.sub); err != nil {
			t.Fatalf("load zero subopcode %d: %v", tc.sub, err)
		}
		if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
			t.Fatalf("load zero subopcode %d: stack/code = %#v / %d", tc.sub, f.s.back().st, len(f.a.B))
		}
	}
	for _, call := range []func(){
		func() { simdLoadSplatSize(0) },
		func() { simdLoadZeroSize(0) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid SIMD load subopcode did not panic")
				}
			}()
			call()
		}()
	}
	for _, tc := range []struct {
		name string
		call func(*fn, *wasm.Reader) error
	}{
		{"extend", func(f *fn, r *wasm.Reader) error { return f.v128LoadExtend(r, 1) }},
		{"splat", func(f *fn, r *wasm.Reader) error { return f.v128LoadSplat(r, 7) }},
		{"zero", func(f *fn, r *wasm.Reader) error { return f.v128LoadZero(r, 92) }},
	} {
		for _, data := range [][]byte{nil, {0}} {
			f := newLoadFn()
			if err := tc.call(f, wasm.NewReader(data)); err == nil {
				t.Fatalf("%s accepted malformed memarg %#x", tc.name, data)
			}
		}
	}
	for _, call := range []func(){
		func() { _ = newLoadFn().v128LoadExtend(wasm.NewReader(memarg), 0) },
		func() { _ = newLoadFn().v128LoadSplat(wasm.NewReader(memarg), 0) },
		func() { _ = newLoadFn().v128LoadZero(wasm.NewReader(memarg), 0) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid SIMD lowering subopcode did not panic")
				}
			}()
			call()
		}()
	}
}

func TestSIMDLaneMemoryLowering(t *testing.T) {
	newLaneFn := func() *fn {
		f := &fn{a: &a64.Asm{}, s: newStack(), guardMode: true}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0}) // address, below the vector operand
		v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: X0})
		f.fregUser[X0] = v
		return f
	}
	for _, tc := range []struct {
		loadSub  uint32
		storeSub uint32
	}{
		{84, 88}, {85, 89}, {86, 90}, {87, 91},
	} {
		f := newLaneFn()
		if err := f.v128LoadLane(wasm.NewReader([]byte{0, 0, 0}), tc.loadSub); err != nil {
			t.Fatalf("load lane subopcode %d: %v", tc.loadSub, err)
		}
		if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
			t.Fatalf("load lane subopcode %d: stack/code = %#v / %d", tc.loadSub, f.s.back().st, len(f.a.B))
		}
		f = newLaneFn()
		if err := f.v128StoreLane(wasm.NewReader([]byte{0, 0, 0}), tc.storeSub); err != nil {
			t.Fatalf("store lane subopcode %d: %v", tc.storeSub, err)
		}
		if f.depth() != 0 || len(f.a.B) == 0 {
			t.Fatalf("store lane subopcode %d: depth/code = %d / %d", tc.storeSub, f.depth(), len(f.a.B))
		}
	}
	for _, tc := range []struct {
		name string
		call func(*fn, *wasm.Reader) error
	}{
		{"load", func(f *fn, r *wasm.Reader) error { return f.v128LoadLane(r, 84) }},
		{"store", func(f *fn, r *wasm.Reader) error { return f.v128StoreLane(r, 88) }},
	} {
		for _, data := range [][]byte{nil, {0}, {0, 0}} {
			if err := tc.call(newLaneFn(), wasm.NewReader(data)); err == nil {
				t.Fatalf("%s lane accepted malformed immediate %#x", tc.name, data)
			}
		}
	}
	for _, call := range []func(){
		func() { _ = newLaneFn().v128LoadLane(wasm.NewReader([]byte{0, 0, 0}), 0) },
		func() { _ = newLaneFn().v128StoreLane(wasm.NewReader([]byte{0, 0, 0}), 0) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid SIMD lane subopcode did not panic")
				}
			}()
			call()
		}()
	}
}

func TestSIMDPlainLoadAndStoreLowering(t *testing.T) {
	newLoadFn := func() *fn {
		f := &fn{a: &a64.Asm{}, s: newStack(), guardMode: true}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
		return f
	}
	f := newLoadFn()
	if err := f.v128Load(wasm.NewReader([]byte{0, 0})); err != nil {
		t.Fatal(err)
	}
	if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
		t.Fatalf("v128.load stack/code = %#v / %d", f.s.back().st, len(f.a.B))
	}
	newStoreFn := func() *fn {
		f := &fn{a: &a64.Asm{}, s: newStack(), guardMode: true}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
		v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: X0})
		f.fregUser[X0] = v
		return f
	}
	f = newStoreFn()
	if err := f.v128Store(wasm.NewReader([]byte{0, 0})); err != nil {
		t.Fatal(err)
	}
	if f.depth() != 0 || len(f.a.B) == 0 {
		t.Fatalf("v128.store depth/code = %d / %d", f.depth(), len(f.a.B))
	}
	for _, tc := range []struct {
		name string
		call func(*fn, *wasm.Reader) error
		new  func() *fn
	}{
		{"load", func(f *fn, r *wasm.Reader) error { return f.v128Load(r) }, newLoadFn},
		{"store", func(f *fn, r *wasm.Reader) error { return f.v128Store(r) }, newStoreFn},
	} {
		for _, data := range [][]byte{nil, {0}} {
			if err := tc.call(tc.new(), wasm.NewReader(data)); err == nil {
				t.Fatalf("v128.%s accepted malformed memarg %#x", tc.name, data)
			}
		}
	}
}

func TestSIMDAllTrueLowering(t *testing.T) {
	newVectorFn := func() *fn {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: X0})
		f.fregUser[X0] = v
		return f
	}
	for _, lower := range []func(*fn){
		func(f *fn) { f.v128AnyTrue() },
		func(f *fn) { f.v128AllTrue(f.a.NeonCmeqB) },
		func(f *fn) { f.i8x16AllTrue() },
		func(f *fn) { f.i16x8AllTrue() },
		func(f *fn) { f.i32x4AllTrue() },
		func(f *fn) { f.i64x2AllTrue() },
	} {
		f := newVectorFn()
		lower(f)
		if f.depth() != 1 || f.s.back().st.typ != mtI32 || len(f.a.B) == 0 {
			t.Fatalf("all_true lowering stack/code = %#v / %d", f.s.back().st, len(f.a.B))
		}
	}
}

func TestSIMDBitmaskAndSplatLowering(t *testing.T) {
	newVectorFn := func() *fn {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: X0})
		f.fregUser[X0] = v
		return f
	}
	for _, lower := range []func(*fn){
		func(f *fn) { f.i8x16Bitmask() },
		func(f *fn) { f.i16x8Bitmask() },
		func(f *fn) { f.i32x4Bitmask() },
		func(f *fn) { f.i64x2Bitmask() },
	} {
		f := newVectorFn()
		lower(f)
		if f.depth() != 1 || f.s.back().st.typ != mtI32 || len(f.a.B) == 0 {
			t.Fatalf("bitmask stack/code = %#v / %d", f.s.back().st, len(f.a.B))
		}
	}
	for _, tc := range []struct {
		kind uint32
		typ  machineType
	}{
		{15, mtI32}, {16, mtI32}, {17, mtI32}, {18, mtI64}, {19, mtF32}, {20, mtF64},
	} {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		f.pushValue(storage{kind: stConst, typ: tc.typ, cval: 1})
		f.v128Splat(tc.kind)
		if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
			t.Fatalf("splat kind %d stack/code = %#v / %d", tc.kind, f.s.back().st, len(f.a.B))
		}
	}
	for _, size := range []int{1, 2, 4, 8} {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		r := f.v128SplatScalar(X0, size)
		if r == regNone || len(f.a.B) == 0 {
			t.Fatalf("scalar splat size %d = %v, %d bytes", size, r, len(f.a.B))
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("invalid scalar splat width did not panic")
			}
		}()
		_ = (&fn{a: &a64.Asm{}, s: newStack()}).v128SplatScalar(X0, 0)
	}()
}

func TestSIMDLaneExtractAndReplaceLowering(t *testing.T) {
	newVectorFn := func() *fn {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: X0})
		f.fregUser[X0] = v
		return f
	}
	for _, tc := range []struct {
		kind uint32
		lane byte
		typ  machineType
	}{
		{21, 1, mtI32}, {22, 1, mtI32}, {24, 1, mtI32}, {25, 1, mtI32},
		{27, 1, mtI32}, {29, 1, mtI64}, {31, 1, mtF32}, {33, 1, mtF64},
	} {
		f := newVectorFn()
		f.v128ExtractLane(tc.kind, tc.lane)
		if f.depth() != 1 || f.s.back().st.typ != tc.typ || len(f.a.B) == 0 {
			t.Fatalf("extract kind %d stack/code = %#v / %d", tc.kind, f.s.back().st, len(f.a.B))
		}
	}
	for _, tc := range []struct {
		kind      uint32
		scalarTyp machineType
	}{
		{23, mtI32}, {26, mtI32}, {28, mtI32}, {30, mtI64}, {32, mtF32}, {34, mtF64},
	} {
		f := newVectorFn()
		f.pushValue(storage{kind: stConst, typ: tc.scalarTyp, cval: 1})
		f.v128ReplaceLane(tc.kind, 1)
		if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
			t.Fatalf("replace kind %d stack/code = %#v / %d", tc.kind, f.s.back().st, len(f.a.B))
		}
	}
}

func TestSIMDBitselectAndRelaxedMaddLowering(t *testing.T) {
	newVectors := func(n int) *fn {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		for i := 0; i < n; i++ {
			r := Reg(i)
			v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: r})
			f.fregUser[r] = v
		}
		return f
	}
	f := newVectors(3)
	f.v128Bitselect()
	if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
		t.Fatalf("bitselect stack/code = %#v / %d", f.s.back().st, len(f.a.B))
	}
	for _, tc := range []struct{ f64, neg bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
		f := newVectors(3)
		f.v128RelaxedMadd(tc.f64, tc.neg)
		if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
			t.Fatalf("relaxed madd f64=%v neg=%v stack/code = %#v / %d", tc.f64, tc.neg, f.s.back().st, len(f.a.B))
		}
	}
}

func TestSIMDConversionLowering(t *testing.T) {
	newVectorFn := func() *fn {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: X0})
		f.fregUser[X0] = v
		return f
	}
	for _, tc := range []struct {
		name string
		call func(*fn) error
	}{
		{"trunc-f32-s", func(f *fn) error { return f.v128I32x4TruncSat(wasm.NewReader(nil), false, true) }},
		{"trunc-f32-u", func(f *fn) error { return f.v128I32x4TruncSat(wasm.NewReader(nil), false, false) }},
		{"trunc-f64-s", func(f *fn) error { return f.v128I32x4TruncSat(wasm.NewReader(nil), true, true) }},
		{"trunc-f64-u", func(f *fn) error { return f.v128I32x4TruncSat(wasm.NewReader(nil), true, false) }},
		{"demote", func(f *fn) error { return f.v128DemoteF64x2Zero(wasm.NewReader(nil)) }},
		{"promote", func(f *fn) error { return f.v128PromoteLowF32x4(wasm.NewReader(nil)) }},
		{"convert-f32-s", func(f *fn) error { return f.v128I32x4ConvertToFloat(wasm.NewReader(nil), false, true) }},
		{"convert-f32-u", func(f *fn) error { return f.v128I32x4ConvertToFloat(wasm.NewReader(nil), false, false) }},
		{"convert-f64-s", func(f *fn) error { return f.v128I32x4ConvertToFloat(wasm.NewReader(nil), true, true) }},
		{"convert-f64-u", func(f *fn) error { return f.v128I32x4ConvertToFloat(wasm.NewReader(nil), true, false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newVectorFn()
			if err := tc.call(f); err != nil {
				t.Fatal(err)
			}
			if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
				t.Fatalf("conversion stack/code = %#v / %d", f.s.back().st, len(f.a.B))
			}
		})
	}
}

func TestI64x2MulLowering(t *testing.T) {
	f := &fn{a: &a64.Asm{}, s: newStack()}
	for _, r := range []Reg{X0, X1} {
		v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: r})
		f.fregUser[r] = v
	}
	if err := f.i64x2Mul(wasm.NewReader(nil)); err != nil {
		t.Fatal(err)
	}
	if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
		t.Fatalf("i64x2.mul stack/code = %#v / %d", f.s.back().st, len(f.a.B))
	}
}

func TestSIMDWideningLowering(t *testing.T) {
	newVectors := func(n int) *fn {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		for i := 0; i < n; i++ {
			r := Reg(i)
			v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: r})
			f.fregUser[r] = v
		}
		return f
	}
	for _, tc := range []struct {
		name string
		call func(*fn) error
	}{
		{"i16.extend.ss", func(f *fn) error { return f.i16x8ExtendI8x16(wasm.NewReader(nil), true, true) }},
		{"i16.extend.s", func(f *fn) error { return f.i16x8ExtendI8x16(wasm.NewReader(nil), true, false) }},
		{"i16.extend.uh", func(f *fn) error { return f.i16x8ExtendI8x16(wasm.NewReader(nil), false, true) }},
		{"i16.extend.u", func(f *fn) error { return f.i16x8ExtendI8x16(wasm.NewReader(nil), false, false) }},
		{"i16.extadd.s", func(f *fn) error { return f.i16x8ExtaddPairwiseI8x16(wasm.NewReader(nil), true) }},
		{"i16.extadd.u", func(f *fn) error { return f.i16x8ExtaddPairwiseI8x16(wasm.NewReader(nil), false) }},
		{"i32.extend.ss", func(f *fn) error { return f.i32x4ExtendI16x8(wasm.NewReader(nil), true, true) }},
		{"i32.extend.s", func(f *fn) error { return f.i32x4ExtendI16x8(wasm.NewReader(nil), true, false) }},
		{"i32.extend.uh", func(f *fn) error { return f.i32x4ExtendI16x8(wasm.NewReader(nil), false, true) }},
		{"i32.extend.u", func(f *fn) error { return f.i32x4ExtendI16x8(wasm.NewReader(nil), false, false) }},
		{"i32.extadd.s", func(f *fn) error { return f.i32x4ExtaddPairwiseI16x8(wasm.NewReader(nil), true) }},
		{"i32.extadd.u", func(f *fn) error { return f.i32x4ExtaddPairwiseI16x8(wasm.NewReader(nil), false) }},
		{"i64.extend.ss", func(f *fn) error { return f.i64x2ExtendI32x4(wasm.NewReader(nil), true, true) }},
		{"i64.extend.s", func(f *fn) error { return f.i64x2ExtendI32x4(wasm.NewReader(nil), true, false) }},
		{"i64.extend.uh", func(f *fn) error { return f.i64x2ExtendI32x4(wasm.NewReader(nil), false, true) }},
		{"i64.extend.u", func(f *fn) error { return f.i64x2ExtendI32x4(wasm.NewReader(nil), false, false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newVectors(1)
			if err := tc.call(f); err != nil {
				t.Fatal(err)
			}
			if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
				t.Fatalf("widen stack/code = %#v / %d", f.s.back().st, len(f.a.B))
			}
		})
	}
	for _, tc := range []struct {
		name string
		call func(*fn)
	}{
		{"i16.extmul.ss", func(f *fn) { _ = f.i16x8ExtmulI8x16(wasm.NewReader(nil), true, true) }},
		{"i16.extmul.s", func(f *fn) { _ = f.i16x8ExtmulI8x16(wasm.NewReader(nil), true, false) }},
		{"i16.extmul.uh", func(f *fn) { _ = f.i16x8ExtmulI8x16(wasm.NewReader(nil), false, true) }},
		{"i16.extmul.u", func(f *fn) { _ = f.i16x8ExtmulI8x16(wasm.NewReader(nil), false, false) }},
		{"i32.extmul.ss", func(f *fn) { _ = f.i32x4ExtmulI16x8(wasm.NewReader(nil), true, true) }},
		{"i32.extmul.s", func(f *fn) { _ = f.i32x4ExtmulI16x8(wasm.NewReader(nil), true, false) }},
		{"i32.extmul.uh", func(f *fn) { _ = f.i32x4ExtmulI16x8(wasm.NewReader(nil), false, true) }},
		{"i32.extmul.u", func(f *fn) { _ = f.i32x4ExtmulI16x8(wasm.NewReader(nil), false, false) }},
		{"i64.extmul.ss", func(f *fn) { f.i64x2ExtmulI32x4(true, true) }},
		{"i64.extmul.s", func(f *fn) { f.i64x2ExtmulI32x4(true, false) }},
		{"i64.extmul.uh", func(f *fn) { f.i64x2ExtmulI32x4(false, true) }},
		{"i64.extmul.u", func(f *fn) { f.i64x2ExtmulI32x4(false, false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newVectors(2)
			tc.call(f)
			if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
				t.Fatalf("extmul stack/code = %#v / %d", f.s.back().st, len(f.a.B))
			}
		})
	}
}

func TestSIMDDotProductLowering(t *testing.T) {
	newVectors := func(n int) *fn {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		for i := 0; i < n; i++ {
			r := Reg(i)
			v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: r})
			f.fregUser[r] = v
		}
		return f
	}
	for _, tc := range []struct {
		name string
		n    int
		call func(*fn) error
	}{
		{"relaxed-i16", 2, func(f *fn) error { f.i16x8RelaxedDotI8x16I7x16S(); return nil }},
		{"relaxed-i32-add", 3, func(f *fn) error { f.i32x4RelaxedDotI8x16I7x16AddS(); return nil }},
		{"i32-dot", 2, func(f *fn) error { return f.i32x4DotI16x8S(wasm.NewReader(nil)) }},
		{"q15mulr", 2, func(f *fn) error { return f.i16x8Q15mulrSatS(wasm.NewReader(nil)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newVectors(tc.n)
			if err := tc.call(f); err != nil {
				t.Fatal(err)
			}
			if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
				t.Fatalf("dot stack/code = %#v / %d", f.s.back().st, len(f.a.B))
			}
		})
	}
}

func TestSIMDShiftLowering(t *testing.T) {
	newShiftFn := func() *fn {
		f := &fn{a: &a64.Asm{}, s: newStack()}
		v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: X0})
		f.fregUser[X0] = v
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 65})
		return f
	}
	for _, tc := range []struct {
		name string
		call func(*fn) error
	}{
		{"i8-left", func(f *fn) error { return f.i8x16Shift(wasm.NewReader(nil), f.a.NeonUshlB, false) }},
		{"i8-right", func(f *fn) error { return f.i8x16Shift(wasm.NewReader(nil), f.a.NeonUshrvB, true) }},
		{"i16-left", func(f *fn) error { return f.i16x8Shift(wasm.NewReader(nil), f.a.NeonUshlH, false) }},
		{"i16-right", func(f *fn) error { return f.i16x8Shift(wasm.NewReader(nil), f.a.NeonSshrvH, true) }},
		{"i32-left", func(f *fn) error { return f.i32x4Shift(wasm.NewReader(nil), f.a.NeonUshlS, false) }},
		{"i32-right", func(f *fn) error { return f.i32x4Shift(wasm.NewReader(nil), f.a.NeonUshrvS, true) }},
		{"i64-left", func(f *fn) error { return f.i64x2Shift(wasm.NewReader(nil), f.a.NeonUshlD, false) }},
		{"i64-right", func(f *fn) error { return f.i64x2Shift(wasm.NewReader(nil), f.a.NeonSshrvD, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newShiftFn()
			if err := tc.call(f); err != nil {
				t.Fatal(err)
			}
			if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
				t.Fatalf("shift stack/code = %#v / %d", f.s.back().st, len(f.a.B))
			}
		})
	}
}

func TestTableEntrySnapshotAndFillEmitters(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*fn)
	}{
		{"snapshot-funcref", func(f *fn) { f.snapshotFuncrefDescriptor(X0, 0) }},
		{"fill-funcref", func(f *fn) { f.fillTableEntries(X0, X1, 0) }},
		{"fill-externref", func(f *fn) { f.fillExternrefEntries(X0, X1, X2) }},
		{"copy-funcref", func(f *fn) { f.copyFuncrefToEntry(X0, X1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fn{a: &a64.Asm{}, s: newStack()}
			tc.emit(f)
			if len(f.a.B) == 0 || len(f.a.B)%4 != 0 {
				t.Fatalf("encoded %d bytes", len(f.a.B))
			}
		})
	}
}

func TestElemDropLowering(t *testing.T) {
	f := &fn{a: &a64.Asm{}, s: newStack()}
	if err := f.elemDrop(wasm.NewReader([]byte{2})); err != nil {
		t.Fatal(err)
	}
	if len(f.a.B) == 0 || len(f.a.B)%4 != 0 {
		t.Fatalf("elem.drop encoded %d bytes", len(f.a.B))
	}
	if err := (&fn{a: &a64.Asm{}, s: newStack()}).elemDrop(wasm.NewReader(nil)); err == nil {
		t.Fatal("elem.drop accepted missing element index")
	}
}

func TestDataDropLowering(t *testing.T) {
	f := &fn{a: &a64.Asm{}, s: newStack()}
	if err := f.dataDrop(wasm.NewReader([]byte{3})); err != nil {
		t.Fatal(err)
	}
	if len(f.a.B) == 0 || len(f.a.B)%4 != 0 {
		t.Fatalf("data.drop encoded %d bytes", len(f.a.B))
	}
	if err := (&fn{a: &a64.Asm{}, s: newStack()}).dataDrop(wasm.NewReader(nil)); err == nil {
		t.Fatal("data.drop accepted missing data index")
	}
}

func TestMemoryInitLowering(t *testing.T) {
	f := &fn{a: &a64.Asm{}, s: newStack(), memSizeReg: regNone}
	for range 3 {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
	}
	if err := f.memoryInit(wasm.NewReader([]byte{2, 0})); err != nil {
		t.Fatal(err)
	}
	if f.depth() != 0 || len(f.a.B) == 0 {
		t.Fatalf("memory.init depth/code = %d / %d", f.depth(), len(f.a.B))
	}
	for _, data := range [][]byte{nil, {0}} {
		if err := (&fn{a: &a64.Asm{}, s: newStack(), memSizeReg: regNone}).memoryInit(wasm.NewReader(data)); err == nil {
			t.Fatalf("memory.init accepted malformed immediate %#x", data)
		}
	}
}

func TestMemoryCopyLowering(t *testing.T) {
	f := &fn{a: &a64.Asm{}, s: newStack(), memSizeReg: regNone}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0}) // dst
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0}) // src
	n := f.pushValue(storage{kind: stReg, typ: mtI32, reg: X0})
	f.regUser[X0] = n
	if err := f.memoryCopy(wasm.NewReader([]byte{0, 0})); err != nil {
		t.Fatal(err)
	}
	if f.depth() != 0 || len(f.a.B) == 0 {
		t.Fatalf("memory.copy depth/code = %d / %d", f.depth(), len(f.a.B))
	}
	for _, data := range [][]byte{nil, {0}} {
		if err := (&fn{a: &a64.Asm{}, s: newStack(), memSizeReg: regNone}).memoryCopy(wasm.NewReader(data)); err == nil {
			t.Fatalf("memory.copy accepted malformed immediate %#x", data)
		}
	}
}

func TestMemoryFillLowering(t *testing.T) {
	f := &fn{a: &a64.Asm{}, s: newStack(), memSizeReg: regNone}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})    // dst
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0x5a}) // fill byte
	n := f.pushValue(storage{kind: stReg, typ: mtI32, reg: X0})
	f.regUser[X0] = n
	if err := f.memoryFill(wasm.NewReader([]byte{0})); err != nil {
		t.Fatal(err)
	}
	if f.depth() != 0 || len(f.a.B) == 0 {
		t.Fatalf("memory.fill depth/code = %d / %d", f.depth(), len(f.a.B))
	}
	if err := (&fn{a: &a64.Asm{}, s: newStack(), memSizeReg: regNone}).memoryFill(wasm.NewReader(nil)); err == nil {
		t.Fatal("memory.fill accepted missing memory index")
	}
}

func TestTableGetSetLowering(t *testing.T) {
	for _, externref := range []bool{false, true} {
		ref := wasm.AbsRef(wasm.HeapFunc)
		if externref {
			ref = wasm.AbsRef(wasm.HeapExtern)
		}
		newFn := func() *fn {
			return &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: ref}}}}}
		}
		f := newFn()
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
		if err := f.tableGet(wasm.NewReader([]byte{0})); err != nil {
			t.Fatalf("table.get externref=%v: %v", externref, err)
		}
		if f.depth() != 1 || f.s.back().st.typ != mtI64 || len(f.a.B) == 0 {
			t.Fatalf("table.get externref=%v stack/code = %#v / %d", externref, f.s.back().st, len(f.a.B))
		}
		f = newFn()
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0}) // index
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: 0}) // reference
		if err := f.tableSet(wasm.NewReader([]byte{0})); err != nil {
			t.Fatalf("table.set externref=%v: %v", externref, err)
		}
		if f.depth() != 0 || len(f.a.B) == 0 {
			t.Fatalf("table.set externref=%v depth/code = %d / %d", externref, f.depth(), len(f.a.B))
		}
	}
	for _, call := range []func(*fn, *wasm.Reader) error{
		func(f *fn, r *wasm.Reader) error { return f.tableGet(r) },
		func(f *fn, r *wasm.Reader) error { return f.tableSet(r) },
	} {
		if err := call(&fn{a: &a64.Asm{}, s: newStack()}, wasm.NewReader(nil)); err == nil {
			t.Fatal("table immediate missing index was accepted")
		}
	}
}

func TestTableInitLowering(t *testing.T) {
	for _, externref := range []bool{false, true} {
		ref := wasm.AbsRef(wasm.HeapFunc)
		if externref {
			ref = wasm.AbsRef(wasm.HeapExtern)
		}
		f := &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: ref}}}}}
		for range 3 {
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
		}
		if err := f.tableInit(wasm.NewReader([]byte{1, 0})); err != nil {
			t.Fatalf("table.init externref=%v: %v", externref, err)
		}
		if f.depth() != 0 || len(f.a.B) == 0 {
			t.Fatalf("table.init externref=%v depth/code = %d / %d", externref, f.depth(), len(f.a.B))
		}
	}
	for _, data := range [][]byte{nil, {0}} {
		if err := (&fn{a: &a64.Asm{}, s: newStack()}).tableInit(wasm.NewReader(data)); err == nil {
			t.Fatalf("table.init accepted malformed immediate %#x", data)
		}
	}
}

func TestTableFillLowering(t *testing.T) {
	for _, externref := range []bool{false, true} {
		ref := wasm.AbsRef(wasm.HeapFunc)
		if externref {
			ref = wasm.AbsRef(wasm.HeapExtern)
		}
		f := &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: ref}}}}}
		for range 3 {
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
		}
		if err := f.tableFill(wasm.NewReader([]byte{0})); err != nil {
			t.Fatalf("table.fill externref=%v: %v", externref, err)
		}
		if f.depth() != 0 || len(f.a.B) == 0 {
			t.Fatalf("table.fill externref=%v depth/code = %d / %d", externref, f.depth(), len(f.a.B))
		}
	}
	if err := (&fn{a: &a64.Asm{}, s: newStack()}).tableFill(wasm.NewReader(nil)); err == nil {
		t.Fatal("table.fill accepted missing table index")
	}
}

func TestTableGrowLowering(t *testing.T) {
	for _, externref := range []bool{false, true} {
		ref := wasm.AbsRef(wasm.HeapFunc)
		if externref {
			ref = wasm.AbsRef(wasm.HeapExtern)
		}
		f := &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: ref}}}}}
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: 0}) // init reference
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1}) // delta
		if err := f.tableGrow(wasm.NewReader([]byte{0})); err != nil {
			t.Fatalf("table.grow externref=%v: %v", externref, err)
		}
		if f.depth() != 1 || f.s.back().st.typ != mtI32 || len(f.a.B) == 0 {
			t.Fatalf("table.grow externref=%v stack/code = %#v / %d", externref, f.s.back().st, len(f.a.B))
		}
	}
	if err := (&fn{a: &a64.Asm{}, s: newStack()}).tableGrow(wasm.NewReader(nil)); err == nil {
		t.Fatal("table.grow accepted missing table index")
	}
}

func TestTableCopyLowering(t *testing.T) {
	for _, externref := range []bool{false, true} {
		ref := wasm.AbsRef(wasm.HeapFunc)
		if externref {
			ref = wasm.AbsRef(wasm.HeapExtern)
		}
		f := &fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{Tables: []wasm.Table{
			{Type: wasm.TableType{Ref: ref}},
			{Type: wasm.TableType{Ref: ref}},
		}}}
		for range 3 {
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
		}
		if err := f.tableCopy(wasm.NewReader([]byte{0, 1})); err != nil {
			t.Fatalf("table.copy externref=%v: %v", externref, err)
		}
		if f.depth() != 0 || len(f.a.B) == 0 {
			t.Fatalf("table.copy externref=%v depth/code = %d / %d", externref, f.depth(), len(f.a.B))
		}
	}
	for _, data := range [][]byte{nil, {0}} {
		if err := (&fn{a: &a64.Asm{}, s: newStack()}).tableCopy(wasm.NewReader(data)); err == nil {
			t.Fatalf("table.copy accepted malformed immediate %#x", data)
		}
	}
}

func TestOptimizationKnobAndABIHelpers(t *testing.T) {
	before := OptKnobs()
	t.Cleanup(func() {
		for _, k := range before {
			SetOptKnob(k.Name, k.On)
		}
	})
	if len(before) == 0 || !SetOptKnob(before[0].Name, !before[0].On) || SetOptKnob("not-a-knob", true) {
		t.Fatal("optimization knob lookup changed")
	}
	after := OptKnobs()
	if after[0].Name != before[0].Name || after[0].On == before[0].On || after[0].Desc == "" {
		t.Fatalf("optimization knob update = %#v", after[0])
	}
	if !SetOptKnob("stack-fence", false) || OptKnobs()[len(OptKnobs())-2].Name == "" {
		t.Fatal("inverted optimization knob update failed")
	}
	if got := abiValOff([]wasm.ValType{wasm.I32, wasm.V128, wasm.I64}, 2); got != 24 || abiValSize(wasm.V128) != 16 || abiValSize(wasm.I32) != 8 {
		t.Fatalf("ABI value layout = %d", got)
	}
}

func TestCodegenStatsFormattingAndRegisterNames(t *testing.T) {
	stats := &CodegenStats{
		FuncIdx:       2,
		Name:          "work",
		CodeBytes:     44,
		FrameBytes:    16,
		MaxSpillSlots: 3,
		Calls:         map[string]int{"host": 2, "regabi": 1},
		Peephole:      map[string]int{"fold": 3, "sink": 1},
	}
	if got := fmtCountMap(map[string]int{"b": 2, "a": 1}); got != "a=1 b=2" {
		t.Fatalf("count map = %q", got)
	}
	if got := stats.report(); !strings.Contains(got, `fn#2 "work": code=44B`) || !strings.Contains(got, "calls: host=2 regabi=1") || !strings.Contains(got, "peep:  fold=3 sink=1") {
		t.Fatalf("function report = %q", got)
	}
	ms := &ModuleStats{Funcs: []*CodegenStats{stats, nil}, ModuleGlobalPins: []ModuleGlobalPinInfo{{Global: 4, Reg: "x19"}}}
	if got := ms.String(); !strings.Contains(got, "=== codegen explain: 2 function(s) ===") || !strings.Contains(got, "g4") || !strings.Contains(got, "x19") {
		t.Fatalf("module report = %q", got)
	}
	if (&ModuleStats{}).String() == "" || (*ModuleStats)(nil).String() != "" || (*CodegenStats)(nil).report() != "" {
		t.Fatal("nil/empty statistics rendering mismatch")
	}
	for _, tc := range []struct {
		reg  Reg
		want string
	}{{X0, "x0"}, {X19, "x19"}, {SP, "sp"}, {Reg(40), "x?40"}} {
		if got := regName(tc.reg); got != tc.want {
			t.Fatalf("regName(%d) = %q, want %q", tc.reg, got, tc.want)
		}
	}
}

func TestInlineReportAndMemoryClassificationHelpers(t *testing.T) {
	report := &InlineReport{
		Funcs: []InlineCandidateInfo{
			{Name: "small", Candidate: true, CallSites: 1, Params: 1, Results: 1, Reason: "small body"},
			{Name: "this-name-is-deliberately-longer-than-twenty-eight-characters", Candidate: true, CallSites: 3, Reason: "hot"},
			{Name: "large", Reason: "too big (99 bytes)"},
			{Name: "leaf", Reason: "non-leaf call"},
		},
		NumCandidates: 2, TotalInlinableCallSites: 4, MaxBodyBytes: 64,
	}
	if got := report.String(); !strings.Contains(got, "INLINE  this-name-is-deliberately-l…") || !strings.Contains(got, "too-big=1") || !strings.Contains(got, "non-leaf=1") {
		t.Fatalf("inline report = %q", got)
	}
	if (*InlineReport)(nil).String() != "" || reasonBucket("no call sites") != "unused" || reasonBucket("other") != "other" {
		t.Fatal("inline report helpers returned unexpected values")
	}
	if !instrTouchesMemory(wasm.InstrI32Load) || !instrTouchesMemory(wasm.InstrMemoryGrow) || instrTouchesMemory(wasm.InstrI32Add) {
		t.Fatal("memory instruction classifier is inconsistent")
	}
	if !shouldSkipStackFence(false, 0, 1) || shouldSkipStackFence(true, 0, 1) || shouldSkipStackFence(false, 1000, 1) {
		t.Fatal("stack-fence threshold classification is inconsistent")
	}
}

func TestDirectBackendAdapterCompilesEmptyModule(t *testing.T) {
	b := DirectBackend{}
	if b.Name() != "arm64-direct" {
		t.Fatalf("backend name = %q", b.Name())
	}
	obj, err := b.CompileModule(&wasm.Module{}, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(obj.Code) != 0 || len(obj.Entry) != 0 {
		t.Fatalf("empty module object = %#v", obj)
	}
}

func TestHostIndirectThunksReturnAndVaryBySignature(t *testing.T) {
	ret := []byte{0xc0, 0x03, 0x5f, 0xd6}
	asyncA := HostIndirectThunk(7)
	asyncB := HostIndirectThunk(8)
	if len(asyncA) == 0 || !bytes.Equal(asyncA[len(asyncA)-4:], ret) || bytes.Equal(asyncA, asyncB) {
		t.Fatal("async indirect thunk does not encode its import index and return")
	}
	home := HostIndirectSyncThunk(7, 1, 2)
	owned := HostIndirectOwnedSyncThunk(7, 1, 2)
	moreArgs := HostIndirectSyncThunk(7, 2, 2)
	if !bytes.Equal(home[len(home)-4:], ret) || !bytes.Equal(owned[len(owned)-4:], ret) {
		t.Fatal("sync indirect thunk does not end in ret")
	}
	if bytes.Equal(home, owned) || len(moreArgs) <= len(home) {
		t.Fatal("sync thunk did not preserve its home/arity-specific encoding")
	}
}
