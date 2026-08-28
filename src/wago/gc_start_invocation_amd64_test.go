//go:build linux && amd64

package wago

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcAllocatingHostStartModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	startType := wasmtest.FuncType(nil, nil)
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("started")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc), 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, startType)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(8, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x10, 0x00, 0x0b}),
		)),
	)
}

func TestGCAllocatingLocalStartWaitsForInvocationLease(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcAllocatingHostStartModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.genericGCFrameRoots() == nil {
		t.Fatal("allocating local start has no exact native root map")
	}
	store := newReferenceStore(false)
	defer store.closeRuntime()
	gcCfg := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	first, err := instantiateCore(compiled, InstantiateOptions{
		GC:      gcCfg,
		store:   store,
		Imports: Imports{"env.started": HostFunc(func(HostModule, []uint64, []uint64) {})},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	lease := first.lockGCInvocation(newInvocationID())
	started := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		second, instantiateErr := instantiateCore(compiled, InstantiateOptions{
			GC:    gcCfg,
			store: store,
			Imports: Imports{"env.started": HostFunc(func(HostModule, []uint64, []uint64) {
				started <- struct{}{}
			})},
		})
		if second != nil {
			_ = second.Close()
		}
		done <- instantiateErr
	}()
	select {
	case <-started:
		lease.unlock()
		t.Fatal("allocating local start ran while another invocation owned the GC domain")
	case err := <-done:
		lease.unlock()
		t.Fatalf("instantiation completed before the GC lease was released: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	lease.unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("allocating local start did not resume after the GC lease was released")
	}
	select {
	case <-started:
	default:
		t.Fatal("allocating local start did not reach its host callback")
	}
}

func TestGCAllocatingLocalStartCancelsInvocationLeaseWait(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcAllocatingHostStartModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	gcCfg := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	first, err := instantiateCore(compiled, InstantiateOptions{
		GC:      gcCfg,
		store:   store,
		Imports: Imports{"env.started": HostFunc(func(HostModule, []uint64, []uint64) {})},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	lease := first.lockGCInvocation(newInvocationID())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		second, instantiateErr := instantiateCore(compiled, InstantiateOptions{
			Context: ctx,
			GC:      gcCfg,
			store:   store,
			Imports: Imports{"env.started": HostFunc(func(HostModule, []uint64, []uint64) {
				t.Error("canceled local start reached its host callback")
			})},
		})
		if second != nil {
			_ = second.Close()
		}
		done <- instantiateErr
	}()
	select {
	case err := <-done:
		lease.unlock()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("GC invocation-lease wait error = %v, want context deadline", err)
		}
	case <-time.After(time.Second):
		lease.unlock()
		<-done
		t.Fatal("instantiation did not cancel while the GC invocation lease was held")
	}
}

func TestGCAllocatingLocalStartCancelsResumeLeaseWait(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcAllocatingHostStartModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	gcCfg := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	first, err := instantiateCore(compiled, InstantiateOptions{
		GC:      gcCfg,
		store:   store,
		Imports: Imports{"env.started": HostFunc(func(HostModule, []uint64, []uint64) {})},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	hostEntered := make(chan struct{})
	leaseHeld := make(chan struct{})
	releaseLease := make(chan struct{})
	competitorDone := make(chan struct{})
	go func() {
		defer close(competitorDone)
		select {
		case <-hostEntered:
		case <-releaseLease:
			return
		}
		lease := first.lockGCInvocation(newInvocationID())
		close(leaseHeld)
		<-releaseLease
		lease.unlock()
	}()
	defer func() {
		close(releaseLease)
		<-competitorDone
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	var startedInstance *Instance
	go func() {
		second, instantiateErr := instantiateCore(compiled, InstantiateOptions{
			Context: ctx,
			GC:      gcCfg,
			store:   store,
			Imports: Imports{"env.started": HostFunc(func(caller HostModule, _ []uint64, _ []uint64) {
				startedInstance = caller.(instanceHostModule).in
				close(hostEntered)
				<-leaseHeld
			})},
		})
		if second != nil {
			_ = second.Close()
		}
		done <- instantiateErr
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("GC resume-lease wait error = %v, want context deadline", err)
		}
		nativeActiveMu.Lock()
		for activation, count := range abandonedGCInvocations {
			if activation.in == startedInstance && count != 0 {
				nativeActiveMu.Unlock()
				t.Fatalf("abandoned GC invocation count = %d after unwind, want 0", count)
			}
		}
		nativeActiveMu.Unlock()
	case <-time.After(time.Second):
		t.Fatal("instantiation did not cancel while host resume waited for the GC lease")
	}
}
