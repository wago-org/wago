//go:build amd64

package dragline

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestSelectAMD64ModuleGlobalPinUsesAggregateLoopWeight(t *testing.T) {
	body := make([]byte, 0, 1100)
	for range 200 {
		body = append(body, 0x23, 0x00, 0x1a) // global.get 0; drop
	}
	body = append(body, 0x02, 0x40, 0x03, 0x40) // block; loop
	for range 128 {
		body = append(body, 0x23, 0x01, 0x1a) // global.get 1; drop
	}
	body = append(body, 0x0c, 0x01, 0x0b, 0x0b, 0x0b) // exit block; end loop/block/function
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0x00, 0x0b}),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	if pin, ok := selectAMD64ModuleGlobalPin(module, false, false); !ok || pin != (amd64ModuleGlobalPin{global: 1, typ: wasm.I64}) {
		t.Fatalf("module pin = %+v, %v; want g1 i64", pin, ok)
	}
	module.Exports = append(module.Exports, wasm.Export{Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 1}})
	if pin, ok := selectAMD64ModuleGlobalPin(module, false, false); !ok || pin != (amd64ModuleGlobalPin{global: 0, typ: wasm.I32}) {
		t.Fatalf("exported hottest global fell back to %+v, %v; want g0 i32", pin, ok)
	}
	module.Exports = nil
	module.Globals[1].Type.Mutable = false
	if pin, ok := selectAMD64ModuleGlobalPin(module, false, false); !ok || pin != (amd64ModuleGlobalPin{global: 0, typ: wasm.I32}) {
		t.Fatalf("immutable hottest global fell back to %+v, %v; want g0 i32", pin, ok)
	}
	module.Globals[0].Type.Mutable = false
	if pin, ok := selectAMD64ModuleGlobalPin(module, false, false); ok {
		t.Fatalf("immutable globals yielded module pin %+v", pin)
	}
}
