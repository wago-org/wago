//go:build amd64

package amd64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestValueFactsAndRootsFitCompactStorageAMD64(t *testing.T) {
	if got, want := unsafe.Sizeof(storage{}), uintptr(24); got != want {
		t.Fatalf("storage size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(elem{}), uintptr(64); got != want {
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
	if got, want := unsafe.Sizeof(gpCand{}), uintptr(12); got != want {
		t.Fatalf("GP candidate size = %d, want %d", got, want)
	}
}

func TestCompactCallRelocFieldAMD64(t *testing.T) {
	if got, want := compactCallRelocField(int(invalidCallRelocField-1)), invalidCallRelocField-1; got != want {
		t.Fatalf("compact call relocation field = %d, want %d", got, want)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("negative call relocation field accepted")
		}
	}()
	compactCallRelocField(-1)
}

func TestCompactTrapBranchDomainAMD64(t *testing.T) {
	if got, want := compactTrapBranch(int(^uint32(0)-1)), ^uint32(0)-1; got != want {
		t.Fatalf("compact trap branch = %d, want %d", got, want)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("reserved trap branch sentinel accepted")
		}
	}()
	compactTrapBranch(int(^uint32(0)))
}

func TestStorageMetadataFieldsAreIndependentAMD64(t *testing.T) {
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

func TestSignedI32LoadCarriesUpperZeroFactAMD64(t *testing.T) {
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x00,       // no locals
		0x20, 0x00, // local.get 0
		0x2c, 0x00, 0x00, // i32.load8_s align=1 offset=0
		0xad, // i64.extend_i32_u; redundant after the 32-bit load
		0x0b,
	})
	got, _, err := runMemAmd64(t, m, func(memory []byte) { memory[0] = 0xff }, 0)
	if err != nil || got != 0xffffffff {
		t.Fatalf("load8_s then extend_u = %#x, %v; want %#x", got, err, uint64(0xffffffff))
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

func TestCompareCarriesBooleanFactAMD64(t *testing.T) {
	f := fn{s: newStack()}
	f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 1})
	f.pushBinOp(opLtU, mtI32)
	if got := f.s.back().st.valueFacts(); !got.has(factUpper32Zero | factBoolean) {
		t.Fatalf("compare facts = %#x, want upper-zero and boolean", got)
	}
}
