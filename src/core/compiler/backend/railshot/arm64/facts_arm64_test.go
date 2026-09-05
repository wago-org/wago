//go:build (linux || darwin) && arm64

package arm64

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestValueFactsAndRootsFitCompactStorageArm64(t *testing.T) {
	if got, want := unsafe.Sizeof(storage{}), uintptr(24); got != want {
		t.Fatalf("storage size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(elem{}), uintptr(32); got != want {
		t.Fatalf("elem size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(stack{}), uintptr(72); got != want {
		t.Fatalf("stack size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(trapSite{}), uintptr(12); got != want {
		t.Fatalf("trapSite size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(callReloc{}), uintptr(12); got != want {
		t.Fatalf("callReloc size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(localDef{}), uintptr(4); got != want {
		t.Fatalf("local definition size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(gpCand{}), uintptr(12); got != want {
		t.Fatalf("GP candidate size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(finalizerFragment{}), uintptr(12); got != want {
		t.Fatalf("finalizer fragment size = %d, want %d", got, want)
	}
}

func TestOperandNodeBackingIsPointerFreeArm64(t *testing.T) {
	var visit func(reflect.Type) bool
	visit = func(typ reflect.Type) bool {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface, reflect.String, reflect.UnsafePointer:
			return true
		case reflect.Array:
			return visit(typ.Elem())
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				if visit(typ.Field(i).Type) {
					return true
				}
			}
		}
		return false
	}
	if visit(reflect.TypeFor[elem]()) {
		t.Fatal("operand node contains a Go-scanned pointer field")
	}
}

func TestCompactOperandNodeVariantPayloadArm64(t *testing.T) {
	e := elem{}
	e.st.typ = mtI64
	e.st.setValueFacts(factUpper32Zero | factBoolean)
	e.setElemKind(ekDeferred)
	e.setDeferredOp(opRotr)
	e.setDeferredDepth(6)
	e.setChildren(nodeID(0x12340056), nodeID(0x789000ab))

	if got := e.elemKind(); got != ekDeferred {
		t.Fatalf("kind = %v, want deferred", got)
	}
	if got := e.deferredOp(); got != opRotr {
		t.Fatalf("op = %v, want rotr", got)
	}
	if got := e.deferredDepth(); got != 6 {
		t.Fatalf("depth = %d, want 6", got)
	}
	if got := e.child0ID(); got != nodeID(0x12340056) {
		t.Fatalf("child 0 = %#x", got)
	}
	if got := e.child1ID(); got != nodeID(0x789000ab) {
		t.Fatalf("child 1 = %#x", got)
	}
	if e.st.typ != mtI64 || e.st.valueFacts() != factUpper32Zero|factBoolean {
		t.Fatalf("shared type/facts changed: typ=%v meta=%#x", e.st.typ, e.st.meta)
	}

	e.st.setGCRoot(true)
	e.st.setEHRoot(true)
	e.setElemKind(ekValue)
	if e.elemKind() != ekValue || !e.st.hasGCRoot() || !e.st.hasEHRoot() {
		t.Fatalf("value kind changed root metadata: kind=%v meta=%#x", e.elemKind(), e.st.meta)
	}
}

func TestCompactCallRelocFieldArm64(t *testing.T) {
	f := fn{}
	if got, want := f.compactCallRelocField(int(invalidCallRelocField-1)), invalidCallRelocField-1; got != want {
		t.Fatalf("compact call relocation field = %d, want %d", got, want)
	}
	if got := f.compactCallRelocField(-1); got != 0 || f.representationLimit != functionRepresentationCallReloc {
		t.Fatalf("negative call relocation field = %d, limit = %d", got, f.representationLimit)
	}
	if got := f.representationError().Error(); got != "arm64: function call relocation exceeds compact representation limit" {
		t.Fatalf("call relocation error = %q", got)
	}
}

func TestCompactControlOffsetsFailClosedArm64(t *testing.T) {
	f := fn{}
	f.appendReturnSite(-1)
	if f.representationLimit != functionRepresentationReturnSite {
		t.Fatalf("return-site limit = %d", f.representationLimit)
	}
	f = fn{}
	if got := f.packFrameEndSite(-1, false); got != 0 || f.representationLimit != functionRepresentationFrameEnd {
		t.Fatalf("frame-end field = %d, limit = %d", got, f.representationLimit)
	}
}

func TestCompactTrapBranchDomainArm64(t *testing.T) {
	if got, want := compactTrapBranch(int(^uint32(0))), ^uint32(0); got != want {
		t.Fatalf("compact trap branch = %d, want %d", got, want)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("negative trap branch offset accepted")
		}
	}()
	compactTrapBranch(-1)
}

func TestStorageMetadataFieldsAreIndependentArm64(t *testing.T) {
	var st storage
	st.setGCRoot(true)
	st.setEHRoot(true)
	st.setValueFacts(factUpper32Zero | factBoolean)
	if !st.hasGCRoot() || !st.hasEHRoot() {
		t.Fatalf("setting facts cleared roots: meta=%#x", st.meta)
	}
	if got := st.valueFacts(); got != factUpper32Zero|factBoolean {
		t.Fatalf("value facts = %#x, want upper-zero and boolean", got)
	}
	st.setValueFacts(0)
	if !st.hasGCRoot() || !st.hasEHRoot() {
		t.Fatalf("clearing facts cleared roots: meta=%#x", st.meta)
	}
	st.setGCRoot(false)
	if st.hasGCRoot() || !st.hasEHRoot() {
		t.Fatalf("clearing GC root changed another field: meta=%#x", st.meta)
	}
}

func TestSignedI32LoadCarriesUpperZeroFactArm64(t *testing.T) {
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x00,       // no locals
		0x20, 0x00, // local.get 0
		0x2c, 0x00, 0x00, // i32.load8_s align=1 offset=0
		0xad, // i64.extend_i32_u; redundant after the W-register load
		0x0b,
	})
	if got, err := runArm64WrapperMem(t, m, 0, func(memory []byte) { memory[0] = 0xff }); err != nil || got != 0xffffffff {
		t.Fatalf("load8_s then extend_u = %#x, %v; want %#x", got, err, uint32(0xffffffff))
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["ext-elim"]; got != 1 {
		t.Fatalf("ext-elim hits = %d, want 1; peeps=%v", got, stats.Funcs[0].Peephole)
	}
}

func TestCompareCarriesBooleanFactArm64(t *testing.T) {
	f := fn{s: newStack()}
	f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 1})
	f.pushBinOp(opLtU, mtI32)
	if got := f.s.back().st.valueFacts(); !got.has(factUpper32Zero | factBoolean) {
		t.Fatalf("compare facts = %#x, want upper-zero and boolean", got)
	}
}

func TestStraightLineLocalCarriesUpperZeroFactArm64(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x01, 0x01, 0x7f, // one declared i32 local
		0x20, 0x00, // local.get 0
		0x20, 0x01, // local.get 1
		0x6a,       // i32.add
		0x21, 0x02, // local.set 2
		0x20, 0x02, // local.get 2
		0xad, // i64.extend_i32_u
		0x0b,
	})
	if got := runArm64u(t, m, 0xffffffff, 2); got != 1 {
		t.Fatalf("wrapped local result = %#x, want 1", got)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["local-fact"]; got != 1 {
		t.Fatalf("local-fact transfers = %d, want 1; peeps=%v", got, stats.Funcs[0].Peephole)
	}
}

