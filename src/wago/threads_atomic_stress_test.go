//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestThreadsAtomicRMWContentionReturnsExactCounter(t *testing.T) {
	const goroutines, increments = 8, 1000
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicAddModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, _ := NewSharedMemory(1, 1)
	defer memory.Close()
	instances := make([]*Instance, goroutines)
	for i := range instances {
		instances[i], err = Instantiate(compiled, Imports{"env.memory": memory})
		if err != nil {
			t.Fatal(err)
		}
		defer instances[i].Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	var ready sync.WaitGroup
	ready.Add(goroutines)
	for _, instance := range instances {
		go func(in *Instance) {
			ready.Done()
			<-start
			for range increments {
				if _, err := in.InvokeContext(ctx, "add", I32(1)); err != nil {
					errs <- err
					return
				}
			}
			errs <- nil
		}(instance)
	}
	ready.Wait()
	close(start)
	for range goroutines {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadUint32((*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[0]))); got != goroutines*increments {
		t.Fatalf("contended RMW counter = %d, want %d", got, goroutines*increments)
	}
}

func TestThreadsAtomicCmpxchgContentionReturnsExactCounter(t *testing.T) {
	const goroutines, increments = 8, 300
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicCmpxchgModule(0x48, 2, wasm.I32))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, _ := NewSharedMemory(1, 1)
	defer memory.Close()
	cell := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[0]))
	instances := make([]*Instance, goroutines)
	for i := range instances {
		instances[i], err = Instantiate(compiled, Imports{"env.memory": memory})
		if err != nil {
			t.Fatal(err)
		}
		defer instances[i].Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	for _, instance := range instances {
		go func(in *Instance) {
			<-start
			for range increments {
				for {
					expected := atomic.LoadUint32(cell)
					out, err := in.InvokeContext(ctx, "cmpxchg", I32(int32(expected)), I32(int32(expected+1)))
					if err != nil {
						errs <- err
						return
					}
					if uint32(out[0]) == expected {
						break
					}
				}
			}
			errs <- nil
		}(instance)
	}
	close(start)
	for range goroutines {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadUint32(cell); got != goroutines*increments {
		t.Fatalf("contended cmpxchg counter = %d, want %d", got, goroutines*increments)
	}
}

func TestThreadsAtomicWaitNotifyBarrier(t *testing.T) {
	const participants = 8
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	addCode, err := Compile(config, sharedAtomicAddModule())
	if err != nil {
		t.Fatal(err)
	}
	defer addCode.Close()
	waitCode, err := Compile(config, sharedAtomicWaitNotifyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer waitCode.Close()
	memory, _ := NewSharedMemory(1, 1)
	defer memory.Close()
	phase := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[4]))
	type pair struct{ add, wait *Instance }
	instances := make([]pair, participants)
	for i := range instances {
		instances[i].add, err = Instantiate(addCode, Imports{"env.memory": memory})
		if err != nil {
			t.Fatal(err)
		}
		defer instances[i].add.Close()
		instances[i].wait, err = Instantiate(waitCode, Imports{"env.memory": memory})
		if err != nil {
			t.Fatal(err)
		}
		defer instances[i].wait.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, participants)
	for _, instance := range instances {
		go func(in pair) {
			<-start
			out, err := in.add.InvokeContext(ctx, "add", I32(1))
			if err != nil {
				errs <- err
				return
			}
			if uint32(out[0]) == participants-1 {
				atomic.StoreUint32(phase, 1)
				_, err = in.wait.InvokeContext(ctx, "notify", I32(4), I32(participants))
			} else {
				out, err = in.wait.InvokeContext(ctx, "wait32", I32(4), I32(0), I64(-1))
				if err == nil && uint32(out[0]) > memoryWaitNotEqual {
					err = fmt.Errorf("barrier wait returned code %d", out[0])
				}
			}
			errs <- err
		}(instance)
	}
	close(start)
	for range participants {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadUint32(phase); got != 1 {
		t.Fatalf("barrier phase = %d, want 1", got)
	}
	assertNoMemoryWaiters(t, memory)
}

func TestThreadsRepeatedInstantiateCloseReclaimsImporterState(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, sharedAtomicAddModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, _ := NewSharedMemory(1, 1)
	for i := 0; i < 250; i++ {
		instance, err := Instantiate(compiled, Imports{"env.memory": memory})
		if err != nil {
			t.Fatalf("cycle %d instantiate: %v", i, err)
		}
		if _, err := instance.Invoke("add", I32(1)); err != nil {
			t.Fatalf("cycle %d invoke: %v", i, err)
		}
		if err := instance.Close(); err != nil {
			t.Fatalf("cycle %d close: %v", i, err)
		}
	}
	s := memory.state.Load()
	s.mu.Lock()
	importers := s.importerCount()
	s.mu.Unlock()
	if importers != 0 {
		t.Fatalf("importer count after cycles = %d", importers)
	}
	if err := memory.Close(); err != nil {
		t.Fatal(err)
	}
}
