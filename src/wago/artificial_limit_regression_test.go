//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func wideHostImport(module, name string, typeIndex uint32) []byte {
	entry := append(wasmtest.Name(module), wasmtest.Name(name)...)
	entry = append(entry, byte(wasm.ExternFunc))
	return append(entry, wasmtest.ULEB(typeIndex)...)
}

func wideHostSignatureModule(slots int) []byte {
	params := make([]wasm.ValType, slots)
	results := make([]wasm.ValType, slots)
	for i := range params {
		params[i], results[i] = wasm.I64, wasm.I64
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(params, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, results),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			wideHostImport("env", "wide_params", 0),
			wideHostImport("env", "wide_results", 1),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("wide_params", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("wide_results", byte(wasm.ExternFunc), 1),
		)),
	)
}

func TestSynchronousHostCallsSpillBeyond64Slots(t *testing.T) {
	const slots = 65
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)))
	defer rt.Close()
	mod, err := rt.Compile(wideHostSignatureModule(slots))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{
		"env.wide_params": HostFunc(func(_ HostModule, args, results []uint64) {
			for _, value := range args {
				results[0] += value
			}
		}),
		"env.wide_results": HostFunc(func(_ HostModule, _ []uint64, results []uint64) {
			for i := range results {
				results[i] = uint64(i + 1)
			}
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	args := make([]uint64, slots)
	var want uint64
	for i := range args {
		args[i] = uint64(i + 1)
		want += args[i]
	}
	if got := invokeOne(t, in, "wide_params", args...); got != want {
		t.Fatalf("wide parameter sum = %d, want %d", got, want)
	}
	results, err := in.Invoke("wide_results")
	if err != nil || len(results) != slots {
		t.Fatalf("wide results = %v, %v", results, err)
	}
	for i, value := range results {
		if value != uint64(i+1) {
			t.Fatalf("wide result %d = %d, want %d", i, value, i+1)
		}
	}
}

func TestSharedMemorySupportsMoreThan255Importers(t *testing.T) {
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	const importers = 300
	for i := 0; i < importers; i++ {
		if err := memory.attachImporter(); err != nil {
			t.Fatalf("attach importer %d: %v", i, err)
		}
	}
	state := memory.state.Load()
	state.mu.Lock()
	got := state.importerCount()
	state.mu.Unlock()
	if got != importers {
		t.Fatalf("importer count = %d, want %d", got, importers)
	}
	for i := 0; i < importers; i++ {
		memory.detachImporter()
	}
	state.mu.Lock()
	got = state.importerCount()
	state.mu.Unlock()
	if got != 0 {
		t.Fatalf("importer count after detach = %d", got)
	}
}
