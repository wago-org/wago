//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestEHCatchRouteSnapshotAbove16Pins(t *testing.T) {
	const n = 24
	params := make([]wasm.ValType, n)
	for i := range params {
		params[i] = wasm.F64
	}
	body := []byte{0x02, 0x40, 0x1f, 0x40, 1, 0, 0, 0, 0x10, 0, 0x0b, 0x0b}
	for i := 0; i < n; i++ {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(params, params))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x08, 0, 0x0b}), wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[1].PinnedLocals; got <= 16 {
		t.Fatalf("actual pins = %d, want more than 16", got)
	} else {
		t.Logf("actual pins: %d", got)
	}
}
