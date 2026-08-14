//go:build amd64

package amd64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestValueFactsFitExistingStoragePaddingAMD64(t *testing.T) {
	if got, want := unsafe.Sizeof(storage{}), uintptr(64); got != want {
		t.Fatalf("storage size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(elem{}), uintptr(112); got != want {
		t.Fatalf("elem size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(localDef{}), uintptr(4); got != want {
		t.Fatalf("local definition size = %d, want %d", got, want)
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
	if got := f.s.back().st.facts; !got.has(factUpper32Zero | factBoolean) {
		t.Fatalf("compare facts = %#x, want upper-zero and boolean", got)
	}
}

func TestStraightLineLocalCarriesUpperZeroFactAMD64(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x01, 0x01, 0x7f,
		0x20, 0x00,
		0x20, 0x01,
		0x6a,
		0x21, 0x02,
		0x20, 0x02,
		0xad,
		0x0b,
	})
	if got := runAmd64u(t, m, 0xffffffff, 2); got != 1 {
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

func TestLocalFactsDisabledAcrossControlFlowAMD64(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x01, 0x01, 0x7f,
		0x02, 0x40,
		0x20, 0x00,
		0x21, 0x01,
		0x0b,
		0x20, 0x01,
		0xad,
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