func TestLocalFactsDisabledAcrossControlFlowArm64(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x01, 0x01, 0x7f, // one declared i32 local
		0x02, 0x40, // block
		0x20, 0x00, // local.get 0
		0x21, 0x01, // local.set 1
		0x0b,       // end block
		0x20, 0x01, // local.get 1
		0xad, // i64.extend_i32_u
		0x0b,
	})
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["local-fact"]; got != 0 {
		t.Fatalf("local-fact transfers = %d, want 0 across control flow; peeps=%v", got, stats.Funcs[0].Peephole)
	}
}

func TestMemoryAddressUsesUpperZeroFactArm64(t *testing.T) {
	computed := modMem(t, 1,
		[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x28, 0x02, 0x00, 0x0b},
	)
	on, err := CompileModuleWith(computed, CompileOptions{Optimizations: map[string]bool{"value-facts": true}})
	if err != nil {
		t.Fatal(err)
	}
	defer on.CodeImage.Close()
	off, err := CompileModuleWith(computed, CompileOptions{Optimizations: map[string]bool{"value-facts": false}})
	if err != nil {
		t.Fatal(err)
	}
	defer off.CodeImage.Close()
	if got, want := len(on.Code), len(off.Code)-4; got != want {
		t.Fatalf("upper-zero address code bytes = %d, want %d (without fact: %d)", got, want, len(off.Code))
	}
	if got, err := runArm64WrapperWithOptions(t, computed, CompileOptions{Optimizations: map[string]bool{"value-facts": true}}, 1, 2); err != nil || got != 0 {
		t.Fatalf("computed address load = %#x, %v; want zero", got, err)
	}

	// Function parameters have no upper-zero fact: serialized callers may leave
	// non-canonical bits above the Wasm i32, so their explicit truncation remains.
	parameter := modMem(t, 1,
		[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b},
	)
	paramOn, err := CompileModuleWith(parameter, CompileOptions{Optimizations: map[string]bool{"value-facts": true}})
	if err != nil {
		t.Fatal(err)
	}
	defer paramOn.CodeImage.Close()
	paramOff, err := CompileModuleWith(parameter, CompileOptions{Optimizations: map[string]bool{"value-facts": false}})
	if err != nil {
		t.Fatal(err)
	}
	defer paramOff.CodeImage.Close()
	if len(paramOn.Code) != len(paramOff.Code) {
		t.Fatalf("unknown-upper address changed code bytes: with facts %d, without %d", len(paramOn.Code), len(paramOff.Code))
	}
	if got, err := runArm64WrapperWithOptions(t, parameter, CompileOptions{Optimizations: map[string]bool{"value-facts": true}}, uint64(1)<<32); err != nil || got != 0 {
		t.Fatalf("non-canonical parameter address = %#x, %v; want truncated address zero", got, err)
	}
}
