//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestMemory32AddressZExtElision(t *testing.T) {
	requireCompilerDiagnostics(t)
	t.Run("frame local", func(t *testing.T) {
		// Give fifteen parameters more uses than parameter 15 so the latter remains
		// frame-resident. The wrapper passes dirty upper bits, while the i32 frame
		// store/load pair establishes the clean address proven by the optimization.
		params := make([]wasm.ValType, 16)
		for i := range params {
			params[i] = wasm.I32
		}
		body := []byte{0x00, 0x02, 0x40, 0x0b} // no locals; empty block disables regional pinning
		for x := byte(0); x < 15; x++ {
			body = append(body, 0x20, x, 0x1a, 0x20, x, 0x1a) // two local.get/drop pairs
		}
		body = append(body, 0x20, 0x0f, 0x2d, 0x00, 0x00, 0x0b)
		m := modMem(t, 1, params, []wasm.ValType{wasm.I32}, body)
		args := make([]uint64, 16)
		args[15] = 0xdead_beef_0000_0007
		got, _, err := runMemAmd64(t, m, func(mem []byte) { mem[7] = 0xa5 }, args...)
		if err != nil || got != 0xa5 {
			t.Fatalf("load = %#x, %v; want 0xa5", got, err)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatal(err)
		}
		if got := ms.Funcs[0].Peephole["addr-zext-elim"]; got == 0 {
			t.Fatalf("addr-zext-elim did not fire: %v", ms.Funcs[0].Peephole)
		}
	})

	t.Run("dirty host upper", func(t *testing.T) {
		// A wrapper-ABI i32 argument occupies a 64-bit word and may carry arbitrary
		// high bits. Call-free pinned-local ingress canonicalizes it once.
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
			0x00,
			0x20, 0x00, 0x2d, 0x00, 0x00,
			0x0b,
		})
		got, _, err := runMemAmd64(t, m, func(mem []byte) { mem[7] = 0x5a }, 0xdead_beef_0000_0007)
		if err != nil || got != 0x5a {
			t.Fatalf("load = %#x, %v; want 0x5a", got, err)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatal(err)
		}
		if got := ms.Funcs[0].Peephole["addr-zext-elim"]; got != 0 {
			t.Fatalf("isolated borrowed parameter used addr-zext-elim %d times", got)
		}
	})

	t.Run("call-making pinned parameter", func(t *testing.T) {
		// The untaken call keeps this function in the call-making pin class.
		// Its parameter still enters through a 32-bit load, even when the host
		// wrapper supplies arbitrary upper bits in the serialized i32 word.
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
			0x00,
			0x41, 0x00, 0x04, 0x40, // i32.const 0; if
			0x20, 0x00, 0x10, 0x00, 0x1a, 0x0b, // call self; drop; end
			0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b, // local.get; i32.load8_u; end
		})
		got, _, err := runMemAmd64(t, m, func(mem []byte) { mem[7] = 0x6b }, 0xdead_beef_0000_0007)
		if err != nil || got != 0x6b {
			t.Fatalf("load = %#x, %v; want 0x6b", got, err)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatal(err)
		}
		if got := ms.Funcs[0].Peephole["addr-zext-elim"]; got == 0 {
			t.Fatalf("call-making i32 pin kept a redundant zero extension: %v", ms.Funcs[0].Peephole)
		}
	})

	t.Run("internal call result to pinned local", func(t *testing.T) {
		body0 := []byte{
			0x00,
			0x20, 0x00, 0x10, 0x01, 0x21, 0x00, // local.get 0; call 1; local.set 0
			0x20, 0x01, 0x2d, 0x00, 0x00, // load8_u from param 1 after call reload
			0x20, 0x00, 0x2d, 0x00, 0x00, 0x6a, 0x0b, // load8_u from result; add
		}
		body1 := []byte{0x00, 0x03, 0x40, 0x0b, 0x20, 0x00, 0x0b} // loop keeps helper out of leaf inlining
		code0 := append(wasmtest.ULEB(uint32(len(body0))), body0...)
		code1 := append(wasmtest.ULEB(uint32(len(body1))), body1...)
		memory := append([]byte{0x00}, wasmtest.ULEB(1)...)
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
				wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
			wasmtest.Section(5, wasmtest.Vec(memory)),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(code0, code1)),
		)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := runMemAmd64(t, m, func(mem []byte) { mem[7] = 0x39 }, 0xdead_beef_0000_0007, 0xcafe_babe_0000_0007)
		if err != nil || got != 0x72 {
			t.Fatalf("loads after call = %#x, %v; want 0x72", got, err)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatal(err)
		}
		if ms.Funcs[0].Calls["regabi"] == 0 || ms.Funcs[0].Peephole["call-localset-fuse"] == 0 || ms.Funcs[0].Peephole["addr-zext-elim"] < 2 {
			t.Fatalf("missing call or canonical result: calls=%v peep=%v", ms.Funcs[0].Calls, ms.Funcs[0].Peephole)
		}
	})

	t.Run("borrowed local tee", func(t *testing.T) {
		// local.tee preserves the canonical call-free register form established at
		// wrapper ingress.
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
			0x00,
			0x20, 0x00, // local.get 0
			0x22, 0x00, // local.tee 0
			0x2d, 0x00, 0x00, 0x0b,
		})
		got, _, err := runMemAmd64(t, m, func(mem []byte) { mem[7] = 0x3c }, 0xdead_beef_0000_0007)
		if err != nil || got != 0x3c {
			t.Fatalf("load = %#x, %v; want 0x3c", got, err)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatal(err)
		}
		if got := ms.Funcs[0].Peephole["addr-zext-elim"]; got != 0 {
			t.Fatalf("isolated borrowed local.tee used addr-zext-elim %d times", got)
		}
	})

	t.Run("deferred arithmetic address", func(t *testing.T) {
		// i32.add materializes through a 32-bit destination, so its retained
		// upper-zero fact is enough to use it as memory32. The operands also wrap
		// to address zero, while their host words carry dirty upper bits.
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
			0x00,
			0x20, 0x00, // local.get 0 (host word intentionally has dirty high bits)
			0x20, 0x01, // local.get 1 (also has dirty high bits)
			0x6A,             // i32.add
			0x2D, 0x00, 0x00, // i32.load8_u
			0x0B,
		})
		args := []uint64{0xdead_beef_ffff_fff0, 0xcafe_babe_0000_0010}
		got, _, err := runMemAmd64(t, m, func(mem []byte) { mem[0] = 0x79 }, args...)
		if err != nil || got != 0x79 {
			t.Fatalf("load through deferred address = %#x, %v; want 0x79", got, err)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatal(err)
		}
		if got := ms.Funcs[0].Peephole["addr-zext-elim"]; got == 0 {
			t.Fatalf("addr-zext-elim did not accept the deferred clean address: %v", ms.Funcs[0].Peephole)
		}
	})

	t.Run("deferred signed extension address", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
			0x00,
			0x20, 0x00, // local.get 0
			0xC0,             // i32.extend8_s, emitted into a 32-bit destination
			0x2D, 0x00, 0x00, // i32.load8_u
			0x0B,
		})
		got, _, err := runMemAmd64(t, m, func(mem []byte) { mem[1] = 0x5A }, 0xdead_beef_0000_0101)
		if err != nil || got != 0x5A {
			t.Fatalf("load through deferred signed-extension address = %#x, %v; want 0x5a", got, err)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatal(err)
		}
		if got := ms.Funcs[0].Peephole["addr-zext-elim"]; got == 0 {
			t.Fatalf("addr-zext-elim did not accept the signed i32 address: %v", ms.Funcs[0].Peephole)
		}
	})

	t.Run("oob remains oob", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
			0x00, 0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b,
		})
		if _, _, err := runMemAmd64(t, m, nil, 0xdead_beef_0001_0000); err == nil {
			t.Fatal("out-of-bounds memory32 address did not trap")
		}
	})
}

