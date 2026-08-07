//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func sharedAtomicAddModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01) // shared memory32, min=1, max=1
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, // i32.const 0: address
			0x20, 0x00, // local.get 0: delta
			0xfe, 0x1e, 0x02, 0x00, // i32.atomic.rmw.add align=4 offset=0
			0x0b,
		}))),
	)
}

func TestThreadsAtomicRMWAddExecutesOnSharedMemory(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads)
	compiled, err := Compile(config, sharedAtomicAddModule())
	if err != nil {
		t.Fatalf("compile shared atomic module: %v", err)
	}
	defer compiled.Close()

	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()

	instance, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatalf("instantiate shared atomic module: %v", err)
	}
	defer instance.Close()

	result, err := instance.Invoke("add", I32(7))
	if err != nil {
		t.Fatalf("atomic add: %v", err)
	}
	if old := AsI32(result[0]); old != 0 {
		t.Fatalf("atomic add old value = %d, want 0", old)
	}
	if got := binary.LittleEndian.Uint32(memory.Bytes()[:4]); got != 7 {
		t.Fatalf("shared memory value = %d, want 7", got)
	}
}
