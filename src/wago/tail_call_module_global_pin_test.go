//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A wrapper-ABI caller (here, one with an externref parameter) can tail-call a
// register-ABI target. The target keeps module-pinned globals in registers, so
// the return trampoline must write them back before returning in place of the
// caller's epilogue; otherwise every global update made by the target is lost.
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
