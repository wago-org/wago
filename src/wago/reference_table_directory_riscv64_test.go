//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64FunctionTableDirectoryModule() []byte {
	run := []byte{
		0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 0x0c, 0, 2,
		0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 0x0e, 1, 2,
		0x41, 0, 0x11, 0, 1, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 2, 2}, []byte{0x70, 1, 2, 2}, []byte{0x70, 1, 2, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1), wasmtest.ExportEntry("target", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("third", byte(wasm.ExternTable), 2))),
		wasmtest.Section(9, []byte{1, 1, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code(run))),
	)
}

func TestRV64FunctionTableDirectoryExecutes(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2), rv64FunctionTableDirectoryModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64PrimitiveReferenceContract(view) {
		t.Fatal("published primitive execution contract missing")
	}
	if allocs := testing.AllocsPerRun(1000, func() { _ = rv64PrimitiveReferenceContract(view) }); allocs != 0 {
		t.Fatal("warm primitive classification allocates", allocs)
	}
	t.Logf("logical sizes: Compiled=%d memo=%d RV64 indexes=%d RV64 state=%d", unsafe.Sizeof(Compiled{}), unsafe.Sizeof(validateMemo{}), unsafe.Sizeof(compiledCacheIndexes{}), unsafe.Sizeof(railshotTailCompiledState{}))
	changedContract := cloneCompiledMetadata(view)
	changedContract.TableMax++
	if rv64PrimitiveReferenceContract(changedContract) {
		t.Fatal("mutable metadata copy inherited immutable primitive classification")
	}
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	allocs := testing.AllocsPerRun(1000, func() {
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 42 {
			panic("multi-table initialization/copy/indirect call failed")
		}
	})
	control := testing.AllocsPerRun(1000, func() {
		got, err := instance.Invoke("target")
		if err != nil || len(got) != 1 || got[0] != 42 {
			panic("direct allocation control failed")
		}
	})
	t.Logf("warm three-table init/copy/call: %.0f allocations/run; direct result control: %.0f", allocs, control)
	if allocs > control {
		t.Fatal("table operations add warm invocation allocations", allocs, control)
	}
}

func TestRV64FunctionTableDirectoryRejectsChangedMetadata(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2), rv64FunctionTableDirectoryModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"third-table-limits":   func(c *Compiled) { c.extraTables = slices.Clone(c.extraTables); c.extraTables[1].Max = 1 },
		"third-table-type":     func(c *Compiled) { c.extraTables = slices.Clone(c.extraTables); c.extraTables[1].Type = ValExternRef },
		"export-index":         func(c *Compiled) { c.tableExports = map[string]int{"third": 3} },
		"inactive-table-index": func(c *Compiled) { c.passiveElems = slices.Clone(c.passiveElems); c.passiveElems[0].TableIndex = 1 },
		"reference-abi":        func(c *Compiled) { c.nativeTableReferenceABI = false },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if err := rv64ValidateOwnedTable(candidate); err == nil {
				t.Fatal("changed table directory retained admission")
			}
		})
	}
}

func TestRV64ExternalTableDirectoryExecutes(t *testing.T) {
	// Opaque null rows use the compact eight-byte ABI, including nonzero active
	// destinations and passive segment initialization.
	body := []byte{
		0x41, 0, 0xd0, 0x6f, 0x41, 2, 0xfc, 0x11, 1,
		0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 0x0c, 1, 1,
		0x41, 0, 0x41, 0, 0x41, 2, 0xfc, 0x0e, 0, 1,
		0x41, 1, 0x25, 0, 0xd1, 0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 1, 2, 2}, []byte{0x6f, 1, 2, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{2, 6, 1, 0x41, 0, 0x0b, 0x6f, 1, 0xd0, 0x6f, 0x0b, 5, 0x6f, 1, 0xd0, 0x6f, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatal("external table operations", got, err)
	}
	changed := cloneCompiledMetadata(compiled.executionView())
	changed.extraTables = slices.Clone(changed.extraTables)
	changed.extraTables[0].Type = ValFuncRef
	if rv64ValidateOwnedTable(changed) == nil {
		t.Fatal("compact row layout metadata mutation retained admission")
	}
}
