//go:build amd64

package wagobench

import (
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDraglineModuleGlobalValueAcrossDirectAndIndirectCalls(t *testing.T) {
	entry := make([]byte, 0, 400)
	for range 128 {
		entry = append(entry, 0x23, 0x00, 0x1a) // global.get 0; drop
	}
	entry = append(entry,
		0x10, 0x00, 0x1a, // call 0; drop
		0x41, 0x01, 0x11, 0x00, 0x00, 0x1a, // call_indirect type 0, table 0; drop
		0x23, 0x00, 0x0b, // global.get 0; end
	)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x02})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("null", 0, 2), wasmtest.ExportEntry("out_of_bounds", 0, 3))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x01, 0x0b, 0x01, 0x00})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x41, 0x01, 0x6a, 0x24, 0x00, 0x23, 0x00, 0x0b}),
			wasmtest.Code(entry),
			wasmtest.Code([]byte{0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x03, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
	for _, bounds := range []wago.BoundsCheckMode{wago.BoundsChecksExplicit, wago.BoundsChecksSignalsBased} {
		compiled, err := wago.NewRuntimeConfig().WithCompiler(wago.CompilerDragline).WithTarget(wago.TargetNative).WithBoundsChecks(bounds).Compile(source)
		if err != nil {
			t.Fatalf("bounds=%v compile: %v", bounds, err)
		}
		for instanceIndex := range 2 {
			instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("bounds=%v instance=%d: %v", bounds, instanceIndex, err)
			}
			for invocation := range 100 {
				result, err := instance.Invoke("run")
				if err != nil || len(result) != 1 || result[0] != uint64(2*(invocation+1)) {
					t.Fatalf("bounds=%v instance=%d invocation=%d: %v, %v", bounds, instanceIndex, invocation, result, err)
				}
			}
			if result, err := instance.Invoke("null"); err == nil {
				t.Fatalf("bounds=%v instance=%d null slot returned %v instead of trapping", bounds, instanceIndex, result)
			}
			if result, err := instance.Invoke("out_of_bounds"); err == nil {
				t.Fatalf("bounds=%v instance=%d out-of-bounds slot returned %v instead of trapping", bounds, instanceIndex, result)
			}
			instance.Close()
		}
		compiled.Close()
	}
}
