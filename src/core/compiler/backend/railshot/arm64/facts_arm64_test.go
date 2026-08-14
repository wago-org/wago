//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestValueFactsFitExistingStoragePaddingArm64(t *testing.T) {
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
	if got := f.s.back().st.facts; !got.Has(factUpper32Zero | factBoolean) {
		t.Fatalf("compare facts = %#x, want upper-zero and boolean", got)
	}
}

func TestSignExtensionFactsAndNearMissArm64(t *testing.T) {
	if got := deferredResultFacts(opSExt8, mtI64); !got.Has(factSignExt8 | factSignExt16 | factSignExt32) {
		t.Fatalf("i64 sign-extend8 facts = %#x", got)
	}
	if got := shared.ValueFactsForIntLoad(2, true, false); !got.Has(factUpper32Zero|factSignExt16) || got.Has(factSignExt32) {
		t.Fatalf("i32.load16_s facts = %#x", got)
	}
	if redundantSignExtension(opSExt32, mtI64, mtI32, factSignExt32) {
		t.Fatal("i32 to i64 sign extension must not be removed")
	}
}

func TestRedundantSignExtensionsArm64(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x00,
		0x20, 0x00, // local.get 0
		0xc2, // i64.extend8_s
		0xc3, // i64.extend16_s: redundant
		0xc4, // i64.extend32_s: redundant
		0x0b,
	})
	for _, x := range []uint64{0, 1, 0x7f, 0x80, 0xff, 0x1234, 0x8000000000000000, ^uint64(0)} {
		if got, want := runArm64u(t, m, x), uint64(int64(int8(x))); got != want {
			t.Fatalf("extend chain(%#x) = %#x, want %#x", x, got, want)
		}
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["ext-elim"]; got == 0 {
		t.Fatalf("ext-elim did not consume sign provenance; peeps=%v", stats.Funcs[0].Peephole)
	}
	if got := stats.Funcs[0].Peephole["sign-ext-elim"]; got == 0 {
		t.Fatalf("sign-ext-elim hits = 0; peeps=%v", stats.Funcs[0].Peephole)
	}
	without, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"value-facts": false}})
	if err != nil {
		t.Fatal(err)
	}
	defer without.CodeImage.Close()
	if got, wantLessThan := len(compiled.Code), len(without.Code); got >= wantLessThan {
		t.Fatalf("fact-enabled code bytes = %d, want less than %d", got, wantLessThan)
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
