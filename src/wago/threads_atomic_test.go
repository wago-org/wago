//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

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

func sharedAtomicMutableGlobalImportModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			memoryImport,
			wasmtest.GlobalImportEntry("env", "global", wasm.I32, true),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0xfe, 0x10, 0x02, 0x00, 0x0b,
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

func sharedThreadedArgumentModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("accept", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func sharedThreadedGlobalHoldModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x07, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("hold", 0, 0),
			wasmtest.ExportEntry("global", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0x41, 0x01, 0xfe, 0x17, 0x02, 0x00, // signal = 1
			0x03, 0x40, // loop
			0x41, 0x04, 0xfe, 0x10, 0x02, 0x00, 0x45, 0x0d, 0x00, // while release == 0
			0x0b,
			0x23, 0x00, // global.get 0
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

func unsharedAtomicWaitNotifyModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})), // unshared memory32, min=1, max=1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("notify", 0, 0),
			wasmtest.ExportEntry("wait32", 0, 1),
			wasmtest.ExportEntry("wait64", 0, 2),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0xfe, 0x00, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfe, 0x01, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfe, 0x02, 0x03, 0x00, 0x0b}),
		)),
	)
}

func importedUnsharedAtomicWaitNotifyModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x01, 0x01, 0x01) // unshared memory32, min=1, max=1
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
	if compiled.usesAtomicWaitHelpers() || len(instance.ctrl) != 0 {
		t.Fatalf("direct-only atomic module retained wait bridge: helper=%v ctrl=%d", compiled.usesAtomicWaitHelpers(), len(instance.ctrl))
	}

	result, err := instance.Invoke("add", I32(7))
	if err != nil {
		t.Fatalf("atomic add: %v", err)
	}
	if old := AsI32(result[0]); old != 0 {
		t.Fatalf("atomic add old value = %d, want 0", old)
	}
	if got := binary.LittleEndian.Uint32(memory.UnsafeBytes()[:4]); got != 7 {
		t.Fatalf("shared memory value = %d, want 7", got)
	}
}

