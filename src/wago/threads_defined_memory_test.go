//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func definedSharedAtomicModule(startTrap bool) []byte {
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2}, []byte{3})),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("add", 0, 0), wasmtest.ExportEntry("wait", 0, 1),
			wasmtest.ExportEntry("notify", 0, 2), wasmtest.ExportEntry("memory", 2, 0),
		)),
	}
	if startTrap {
		sections = append(sections, wasmtest.Section(8, []byte{3}))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0x41, 0, 0x20, 0, 0xfe, 0x1e, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x41, 0, 0x20, 0, 0x20, 1, 0xfe, 1, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x41, 0, 0x20, 0, 0xfe, 0, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0, 0x0b}),
	)))
	return wasmtest.Module(sections...)
}

func TestThreadsDefinedSharedMemoryExecutionAndCodec(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(definedSharedAtomicModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	for _, compiled := range []*Compiled{c, loaded} {
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		memory, err := instance.ExportedMemory("memory")
		if err != nil {
			t.Fatal(err)
		}
		if !instance.ownsMem || instance.jm == memory.jobMemory() {
			t.Fatal("shared pages alias invocation control")
		}
		if got, err := instance.Invoke("add", I32(7)); err != nil || got[0] != 0 {
			t.Fatalf("add = %v, %v", got, err)
		}
		if got, err := instance.Invoke("wait", I32(6), I64(0)); err != nil || got[0] != 1 {
			t.Fatalf("wait mismatch = %v, %v", got, err)
		}
		if got, err := instance.Invoke("wait", I32(7), I64(0)); err != nil || got[0] != 2 {
			t.Fatalf("wait timeout = %v, %v", got, err)
		}
		if got, err := instance.Invoke("notify", I32(1)); err != nil || got[0] != 0 {
			t.Fatalf("notify = %v, %v", got, err)
		}
		if err := memory.Close(); err == nil {
			t.Fatal("exported memory released live instance backing")
		}
		instance.Close()
		if memory.UnsafeBytes() != nil || memory.jobMemory() != nil {
			t.Fatal("closed owner retains shared pages")
		}
	}
}

func TestThreadsDefinedSharedMemorySharesAcrossProducerClose(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	producerCompiled, err := cfg.Compile(definedSharedAtomicModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer producerCompiled.Close()
	consumerCompiled, err := cfg.Compile(sharedAtomicAddModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCompiled.Close()
	producer, err := Instantiate(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	memory, err := producer.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := Instantiate(consumerCompiled, testImports("env.memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, in := range []*Instance{producer, consumer} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				if _, err := in.Invoke("add", I32(1)); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := consumer.Invoke("add", I32(1)); err != nil || got[0] != 2000 {
		t.Fatalf("consumer after producer close = %v, %v", got, err)
	}
	consumer.Close()
	if memory.jobMemory() != nil {
		t.Fatal("last importer did not release producer backing")
	}
}

func TestThreadsDefinedSharedMemoryMappingQuotaAndRollback(t *testing.T) {
	for _, startTrap := range []bool{false, true} {
		for _, limit := range []uint32{1, 2} {
			cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithNativeMemoryMappingLimit(limit)
			rt := NewRuntime(WithRuntimeConfig(cfg))
			mod, err := rt.Compile(definedSharedAtomicModule(startTrap))
			if err != nil {
				t.Fatal(err)
			}
			instance, err := rt.Instantiate(context.Background(), mod)
			if limit == 1 || startTrap {
				if err == nil {
					instance.Close()
					t.Fatal("quota/start failure accepted")
				}
				if stats := rt.ResourceStats(); stats.NativeMemoryMappings != 0 {
					t.Fatalf("failed construction retained mappings: %+v", stats)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if stats := rt.ResourceStats(); stats.NativeMemoryMappings != 2 {
					t.Fatalf("shared backing/control count: %+v", stats)
				}
				instance.Close()
				if stats := rt.ResourceStats(); stats.NativeMemoryMappings != 0 {
					t.Fatalf("closed mappings: %+v", stats)
				}
			}
			mod.Close()
			rt.Close()
		}
	}
}

func TestThreadsDefinedSharedMemoryWaitNotifyAcrossImporter(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	producerCompiled, err := cfg.Compile(definedSharedAtomicModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer producerCompiled.Close()
	consumerCompiled, err := cfg.Compile(sharedAtomicWaitNotifyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCompiled.Close()
	producer, err := Instantiate(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	memory, err := producer.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	notifier, err := Instantiate(consumerCompiled, testImports("env.memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer notifier.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		got, err := producer.InvokeContext(ctx, "wait", I32(0), I64(-1))
		if err == nil && (len(got) != 1 || got[0] != uint64(memoryWaitNotified)) {
			err = fmt.Errorf("wait result = %v", got)
		}
		done <- err
	}()
	waitForMemoryWaiters(t, memory, 1)
	if got, err := notifier.Invoke("notify", I32(0), I32(1)); err != nil || got[0] != 1 {
		t.Fatalf("notify = %v, %v", got, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
