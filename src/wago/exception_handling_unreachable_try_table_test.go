//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A try_table with parameters may open in unreachable code whose polymorphic
// stack is shallower than its parameter count. Codegen used to slice the
// logical stack at a negative height and fail with a recovered panic.
func TestTryTableWithParamsInUnreachableCode(t *testing.T) {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
	}
	trapAfterUnreachable := []byte{
		0x00,                   // unreachable
		0x1f, 0x01, 0x00, 0x0b, // try_table (type 1), no catches, end
		0x0b,
	}
	skipUnreachable := []byte{
		0x02, 0x7f, // block (result i32)
		0x41, 0x07, 0x41, 0x01, 0x0d, 0x00, // br_if 0 with 7
		0x00,                               // unreachable
		0x1f, 0x02, 0x01, 0x00, 0x00, 0x00, // try_table (type 2) (catch tag 0 -> label 0)
		0x1a, 0x0b, // drop; end try_table
		0x0b, // end block
		0x0b,
	}
	reachable := []byte{
		0x41, 0x05,
		0x1f, 0x01, 0x00, // try_table (type 1), no catches
		0x41, 0x01, 0x6a, // i32.const 1; i32.add
		0x0b,
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("trap", 0, 0),
			wasmtest.ExportEntry("skip", 0, 1),
			wasmtest.ExportEntry("reachable", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(trapAfterUnreachable), wasmtest.Code(skipUnreachable), wasmtest.Code(reachable))),
	)
	c := compileStagedExceptionHandling(t, data)
	defer c.Close()
	inst, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	if _, err := inst.Invoke("trap"); err == nil {
		t.Fatal("trap: want unreachable trap")
	}
	for name, want := range map[string]int32{"skip": 7, "reachable": 6} {
		got, err := inst.Invoke(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if AsI32(got[0]) != want {
			t.Fatalf("%s = %d, want %d", name, AsI32(got[0]), want)
		}
	}
}
