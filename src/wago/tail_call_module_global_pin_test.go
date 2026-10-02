//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A wrapper-ABI caller (here, one with an externref parameter) can tail-call a
// direct-only register-ABI target. The target keeps module-pinned globals in
// registers, so the tail path must write them back before returning in place of
// the caller's epilogue; otherwise every global update made by the target is
// lost.
func TestTailCallFromWrapperABIPersistsModulePinnedGlobals(t *testing.T) {
	inc := []byte{0x23, 0x00, 0x41, 0x01, 0x6a, 0x24, 0x00} // g0 += 1
	inner := []byte{0x03, 0x40}                             // loop
	for range 8 {
		inner = append(inner, inc...)
	}
	inner = append(inner,
		0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, // l2 += 1
		0x20, 0x02, 0x41, 0x02, 0x49, 0x0d, 0x00, // br_if l2 < 2
		0x0b,
	)
	bump := []byte{0x03, 0x40, 0x41, 0x00, 0x21, 0x02} // loop; l2 = 0
	bump = append(bump, inner...)
	bump = append(bump,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, // l1 += 1
		0x20, 0x01, 0x20, 0x00, 0x49, 0x0d, 0x00, // br_if l1 < l0
		0x0b,
		0x0b,
	)
	bumpCode := append([]byte{0x01, 0x02, 0x7f}, bump...) // two i32 locals
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", 3, 0),
			wasmtest.ExportEntry("direct", 0, 1),
			wasmtest.ExportEntry("tail", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(bumpCode))), bumpCode...),
			wasmtest.Code([]byte{0x41, 0x03, 0x10, 0x00, 0x0b}), // call 0 with 3
			wasmtest.Code([]byte{0x41, 0x03, 0x12, 0x00, 0x0b}), // return_call 0 with 3
		)),
	)
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, export := range []string{"direct", "tail"} {
		inst, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		args := []uint64(nil)
		if export == "tail" {
			args = []uint64{0}
		}
		if _, err := inst.Invoke(export, args...); err != nil {
			t.Fatalf("%s: %v", export, err)
		}
		g, err := inst.ExportedGlobalObject("g")
		if err != nil {
			t.Fatal(err)
		}
		if got := AsI32(g.Get()); got != 48 {
			t.Errorf("%s: global = %d, want 48", export, got)
		}
		inst.Close()
	}
}

// The same wrapper-ABI tail path must deliver every argument class. AMD64 once
// jumped into the target's register-ABI body with arguments still in the
// wrapper tail bank, so the target read stale registers.
func TestTailCallFromWrapperABIPassesMixedArguments(t *testing.T) {
	seen := []byte{
		0x20, 0x00, 0x24, 0x00, // global.set 0 (local 0)
		0x20, 0x01, 0x24, 0x01, // global.set 1 (local 1)
		0x20, 0x02, 0x24, 0x02, // global.set 2 (local 2)
		0x0b,
	}
	args := []byte{
		0x41, 0x03, // i32.const 3
		0x42, 0x07, // i64.const 7
		0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x40, // f64.const 2.5
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64, wasm.F64}, nil),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.F64, true, []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("a", 3, 0),
			wasmtest.ExportEntry("b", 3, 1),
			wasmtest.ExportEntry("c", 3, 2),
			wasmtest.ExportEntry("direct", 0, 1),
			wasmtest.ExportEntry("tail", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(seen),
			wasmtest.Code(append(append([]byte(nil), args...), 0x10, 0x00, 0x0b)), // call 0
			wasmtest.Code(append(append([]byte(nil), args...), 0x12, 0x00, 0x0b)), // return_call 0
		)),
	)
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, export := range []string{"direct", "tail"} {
		inst, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		callArgs := []uint64(nil)
		if export == "tail" {
			callArgs = []uint64{0}
		}
		if _, err := inst.Invoke(export, callArgs...); err != nil {
			t.Fatalf("%s: %v", export, err)
		}
		var got [3]uint64
		for i, name := range []string{"a", "b", "c"} {
			g, err := inst.ExportedGlobalObject(name)
			if err != nil {
				t.Fatal(err)
			}
			got[i] = g.Get()
		}
		if AsI32(got[0]) != 3 || AsI64(got[1]) != 7 || AsF64(got[2]) != 2.5 {
			t.Errorf("%s: globals = (%d, %d, %v), want (3, 7, 2.5)", export, AsI32(got[0]), AsI64(got[1]), AsF64(got[2]))
		}
		inst.Close()
	}
}
