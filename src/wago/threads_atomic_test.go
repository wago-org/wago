//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

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

func sharedAtomicAddAtModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add-at", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x20, 0x01,
			0xfe, 0x1e, 0x02, 0x00,
			0x0b,
		}))),
	)
}

func sharedAtomicLoadStoreFenceModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("store", 0, 0),
			wasmtest.ExportEntry("load", 0, 1),
			wasmtest.ExportEntry("fence", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x00, 0x20, 0x00, 0xfe, 0x17, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0xfe, 0x10, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfe, 0x03, 0x00, 0x0b}),
		)),
	)
}

func sharedAtomicRMWModule(sub, align byte, typ wasm.ValType) []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("rmw", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0x20, 0x00, 0xfe, sub, align, 0x00, 0x0b,
		}))),
	)
}

func sharedAtomicCmpxchgModule(sub, align byte, typ wasm.ValType) []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cmpxchg", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0x20, 0x00, 0x20, 0x01, 0xfe, sub, align, 0x00, 0x0b,
		}))),
	)
}

func sharedAtomicOverlapModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("rendezvous", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0x41, 0x01, 0xfe, 0x1e, 0x02, 0x00, 0x1a, // arrivals++
			0x03, 0x40, // loop
			0x41, 0x00, 0x41, 0x00, 0xfe, 0x1e, 0x02, 0x00, // atomic read via add(0)
			0x41, 0x02, 0x49, 0x0d, 0x00, // retry while arrivals < 2
			0x0b,
			0x41, 0x04, 0x41, 0x01, 0xfe, 0x1e, 0x02, 0x00, 0x1a, // completions++
			0x0b,
		}))),
	)
}

func TestThreadsAtomicRMWAddExecutesOnSharedMemory(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
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

func TestThreadsAtomicLoadStoreAndFenceExecute(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicLoadStoreFenceModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	instance, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("store", I32(-559038737)); err != nil { // 0xdeadbeef
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(memory.Bytes()[:4]); got != 0xdeadbeef {
		t.Fatalf("host memory = %#x", got)
	}
	result, err := instance.Invoke("load")
	if err != nil || AsI32(result[0]) != int32(-559038737) {
		t.Fatalf("atomic load = %v, %v", result, err)
	}
	if _, err := instance.Invoke("fence"); err != nil {
		t.Fatal(err)
	}
}

func TestThreadsAtomicRMWOperationAndWidthMatrix(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, tc := range []struct {
		name          string
		sub, align    byte
		typ           wasm.ValType
		old, value    uint64
		want, memMask uint64
	}{
		{"add64", 0x1f, 3, wasm.I64, 5, 7, 12, ^uint64(0)},
		{"add8_i32", 0x20, 0, wasm.I32, 0xfe, 5, 3, 0xff},
		{"sub16_i64", 0x2a, 1, wasm.I64, 2, 5, 0xfffd, 0xffff},
		{"and32", 0x2c, 2, wasm.I32, 0xf0f0, 0x0ff0, 0x00f0, 0xffffffff},
		{"or64", 0x34, 3, wasm.I64, 0xf0, 0x0f, 0xff, ^uint64(0)},
		{"xor32", 0x3a, 2, wasm.I32, 0xaa, 0xff, 0x55, 0xffffffff},
		{"xchg32_i64", 0x47, 2, wasm.I64, 0xdeadbeef, 0x12345678, 0x12345678, 0xffffffff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(config, sharedAtomicRMWModule(tc.sub, tc.align, tc.typ))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			binary.LittleEndian.PutUint64(memory.Bytes()[:8], tc.old)
			instance, err := Instantiate(compiled, Imports{"env.memory": memory})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("rmw", tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got := result[0]; got != tc.old&tc.memMask {
				t.Fatalf("old = %#x, want %#x", got, tc.old&tc.memMask)
			}
			if got := binary.LittleEndian.Uint64(memory.Bytes()[:8]) & tc.memMask; got != tc.want {
				t.Fatalf("memory = %#x, want %#x", got, tc.want)
			}
		})
	}
}

func TestThreadsAtomicCmpxchgSuccessFailureAndWidths(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, tc := range []struct {
		name       string
		sub, align byte
		typ        wasm.ValType
		old, mask  uint64
	}{
		{"i32", 0x48, 2, wasm.I32, 0xdeadbeef, 0xffffffff},
		{"i64", 0x49, 3, wasm.I64, 0xfeedfacedeadbeef, ^uint64(0)},
		{"i32_8", 0x4a, 0, wasm.I32, 0xef, 0xff},
		{"i64_16", 0x4d, 1, wasm.I64, 0xbeef, 0xffff},
		{"i64_32", 0x4e, 2, wasm.I64, 0xdeadbeef, 0xffffffff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(config, sharedAtomicCmpxchgModule(tc.sub, tc.align, tc.typ))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			binary.LittleEndian.PutUint64(memory.Bytes()[:8], tc.old)
			instance, err := Instantiate(compiled, Imports{"env.memory": memory})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("cmpxchg", tc.old+1, 0x12345678)
			if err != nil || result[0] != tc.old&tc.mask {
				t.Fatalf("failed cmpxchg old = %v, %v", result, err)
			}
			if got := binary.LittleEndian.Uint64(memory.Bytes()[:8]) & tc.mask; got != tc.old&tc.mask {
				t.Fatalf("failed cmpxchg changed memory to %#x", got)
			}
			result, err = instance.Invoke("cmpxchg", tc.old, 0x12345678)
			if err != nil || result[0] != tc.old&tc.mask {
				t.Fatalf("successful cmpxchg old = %v, %v", result, err)
			}
			if got := binary.LittleEndian.Uint64(memory.Bytes()[:8]) & tc.mask; got != 0x12345678&tc.mask {
				t.Fatalf("successful cmpxchg memory = %#x", got)
			}
		})
	}
}

func TestThreadsAtomicRMWRejectsUnalignedAddressBeforeWrite(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicAddAtModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	instance, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	_, err = instance.Invoke("add-at", I32(1), I32(7))
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapAtomicUnaligned {
		t.Fatalf("unaligned add error = %v, want atomic alignment trap", err)
	}
	if got := binary.LittleEndian.Uint32(memory.Bytes()[:4]); got != 0 {
		t.Fatalf("memory changed on alignment trap: %d", got)
	}
}

func TestThreadsDistinctInstancesOverlapInNativeExecution(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicOverlapModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()

	instances := make([]*Instance, 2)
	for i := range instances {
		instances[i], err = Instantiate(compiled, Imports{"env.memory": memory})
		if err != nil {
			t.Fatal(err)
		}
		defer instances[i].Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, len(instances))
	var ready sync.WaitGroup
	ready.Add(len(instances))
	for _, instance := range instances {
		go func(in *Instance) {
			ready.Done()
			<-start
			_, callErr := in.InvokeContext(ctx, "rendezvous")
			errs <- callErr
		}(instance)
	}
	ready.Wait()
	close(start)
	for range instances {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent native call: %v", err)
		}
	}
	if arrivals := binary.LittleEndian.Uint32(memory.Bytes()[0:4]); arrivals != 2 {
		t.Fatalf("arrivals = %d, want 2", arrivals)
	}
	if completions := binary.LittleEndian.Uint32(memory.Bytes()[4:8]); completions != 2 {
		t.Fatalf("completions = %d, want 2", completions)
	}
}
