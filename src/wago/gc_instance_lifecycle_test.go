package wago

import (
	"context"
	"testing"

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

func TestReverseCloseReexportChainReleasesFuncrefCycle(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	ownerModule := mustCompileWat(rt, t, `(module
		(table (export "state_table") 4 8 funcref)
		(memory (export "state_memory") 1 2)
		(global (export "state_global_i32") (mut i32) (i32.const 0)))`)
	relayModule := mustCompileWat(rt, t, `(module
		(import "link" "state_table" (table 4 8 funcref))
		(import "link" "state_memory" (memory 1 2))
		(import "link" "state_global_i32" (global (mut i32)))
		(export "state_table" (table 0))
		(export "state_memory" (memory 0))
		(export "state_global_i32" (global 0)))`)
	consumerModule := mustCompileWat(rt, t, `(module
		(type $unary (func (param i32) (result i32)))
		(import "link" "state_table" (table 4 8 funcref))
		(import "link" "state_memory" (memory 1 2))
		(import "link" "state_global_i32" (global (mut i32)))
		(func $identity (type $unary) (local.get 0))
		(elem (i32.const 0) func $identity))`)
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
