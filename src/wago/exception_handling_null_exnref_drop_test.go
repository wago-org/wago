//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A catch_all_ref target carries a rooted exception reference when the handler
// runs, but its fallthrough edge carries a null exnref. Dropping that value
// cleared the root record through the null pointer and crashed the process.
func TestDroppedNullExnrefAtCatchRefTarget(t *testing.T) {
	block := func(tryBody ...byte) []byte {
		out := []byte{0x02, 0x69, 0x1f, 0x40, 0x01, 0x03, 0x00} // block (result exnref); try_table (catch_all_ref 0)
		out = append(out, tryBody...)
		return append(out, 0x0b, 0xd0, 0x69, 0x0b, 0x1a) // end; ref.null exn; end; drop
	}
	nullEdge := append(block(0x01), 0x41, 0x01, 0x0b)                                   // nop
	caught := append(append(block(0x08, 0x00), block(0x08, 0x00)...), 0x41, 0x02, 0x0b) // throw 0, twice
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("fallthrough", 0, 0),
			wasmtest.ExportEntry("caught", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(nullEdge), wasmtest.Code(caught))),
	)
	c := compileStagedExceptionHandlingFeatures(t, data, true)
	defer c.Close()
	inst, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	for export, want := range map[string]int32{"fallthrough": 1, "caught": 2} {
		got, err := inst.Invoke(export)
		if err != nil {
			t.Fatalf("%s: %v", export, err)
		}
		if AsI32(got[0]) != want {
			t.Fatalf("%s = %d, want %d", export, AsI32(got[0]), want)
		}
	}
}
