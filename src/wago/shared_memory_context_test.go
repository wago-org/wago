package wago

import (
	"testing"

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