func TestCleanMemory32AddressProof(t *testing.T) {
	saved := memory32AddrZExtElimEnabled
	defer SetOptKnob("addr-zext-elim", saved)
	savedFacts := valueFactsEnabled
	defer SetOptKnob("value-facts", savedFacts)
	if !SetOptKnob("addr-zext-elim", true) {
		t.Fatal("addr-zext-elim is not registered")
	}
	if !SetOptKnob("value-facts", true) {
		t.Fatal("value-facts is not registered")
	}

	f := &fn{policy: currentCodegenPolicy()}
	cleanDeferred := testDeferredElem(opAdd, mtI32, nil, nil)
	cleanDeferred.st.setValueFacts(factUpper32Zero)
	cleanRegister := testValueElem(storage{kind: stReg, typ: mtI32})
	cleanRegister.st.setValueFacts(factUpper32Zero)
	tests := []struct {
		name string
		e    *elem
		want bool
	}{
		{name: "nil"},
		{name: "deferred expression with upper-zero fact", e: cleanDeferred, want: true},
		{name: "materialized expression with upper-zero fact", e: cleanRegister, want: true},
		{name: "nonclean deferred", e: testDeferredElem(opSExt8, mtI32, nil, nil)},
		{name: "wrong deferred type", e: testDeferredElem(opAdd, mtI64, nil, nil)},
		{name: "i32 constant", e: testValueElem(storage{kind: stConst, typ: mtI32}), want: true},
		{name: "i32 frame local", e: testValueElem(storage{kind: stLocalRef, typ: mtI32}), want: true},
		{name: "i64 constant", e: testValueElem(storage{kind: stConst, typ: mtI64})},
		{name: "owned register", e: testValueElem(storage{kind: stReg, typ: mtI32})},
		{name: "spill slot", e: testValueElem(storage{kind: stSlot, typ: mtI32})},
		{name: "borrowed local", e: testValueElem(storage{kind: stLocalReg, typ: mtI32})},
		{name: "borrowed global", e: testValueElem(storage{kind: stGlobReg, typ: mtI32})},
		{name: "deferred memory load", e: testValueElem(storage{kind: stMemRef, typ: mtI32})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.cleanMemory32Address(tt.e); got != tt.want {
				t.Fatalf("cleanMemory32Address() = %v, want %v", got, tt.want)
			}
		})
	}
	f.usesCalls = true
	if got := f.cleanMemory32Address(testValueElem(storage{kind: stLocalReg, typ: mtI32})); !got {
		t.Fatal("call-making whole-function i32 local was not treated as canonical")
	}
	f.intervalReg = []Reg{R12}
	f.canonicalI32Uses = 2
	if got := f.cleanMemory32Address(testValueElem(storage{kind: stLocalReg, typ: mtI32})); !got {
		t.Fatal("third regional borrowed i32 use was not proven profitable and canonical")
	}

	if !SetOptKnob("addr-zext-elim", false) {
		t.Fatal("addr-zext-elim is not registered")
	}
	f.policy = currentCodegenPolicy()
	if f.cleanMemory32Address(testValueElem(storage{kind: stConst, typ: mtI32})) {
		t.Fatal("disabled optimization accepted a clean address")
	}
	if !SetOptKnob("addr-zext-elim", true) || !SetOptKnob("value-facts", false) {
		t.Fatal("could not configure provenance-disabled policy")
	}
	f.policy = currentCodegenPolicy()
	if f.cleanMemory32Address(cleanDeferred) {
		t.Fatal("deferred address used provenance while value-facts was disabled")
	}
}

func TestMemory64AddressDoesNotUseZExtElision(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := modMem(t, 1, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, []byte{
		0x00, 0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b,
	})
	m.Memories[0].Limits.Addr64 = true
	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
		t.Fatal(err)
	}
	if got := ms.Funcs[0].Peephole["addr-zext-elim"]; got != 0 {
		t.Fatalf("memory64 used addr-zext-elim %d times", got)
	}
}
