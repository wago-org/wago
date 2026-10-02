//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Any extern.convert_any or any.convert_extern selects the extern GC product.
// Its table-backed state expected one compact reference table, so a module that
// converted references and also had an ordinary function table compiled but
// failed to instantiate with "invalid mixed-table layout".
func TestGCExternConversionWithFunctionTable(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00}, // (struct)
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})), // (table 1 funcref)
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0xfb, 0x01, 0x00, // struct.new_default 0
			0xfb, 0x1b, // extern.convert_any
			0xd1, // ref.is_null
			0x0b,
		}))),
	)
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	inst, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close()
	got, err := inst.Invoke("f")
	if err != nil {
		t.Fatal(err)
	}
	if AsI32(got[0]) != 0 {
		t.Fatalf("ref.is_null(extern.convert_any(struct)) = %d, want 0", AsI32(got[0]))
	}
}
