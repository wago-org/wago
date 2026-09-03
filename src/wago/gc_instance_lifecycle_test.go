package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcHostImportLifecycleModule() []byte {
	structType := []byte{0x5f, 0x00}
	voidType := wasmtest.FuncType(nil, nil)
	hostImport := append(append(wasmtest.Name("env"), wasmtest.Name("mark")...), 0x00)
	hostImport = append(hostImport, wasmtest.ULEB(1)...)
	start := []byte{
		0xfb, 0x01, 0x00, // struct.new_default 0
		0x1a,       // drop
		0x10, 0x00, // call imported mark
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, voidType)),
		wasmtest.Section(2, wasmtest.Vec(hostImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(8, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(start))),
	)
}

func TestRuntimeGCInstanceCloseReleasesHostThunk(t *testing.T) {
	requireCompleteCore3Backend(t)
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	mod, err := rt.Compile(gcHostImportLifecycleModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{
		"env.mark": HostFunc(func(HostModule, []uint64, []uint64) {}),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(in.thunkMem) == 0 {
		t.Fatal("instance has no host thunk mapping")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if !in.resourcesClosed || in.thunkMem != nil {
		t.Fatalf("closed GC instance retained physical resources: released=%v thunk=%d", in.resourcesClosed, len(in.thunkMem))
	}
}

func funcrefCycleTableImport() []byte {
	out := append(wasmtest.Name("link"), wasmtest.Name("state_table")...)
	return append(out, byte(wasm.ExternTable), 0x70, 0x01, 0x04, 0x08)
}

func funcrefCycleMemoryImport() []byte {
	out := append(wasmtest.Name("link"), wasmtest.Name("state_memory")...)
	return append(out, byte(wasm.ExternMem), 0x01, 0x01, 0x02)
}

func funcrefCycleOwnerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x04, 0x08})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("state_table", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("state_memory", byte(wasm.ExternMem), 0),
			wasmtest.ExportEntry("state_global_i32", byte(wasm.ExternGlobal), 0),
		)),
	)
}

func funcrefCycleRelayModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(
			funcrefCycleTableImport(),
			funcrefCycleMemoryImport(),
			wasmtest.GlobalImportEntry("link", "state_global_i32", wasm.I32, true),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("state_table", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("state_memory", byte(wasm.ExternMem), 0),
			wasmtest.ExportEntry("state_global_i32", byte(wasm.ExternGlobal), 0),
		)),
	)
}

func funcrefCycleConsumerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			funcrefCycleTableImport(),
			funcrefCycleMemoryImport(),
			wasmtest.GlobalImportEntry("link", "state_global_i32", wasm.I32, true),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
	)
}

func TestReverseCloseReexportChainReleasesFuncrefCycle(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	compile := func(binary []byte) *Module {
		module, err := rt.Compile(binary)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		return module
	}
	ownerModule := compile(funcrefCycleOwnerModule())
	relayModule := compile(funcrefCycleRelayModule())
	consumerModule := compile(funcrefCycleConsumerModule())
	defer ownerModule.Close()
	defer relayModule.Close()
	defer consumerModule.Close()

	owner, err := rt.Instantiate(context.Background(), ownerModule)
	if err != nil {
		t.Fatal(err)
	}
	instances := []*Instance{owner}
	provider := owner
	for range 2 {
		table, err := provider.ExportedTable("state_table")
		if err != nil {
			t.Fatal(err)
		}
		memory, err := provider.ExportedMemory("state_memory")
		if err != nil {
			t.Fatal(err)
		}
		global, err := provider.ExportedGlobalObject("state_global_i32")
		if err != nil {
			t.Fatal(err)
		}
		provider, err = rt.Instantiate(context.Background(), relayModule, WithImports(Imports{
			"link.state_table":      table,
			"link.state_memory":     memory,
			"link.state_global_i32": global,
		}))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, provider)
	}
	table, err := provider.ExportedTable("state_table")
	if err != nil {
		t.Fatal(err)
	}
	memory, err := provider.ExportedMemory("state_memory")
	if err != nil {
		t.Fatal(err)
	}
	global, err := provider.ExportedGlobalObject("state_global_i32")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(Imports{
		"link.state_table":      table,
		"link.state_memory":     memory,
		"link.state_global_i32": global,
	}))
	if err != nil {
		t.Fatal(err)
	}
	instances = append(instances, consumer)

	for i := len(instances) - 1; i >= 0; i-- {
		if err := instances[i].Close(); err != nil {
			t.Fatal(err)
		}
	}
	for i, instance := range instances {
		state := instance.referenceLifetime().snapshot()
		if state.PhysicalResources || state.ResourceRoots != 0 {
			t.Errorf("instance %d after reverse close: physical=%v roots=%d", i, state.PhysicalResources, state.ResourceRoots)
		}
	}
}
