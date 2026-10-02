//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A try_table whose body ends in `br 0` reaches its end only through the branch.
// That edge must pop the handler record and skip the handler body; AMD64 once
// landed it in the handler, which here dispatched catch_all to the function exit
// and skipped the global store.
func TestTryTableEndReachedOnlyByBranch(t *testing.T) {
	store := []byte{0x42, 0x2a, 0x24, 0x00, 0x0b} // i64.const 42; global.set 0; end
	body := func(tryTable ...byte) []byte {
		out := append(append([]byte(nil), tryTable...), 0x0c, 0x00, 0x0b) // br 0; end
		return append(out, store...)
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", 3, 0),
			wasmtest.ExportEntry("catch_all", 0, 0),
			wasmtest.ExportEntry("no_catch", 0, 1),
			wasmtest.ExportEntry("catch_tag", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(body(0x1f, 0x40, 0x01, 0x02, 0x00)),       // try_table (catch_all 0)
			wasmtest.Code(body(0x1f, 0x40, 0x00)),                   // try_table
			wasmtest.Code(body(0x1f, 0x40, 0x01, 0x00, 0x00, 0x00)), // try_table (catch 0 0)
		)),
	)
	c := compileStagedExceptionHandling(t, data)
	defer c.Close()
	for _, export := range []string{"catch_all", "no_catch", "catch_tag"} {
		inst, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := inst.Invoke(export); err != nil {
			t.Fatalf("%s: %v", export, err)
		}
		g, err := inst.ExportedGlobalObject("g")
		if err != nil {
			t.Fatal(err)
		}
		if got := AsI64(g.Get()); got != 42 {
			t.Errorf("%s: global = %d, want 42", export, got)
		}
		inst.Close()
	}
}
