//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
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

func sharedAtomicWriteAtModule(sub, align byte, typ wasm.ValType, operands int) []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	params := []wasm.ValType{wasm.I32}
	for range operands {
		params = append(params, typ)
	}
	results := []wasm.ValType(nil)
	if sub >= 0x1e {
		results = []wasm.ValType{typ}
	}
	body := []byte{0x20, 0x00}
	for i := range operands {
		body = append(body, 0x20, byte(i+1))
	}
	body = append(body, 0xfe, sub, align, 0x08, 0x0b) // offset=8
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("write", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func sharedAtomicLoadStoreWidthModule(loadSub, storeSub, align byte, typ wasm.ValType) []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{typ}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{typ}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("store", 0, 0),
			wasmtest.ExportEntry("load", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x00, 0x20, 0x00, 0xfe, storeSub, align, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0xfe, loadSub, align, 0x00, 0x0b}),
		)),
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

func sharedAtomicWaitNotifyModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("notify", 0, 0),
			wasmtest.ExportEntry("wait32", 0, 1),
			wasmtest.ExportEntry("wait64", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0xfe, 0x00, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfe, 0x01, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfe, 0x02, 0x03, 0x00, 0x0b}),
		)),
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
	if compiled.usesAtomicWaitHelpers() || (!forceSyncHostImports && len(instance.ctrl) != 0) {
		t.Fatalf("direct-only atomic module retained wait bridge: helper=%v ctrl=%d", compiled.usesAtomicWaitHelpers(), len(instance.ctrl))
	}

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

