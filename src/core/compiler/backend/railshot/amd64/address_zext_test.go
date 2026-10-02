//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestMemory32AddressZExtElision(t *testing.T) {
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
		// high bits. A nonregional pinned parameter must retain the canonicalizing
		// self-move and use only its low 32-bit address.
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
			t.Fatalf("borrowed parameter used addr-zext-elim %d times", got)
		}
	})

	t.Run("borrowed local tee", func(t *testing.T) {
		// local.tee of a pinned parameter can preserve the wrapper's dirty upper
		// half when source and destination are the same native register.
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
			t.Fatalf("borrowed local.tee used addr-zext-elim %d times", got)
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
	if !SetOptKnob("addr-zext-elim", true) {
		t.Fatal("addr-zext-elim is not registered")
	}

	f := new(fn)
	tests := []struct {
		name string
		e    *elem
		want bool
	}{
		{name: "nil"},
		{name: "clean deferred is not concrete", e: &elem{kind: ekDeferred, typ: mtI32, op: opAdd}},
		{name: "nonclean deferred", e: &elem{kind: ekDeferred, typ: mtI32, op: opSExt8}},
		{name: "wrong deferred type", e: &elem{kind: ekDeferred, typ: mtI64, op: opAdd}},
		{name: "i32 constant", e: &elem{kind: ekValue, st: storage{kind: stConst, typ: mtI32}}, want: true},
		{name: "i32 frame local", e: &elem{kind: ekValue, st: storage{kind: stLocalRef, typ: mtI32}}, want: true},
		{name: "i64 constant", e: &elem{kind: ekValue, st: storage{kind: stConst, typ: mtI64}}},
		{name: "owned register", e: &elem{kind: ekValue, st: storage{kind: stReg, typ: mtI32}}},
		{name: "spill slot", e: &elem{kind: ekValue, st: storage{kind: stSlot, typ: mtI32}}},
		{name: "borrowed local", e: &elem{kind: ekValue, st: storage{kind: stLocalReg, typ: mtI32}}},
		{name: "borrowed global", e: &elem{kind: ekValue, st: storage{kind: stGlobReg, typ: mtI32}}},
		{name: "deferred memory load", e: &elem{kind: ekValue, st: storage{kind: stMemRef, typ: mtI32}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.cleanMemory32Address(tt.e); got != tt.want {
				t.Fatalf("cleanMemory32Address() = %v, want %v", got, tt.want)
			}
		})
	}

	if !SetOptKnob("addr-zext-elim", false) {
		t.Fatal("addr-zext-elim is not registered")
	}
	if f.cleanMemory32Address(&elem{kind: ekValue, st: storage{kind: stConst, typ: mtI32}}) {
		t.Fatal("disabled optimization accepted a clean address")
	}
}

func TestMemory64AddressDoesNotUseZExtElision(t *testing.T) {
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
