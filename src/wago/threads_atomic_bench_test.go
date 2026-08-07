//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func BenchmarkThreadsAtomicInvoke(b *testing.B) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, tc := range []struct {
		name   string
		module []byte
		export string
		args   []uint64
	}{
		{"load", sharedAtomicLoadStoreWidthModule(0x10, 0x17, 2, wasm.I32), "load", nil},
		{"store", sharedAtomicLoadStoreWidthModule(0x10, 0x17, 2, wasm.I32), "store", []uint64{1}},
		{"rmw-add", sharedAtomicRMWModule(0x1e, 2, wasm.I32), "rmw", []uint64{1}},
		{"cmpxchg", sharedAtomicCmpxchgModule(0x48, 2, wasm.I32), "cmpxchg", []uint64{0, 0}},
		{"wait-mismatch", sharedAtomicWaitNotifyModule(), "wait32", []uint64{0, 1, ^uint64(0)}},
		{"notify-empty", sharedAtomicWaitNotifyModule(), "notify", []uint64{0, ^uint64(0)}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			compiled, err := Compile(config, tc.module)
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			memory, _ := NewSharedMemory(1, 1)
			defer memory.Close()
			instance, err := Instantiate(compiled, Imports{"env.memory": memory})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := instance.Invoke(tc.export, tc.args...); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(compiled.Code)), "code-B")
		})
	}
}

func BenchmarkThreadsWaitNotifyRoundTrip(b *testing.B) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicWaitNotifyModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	memory, _ := NewSharedMemory(1, 1)
	defer memory.Close()
	waiter, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		b.Fatal(err)
	}
	defer waiter.Close()
	notifier, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		b.Fatal(err)
	}
	defer notifier.Close()
	done := make(chan error, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go func() {
			_, err := waiter.Invoke("wait32", I32(0), I32(0), I64(-1))
			done <- err
		}()
		for {
			s := memory.state.Load()
			s.mu.Lock()
			ws := s.waiterStateLocked(false)
			active := ws != nil && ws.active == 1
			s.mu.Unlock()
			if active {
				break
			}
			runtime.Gosched()
		}
		if _, err := notifier.Invoke("notify", I32(0), I32(1)); err != nil {
			b.Fatal(err)
		}
		if err := <-done; err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkThreadsNativeEntry(b *testing.B) {
	ordinary := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x0b}))),
	)
	b.Run("ordinary", func(b *testing.B) {
		compiled := MustCompile(ordinary)
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			b.Fatal(err)
		}
		defer instance.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := instance.Invoke("run"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("threaded-atomic-load", func(b *testing.B) {
		config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
		compiled, err := Compile(config, sharedAtomicLoadStoreWidthModule(0x10, 0x17, 2, wasm.I32))
		if err != nil {
			b.Fatal(err)
		}
		defer compiled.Close()
		memory, _ := NewSharedMemory(1, 1)
		defer memory.Close()
		instance, err := Instantiate(compiled, Imports{"env.memory": memory})
		if err != nil {
			b.Fatal(err)
		}
		defer instance.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := instance.Invoke("load"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkThreadsCompileAtomic(b *testing.B) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	module := sharedAtomicWaitNotifyModule()
	b.ReportAllocs()
	b.ReportMetric(float64(len(module)), "wasm-B")
	for i := 0; i < b.N; i++ {
		compiled, err := Compile(config, module)
		if err != nil {
			b.Fatal(err)
		}
		if err := compiled.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkThreadsFootprint(b *testing.B) {
	b.ReportMetric(float64(unsafe.Sizeof(Compiled{})), "B/Compiled")
	b.ReportMetric(float64(unsafe.Sizeof(Instance{})), "B/Instance")
	b.ReportMetric(float64(unsafe.Sizeof(Memory{})), "B/Memory")
	b.ReportMetric(float64(unsafe.Sizeof(memoryState{})), "B/memory-state")
	b.ReportMetric(float64(unsafe.Sizeof(memoryWaiterState{})), "B/waiter-state")
	b.ReportMetric(float64(unsafe.Sizeof(memoryWaiter{})), "B/waiter")
}

func TestThreadsWarmedDirectAtomicInvokeAllocatesZero(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicRMWModule(0x1e, 2, wasm.I32))
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
	if _, err := instance.Invoke("rmw", I32(1)); err != nil {
		t.Fatal(err)
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		if _, err := instance.Invoke("rmw", I32(1)); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatalf("warmed direct atomic allocations = %v, want 0", allocs)
	}
}