func TestThreadsFeatureDoesNotChangeOrdinaryGeneratedCode(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
	)
	base, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2).WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	withThreads, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer withThreads.Close()
	if !bytes.Equal(base.Code, withThreads.Code) {
		t.Fatalf("ordinary generated code changed when threads was enabled: %d vs %d bytes", len(base.Code), len(withThreads.Code))
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

func TestThreadsAtomicLoadStoreWidthAndExtensionMatrix(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, tc := range []struct {
		name                     string
		loadSub, storeSub, align byte
		typ                      wasm.ValType
		mask                     uint64
	}{
		{"i32", 0x10, 0x17, 2, wasm.I32, 0xffffffff},
		{"i64", 0x11, 0x18, 3, wasm.I64, ^uint64(0)},
		{"i32_8", 0x12, 0x19, 0, wasm.I32, 0xff},
		{"i32_16", 0x13, 0x1a, 1, wasm.I32, 0xffff},
		{"i64_8", 0x14, 0x1b, 0, wasm.I64, 0xff},
		{"i64_16", 0x15, 0x1c, 1, wasm.I64, 0xffff},
		{"i64_32", 0x16, 0x1d, 2, wasm.I64, 0xffffffff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(config, sharedAtomicLoadStoreWidthModule(tc.loadSub, tc.storeSub, tc.align, tc.typ))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			const initial = uint64(0xaabbccddeeff0011)
			const value = uint64(0x1122334455667788)
			binary.LittleEndian.PutUint64(memory.Bytes()[:8], initial)
			instance, err := Instantiate(compiled, Imports{"env.memory": memory})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if _, err := instance.Invoke("store", value); err != nil {
				t.Fatal(err)
			}
			wantMemory := initial&^tc.mask | value&tc.mask
			if got := binary.LittleEndian.Uint64(memory.Bytes()[:8]); got != wantMemory {
				t.Fatalf("memory = %#x, want %#x", got, wantMemory)
			}
			result, err := instance.Invoke("load")
			if err != nil || result[0] != value&tc.mask {
				t.Fatalf("load = %v, %v; want %#x", result, err, value&tc.mask)
			}
		})
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

func TestThreadsAtomicWriteMatrixTrapsBeforeMutation(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	type atomicWrite struct {
		name       string
		sub, align byte
		typ        wasm.ValType
		operands   int
	}
	var writes []atomicWrite
	widths := []struct {
		name  string
		align byte
		typ   wasm.ValType
	}{
		{"i32", 2, wasm.I32}, {"i64", 3, wasm.I64}, {"i32_8", 0, wasm.I32},
		{"i32_16", 1, wasm.I32}, {"i64_8", 0, wasm.I64}, {"i64_16", 1, wasm.I64},
		{"i64_32", 2, wasm.I64},
	}
	for i, width := range widths {
		writes = append(writes, atomicWrite{"store_" + width.name, byte(0x17 + i), width.align, width.typ, 1})
	}
	for _, operation := range []struct {
		name string
		base byte
	}{
		{"add", 0x1e}, {"sub", 0x25}, {"and", 0x2c}, {"or", 0x33},
		{"xor", 0x3a}, {"xchg", 0x41},
	} {
		for i, width := range widths {
			writes = append(writes, atomicWrite{operation.name + "_" + width.name, operation.base + byte(i), width.align, width.typ, 1})
		}
	}
	for i, width := range widths {
		writes = append(writes, atomicWrite{"cmpxchg_" + width.name, byte(0x48 + i), width.align, width.typ, 2})
	}

	for _, tc := range writes {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(config, sharedAtomicWriteAtModule(tc.sub, tc.align, tc.typ, tc.operands))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			for i := range memory.Bytes() {
				memory.Bytes()[i] = byte(i*131 + 17)
			}
			want := append([]byte(nil), memory.Bytes()...)
			instance, err := Instantiate(compiled, Imports{"env.memory": memory})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()

			for _, address := range []uint64{65528, uint64(uint32(0xfffffff8))} {
				args := []uint64{address, 0x1122334455667788}
				if tc.operands == 2 {
					args = append(args, 0x8877665544332211)
				}
				_, err = instance.Invoke("write", args...)
				var trap *TrapError
				if !errors.As(err, &trap) || (trap.Code != TrapLinMemOutOfBounds && trap.Code != TrapLinkedMemOutOfBounds) {
					t.Fatalf("address %#x error = %v, want memory bounds trap", address, err)
				}
				if !bytes.Equal(memory.Bytes(), want) {
					t.Fatalf("address %#x mutated memory before trapping", address)
				}
			}
		})
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

func TestThreadsAtomicWaitNotifyExecutesAcrossInstances(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicWaitNotifyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !compiled.usesAtomicWaitHelpers() {
		t.Fatal("wait/notify module did not retain helper admission")
	}
	memory, _ := NewSharedMemory(1, 1)
	defer memory.Close()
	waiter, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	notifier, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer notifier.Close()
	if probe, probeErr := waiter.Invoke("wait32", I32(0), I32(0), I64(0)); probeErr != nil || len(probe) != 1 {
		t.Fatalf("wait bridge probe = %v, %v", probe, probeErr)
	}

	waitResult := make(chan struct {
		value uint64
		err   error
	}, 1)
	go func() {
		out, callErr := waiter.Invoke("wait32", I32(0), I32(0), I64(-1))
		value := uint64(0)
		if len(out) != 0 {
			value = out[0]
		}
		waitResult <- struct {
			value uint64
			err   error
		}{value, callErr}
	}()
	waitForMemoryWaiters(t, memory, 1)
	out, err := notifier.Invoke("notify", I32(0), I32(1))
	if err != nil || len(out) != 1 || AsI32(out[0]) != 1 {
		t.Fatalf("notify = %v, %v; want 1", out, err)
	}
	select {
	case got := <-waitResult:
		if got.err != nil || AsI32(got.value) != int32(memoryWaitNotified) {
			t.Fatalf("wait = %d, %v; want notified", got.value, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("notified Wasm wait did not resume")
	}

	binary.LittleEndian.PutUint32(memory.Bytes()[:4], 7)
	if out, err = waiter.Invoke("wait32", I32(0), I32(8), I64(-1)); err != nil || AsI32(out[0]) != int32(memoryWaitNotEqual) {
		t.Fatalf("mismatched wait32 = %v, %v", out, err)
	}
	if out, err = waiter.Invoke("wait32", I32(0), I32(7), I64(0)); err != nil || AsI32(out[0]) != int32(memoryWaitTimedOut) {
		t.Fatalf("zero-timeout wait32 = %v, %v", out, err)
	}
	binary.LittleEndian.PutUint64(memory.Bytes()[8:16], 0x1122334455667788)
	if out, err = waiter.Invoke("wait64", I32(8), I64(0x1122334455667788), I64(0)); err != nil || AsI32(out[0]) != int32(memoryWaitTimedOut) {
		t.Fatalf("zero-timeout wait64 = %v, %v", out, err)
	}
	assertNoMemoryWaiters(t, memory)
}

func TestThreadsAtomicWaitHelperAdmissionSurvivesArtifactRoundTrip(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicWaitNotifyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	loaded := roundTripCompiled(t, compiled)
	if loaded != compiled {
		defer loaded.Close()
	}
	if !loaded.usesAtomicWaitHelpers() || !loaded.requiredFeatures.IsEnabled(CoreFeatureThreads) {
		t.Fatalf("round-trip helper/features = %v/%s", loaded.usesAtomicWaitHelpers(), loaded.requiredFeatures)
	}
	memory, _ := NewSharedMemory(1, 1)
	defer memory.Close()
	instance, err := Instantiate(loaded, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if len(instance.ctrl) == 0 {
		t.Fatal("round-tripped wait module has no synchronous control frame")
	}
	out, err := instance.Invoke("wait32", I32(0), I32(1), I64(-1))
	if err != nil || AsI32(out[0]) != int32(memoryWaitNotEqual) {
		t.Fatalf("round-tripped wait = %v, %v", out, err)
	}
}

func TestThreadsModuleInspectionReportsExactFeatureAndSharedMemory(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	rt := NewRuntime(WithRuntimeConfig(config))
	defer rt.Close()
	module, err := rt.Compile(sharedAtomicAddModule())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	metadata := module.Metadata()
	if metadata.RequiredFeatures != CoreFeatureThreads {
		t.Fatalf("required features = %s, want threads", metadata.RequiredFeatures)
	}
	if len(metadata.Memories) != 1 || !metadata.Memories[0].Shared || !metadata.Memories[0].HasMax || metadata.Memories[0].Min != 1 || metadata.Memories[0].Max != 1 || metadata.Memories[0].ImportModule != "env" || metadata.Memories[0].ImportName != "memory" {
		t.Fatalf("memory metadata = %#v", metadata.Memories)
	}
}

func TestThreadsAtomicWaitHonorsInvokeCancellationAndClose(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicWaitNotifyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	t.Run("context", func(t *testing.T) {
		memory, _ := NewSharedMemory(1, 1)
		defer memory.Close()
		instance, _ := Instantiate(compiled, Imports{"env.memory": memory})
		defer instance.Close()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := instance.InvokeContext(ctx, "wait32", I32(0), I32(0), I64(-1))
			done <- err
		}()
		waitForMemoryWaiters(t, memory, 1)
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("wait cancellation = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancellation did not wake Wasm wait")
		}
		assertNoMemoryWaiters(t, memory)
	})

	t.Run("close", func(t *testing.T) {
		memory, _ := NewSharedMemory(1, 1)
		defer memory.Close()
		instance, _ := Instantiate(compiled, Imports{"env.memory": memory})
		done := make(chan error, 1)
		go func() {
			_, err := instance.Invoke("wait32", I32(0), I32(0), I64(-1))
			done <- err
		}()
		waitForMemoryWaiters(t, memory, 1)
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapInterrupted {
				t.Fatalf("close wake = %v, want interrupted trap", err)
			}
		case <-time.After(time.Second):
			t.Fatal("instance close did not wake Wasm wait")
		}
		assertNoMemoryWaiters(t, memory)
	})
}
