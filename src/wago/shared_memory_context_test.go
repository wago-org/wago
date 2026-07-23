package wago

import (
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func sharedMemoryPrivateGlobalModule(initial byte) []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x01, 0x01, 0x01) // memory, min=1, max=1
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, initial, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("bump", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x23, 0x00, // global.get 0
			0x41, 0x01, // i32.const 1
			0x6a,       // i32.add
			0x24, 0x00, // global.set 0
			0x23, 0x00, // global.get 0
			0x0b,
		}))),
	)
}

func sharedMemoryHostReentryModule() []byte {
	i32 := []wasm.ValType{wasm.I32}
	hostImport := append(append(wasmtest.Name("env"), wasmtest.Name("reenter")...), 0x00, 0x00)
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x01, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(i32, i32),
			wasmtest.FuncType(nil, i32),
		)),
		wasmtest.Section(2, wasmtest.Vec(hostImport, memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x07, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("inner", 0, 1),
			wasmtest.ExportEntry("outer", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{
				0x20, 0x00, 0x10, 0x00, // reenter(x)
				0x23, 0x00, 0x6a, // + private global
				0x41, 0x00, 0x11, 0x01, 0x00, 0x6a, // + table[0]()
				0x0b,
			}),
			wasmtest.Code([]byte{0x41, 0x0b, 0x0b}),
		)),
	)
}

func TestSharedMemoryHostReentryRestoresParkedContext(t *testing.T) {
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	compiled := MustCompile(sharedMemoryHostReentryModule())
	defer compiled.Close()

	var in *Instance
	in, err = Instantiate(compiled, Imports{
		"env.memory": memory,
		"env.reenter": HostFunc(func(_ HostModule, params, results []uint64) {
			nested, nestedErr := in.Invoke("inner", params[0])
			if nestedErr != nil {
				panic(nestedErr)
			}
			results[0] = nested[0]
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	type outcome struct {
		values []uint64
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		values, callErr := in.Invoke("outer", I32(5))
		done <- outcome{values: values, err: callErr}
	}()
	select {
	case got := <-done:
		if got.err != nil || len(got.values) != 1 || AsI32(got.values[0]) != 24 {
			t.Fatalf("outer = %v, %v; want 24", got.values, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shared-memory host re-entry deadlocked")
	}
}

func TestSharedMemoryRebindsPrivateInstanceContext(t *testing.T) {
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()

	compile := func(initial byte) *Compiled {
		compiled, err := Compile(nil, sharedMemoryPrivateGlobalModule(initial))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = compiled.Close() })
		return compiled
	}
	instantiate := func(compiled *Compiled) *Instance {
		instance, err := Instantiate(compiled, Imports{"env.memory": memory})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = instance.Close() })
		return instance
	}

	a := instantiate(compile(10))
	b := instantiate(compile(20))
	for _, tc := range []struct {
		instance *Instance
		want     int32
	}{{a, 11}, {b, 21}, {a, 12}, {b, 22}} {
		got, err := tc.instance.Invoke("bump")
		if err != nil {
			t.Fatal(err)
		}
		if value := AsI32(got[0]); value != tc.want {
			t.Fatalf("bump = %d, want %d", value, tc.want)
		}
	}
}