func TestThreadsSameInstanceConcurrentInvokeSerializesScratch(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedThreadedArgumentModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, _ := NewSharedMemory(1, 1)
	defer memory.Close()
	instance, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	const workers, calls = 8, 200
	start := make(chan struct{})
	errs := make(chan error, workers)
	for worker := range workers {
		go func() {
			<-start
			for call := range calls {
				want := uint64(worker+1)<<32 | uint64(call)
				out, err := instance.Invoke("accept", want)
				if err != nil || len(out) != 0 {
					errs <- fmt.Errorf("accept(%#x) = %v, %v", want, out, err)
					return
				}
			}
			errs <- nil
		}()
	}
	close(start)
	for range workers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestThreadsHostGlobalAccessSerializesWithInvoke(t *testing.T) {
	// Generated native code does not participate in Go's asynchronous scheduler.
	// Keep one P available for the controller while the guest deliberately spins;
	// a one-P test process would otherwise deadlock before it can publish release.
	previousProcs := goruntime.GOMAXPROCS(0)
	if previousProcs < 2 {
		goruntime.GOMAXPROCS(2)
		t.Cleanup(func() { goruntime.GOMAXPROCS(previousProcs) })
	}

	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedThreadedGlobalHoldModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, _ := NewSharedMemory(1, 1)
	defer memory.Close()
	instance, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	result := make(chan struct {
		value uint64
		err   error
	}, 1)
	go func() {
		out, err := instance.Invoke("hold")
		var value uint64
		if len(out) != 0 {
			value = out[0]
		}
		result <- struct {
			value uint64
			err   error
		}{value, err}
	}()
	signal := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[0]))
	release := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[4]))
	defer atomic.StoreUint32(release, 1)
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32(signal) == 0 && time.Now().Before(deadline) {
		goruntime.Gosched()
	}
	if atomic.LoadUint32(signal) == 0 {
		t.Fatalf("guest did not enter hold function within 5s: signal=%d release=%d GOMAXPROCS=%d", atomic.LoadUint32(signal), atomic.LoadUint32(release), goruntime.GOMAXPROCS(0))
	}
	setDone := make(chan error, 1)
	go func() { setDone <- instance.SetGlobal("global", I32(99)) }()
	select {
	case err := <-setDone:
		t.Fatalf("SetGlobal completed during threaded invocation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	atomic.StoreUint32(release, 1)
	var got struct {
		value uint64
		err   error
	}
	select {
	case got = <-result:
	case <-time.After(5 * time.Second):
		t.Fatalf("guest did not observe release within 5s: signal=%d release=%d GOMAXPROCS=%d", atomic.LoadUint32(signal), atomic.LoadUint32(release), goruntime.GOMAXPROCS(0))
	}
	if got.err != nil || got.value != 7 {
		t.Fatalf("hold = %d, %v; want original global 7", got.value, got.err)
	}
	select {
	case err := <-setDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("SetGlobal remained blocked after guest exit: signal=%d release=%d GOMAXPROCS=%d", atomic.LoadUint32(signal), atomic.LoadUint32(release), goruntime.GOMAXPROCS(0))
	}
	if value, err := instance.Global("global"); err != nil || value != I32(99) {
		t.Fatalf("global after serialized set = %d, %v", value, err)
	}
}

func TestThreadsRejectsOrdinaryMemoryForSharedImport(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicAddModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, err := NewMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	if instance, err := Instantiate(compiled, Imports{"env.memory": memory}); err == nil {
		instance.Close()
		t.Fatal("shared memory import accepted an ordinary host memory")
	}
}

func TestThreadsRejectsUnderalignedAtomicMemoryArgument(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	if compiled, err := Compile(config, sharedAtomicRMWModule(0x1e, 1, wasm.I32)); err == nil {
		compiled.Close()
		t.Fatal("i32.atomic.rmw.add accepted align=2 instead of its natural align=4")
	}
}

func TestThreadsRejectsMutableGlobalImports(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	if compiled, err := Compile(config, sharedAtomicMutableGlobalImportModule()); err == nil {
		compiled.Close()
		t.Fatal("threaded module accepted a mutable global import")
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
	if !bytes.Equal(base.code, withThreads.code) {
		t.Fatalf("ordinary generated code changed when threads was enabled: %d vs %d bytes", len(base.code), len(withThreads.code))
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
	if got := binary.LittleEndian.Uint32(memory.UnsafeBytes()[:4]); got != 0xdeadbeef {
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
			binary.LittleEndian.PutUint64(memory.UnsafeBytes()[:8], initial)
			instance, err := Instantiate(compiled, Imports{"env.memory": memory})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if _, err := instance.Invoke("store", value); err != nil {
				t.Fatal(err)
			}
			wantMemory := initial&^tc.mask | value&tc.mask
			if got := binary.LittleEndian.Uint64(memory.UnsafeBytes()[:8]); got != wantMemory {
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
			binary.LittleEndian.PutUint64(memory.UnsafeBytes()[:8], tc.old)
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
			if got := binary.LittleEndian.Uint64(memory.UnsafeBytes()[:8]) & tc.memMask; got != tc.want {
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
		{"i64_32", 0x4e, 2, wasm.I64, 0x1111111111111111, 0xffffffff},
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
			binary.LittleEndian.PutUint64(memory.UnsafeBytes()[:8], tc.old)
			instance, err := Instantiate(compiled, Imports{"env.memory": memory})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("cmpxchg", tc.old+1, 0x12345678)
			if err != nil || result[0] != tc.old&tc.mask {
				t.Fatalf("failed cmpxchg old = %v, %v", result, err)
			}
			if got := binary.LittleEndian.Uint64(memory.UnsafeBytes()[:8]) & tc.mask; got != tc.old&tc.mask {
				t.Fatalf("failed cmpxchg changed memory to %#x", got)
			}
			result, err = instance.Invoke("cmpxchg", tc.old, 0x12345678)
			if err != nil || result[0] != tc.old&tc.mask {
				t.Fatalf("successful cmpxchg old = %v, %v", result, err)
			}
			if got := binary.LittleEndian.Uint64(memory.UnsafeBytes()[:8]) & tc.mask; got != 0x12345678&tc.mask {
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
	if got := binary.LittleEndian.Uint32(memory.UnsafeBytes()[:4]); got != 0 {
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
			for i := range memory.UnsafeBytes() {
				memory.UnsafeBytes()[i] = byte(i*131 + 17)
			}
			want := append([]byte(nil), memory.UnsafeBytes()...)
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
				if !bytes.Equal(memory.UnsafeBytes(), want) {
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
	if arrivals := binary.LittleEndian.Uint32(memory.UnsafeBytes()[0:4]); arrivals != 2 {
		t.Fatalf("arrivals = %d, want 2", arrivals)
	}
	if completions := binary.LittleEndian.Uint32(memory.UnsafeBytes()[4:8]); completions != 2 {
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

	binary.LittleEndian.PutUint32(memory.UnsafeBytes()[:4], 7)
	if out, err = waiter.Invoke("wait32", I32(0), I32(8), I64(-1)); err != nil || AsI32(out[0]) != int32(memoryWaitNotEqual) {
		t.Fatalf("mismatched wait32 = %v, %v", out, err)
	}
	if out, err = waiter.Invoke("wait32", I32(0), I32(7), I64(0)); err != nil || AsI32(out[0]) != int32(memoryWaitTimedOut) {
		t.Fatalf("zero-timeout wait32 = %v, %v", out, err)
	}
	binary.LittleEndian.PutUint64(memory.UnsafeBytes()[8:16], 0x1122334455667788)
	if out, err = waiter.Invoke("wait64", I32(8), I64(0x1122334455667788), I64(0)); err != nil || AsI32(out[0]) != int32(memoryWaitTimedOut) {
		t.Fatalf("zero-timeout wait64 = %v, %v", out, err)
	}
	assertNoMemoryWaiters(t, memory)
}

func assertUnsharedAtomicWaitNotifySemantics(t *testing.T, instance *Instance) {
	t.Helper()
	assertTrap := func(t *testing.T, export string, want TrapCode, args ...uint64) {
		t.Helper()
		_, err := instance.Invoke(export, args...)
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != want {
			t.Fatalf("%s%v error = %v, want %s", export, args, err, want)
		}
	}
	out, err := instance.Invoke("notify", I32(0), I32(0))
	if err != nil || len(out) != 1 || AsI32(out[0]) != 0 {
		t.Fatalf("notify = %v, %v; want 0", out, err)
	}
	assertTrap(t, "notify", TrapLinMemOutOfBounds, I32(65536), I32(0))
	assertTrap(t, "notify", TrapAtomicUnaligned, I32(1), I32(0))

	// Alignment is checked before the unshared-memory condition. Once aligned,
	// wait traps for an unshared memory before loading or checking its bounds.
	assertTrap(t, "wait32", TrapAtomicUnaligned, I32(1), I32(1), I64(0))
	assertTrap(t, "wait32", TrapExpectedSharedMemory, I32(65536), I32(1), I64(0))
	assertTrap(t, "wait32", TrapExpectedSharedMemory, I32(0), I32(1), I64(0))
	assertTrap(t, "wait64", TrapAtomicUnaligned, I32(4), I64(1), I64(0))
	assertTrap(t, "wait64", TrapExpectedSharedMemory, I32(65536), I64(1), I64(0))
	assertTrap(t, "wait64", TrapExpectedSharedMemory, I32(0), I64(1), I64(0))
}

func TestThreadsAtomicWaitNotifyOnUnsharedMemory(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, unsharedAtomicWaitNotifyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	t.Run("local", func(t *testing.T) { assertUnsharedAtomicWaitNotifySemantics(t, instance) })
	if _, err := instance.ExportedMemory("memory"); err != nil {
		t.Fatalf("export memory: %v", err)
	}
	t.Run("exported", func(t *testing.T) { assertUnsharedAtomicWaitNotifySemantics(t, instance) })
}

func TestThreadsAtomicWaitNotifyOnImportedUnsharedMemory(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, importedUnsharedAtomicWaitNotifyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, err := NewMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	instance, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	assertUnsharedAtomicWaitNotifySemantics(t, instance)
}

func TestThreadsAtomicWaitHelperAdmissionSurvivesArtifactRoundTrip(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicWaitNotifyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	loaded := publicArtifactRoundTrip(t, compiled)
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
