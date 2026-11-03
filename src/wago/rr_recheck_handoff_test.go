//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

// Independent RR re-review adversary (rr-invocation-recheck): shared-memory
// handoff from a storage-only signaller to a true-dynamic waiter. Uses context
// invocations so the baseline does not serialize on its global native mutex.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRRRecheckSharedMemoryHandoff(t *testing.T) {
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { memory.Close() })
	rt := NewRuntime()
	tenant, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("a", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0xfb, 0, 0, 0x1a, 0x0b})))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tenant.Close() })
	rrInstantiate(t, tenant, InstantiateOptions{store: rt.refStore, GC: GCConfig{NurseryBytes: 8192}})
	waiter := rrInstantiate(t, rrCompile(t, rrModule(0x70, true, false)), InstantiateOptions{store: rt.refStore, Imports: testImports("env.memory", memory)})
	mem := memory.UnsafeBytes()
	flag := (*uint32)(unsafe.Pointer(&mem[0]))
	entered := (*uint32)(unsafe.Pointer(&mem[4]))
	waiterDone := make(chan error, 1)
	go func() { _, e := waiter.Invoke("run"); waiterDone <- e }()
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32(entered) == 0 {
		if time.Now().After(deadline) {
			atomic.StoreUint32(flag, 1)
			t.Fatal("waiter never entered")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// S1: any storage-only call; it finishes and then releases its admission.
	s1 := rrInstantiate(t, rrCompile(t, rrModule(0x6f, false, false)), InstantiateOptions{store: rt.refStore})
	s1done := make(chan error, 1)
	go func() { _, e := s1.InvokeContext(ctx, "run"); s1done <- e }()
	time.Sleep(500 * time.Millisecond)
	// S2: storage-only signaller sharing the waiter's memory: atomic store [0]=1.
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 3, 1, 1))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("signal", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x41, 1, 0xfe, 0x17, 2, 0, 0x41, 7, 0x0b}))))
	s2 := rrInstantiate(t, rrCompile(t, raw), InstantiateOptions{store: rt.refStore, Imports: testImports("env.memory", memory)})
	s2done := make(chan error, 1)
	go func() { _, e := s2.InvokeContext(ctx, "signal"); s2done <- e }()
	completed := false
	select {
	case err := <-waiterDone:
		completed = true
		if err != nil {
			t.Fatalf("waiter: %v", err)
		}
	case <-time.After(3 * time.Second):
	}
	if !completed {
		atomic.StoreUint32(flag, 1) // break the cycle from Go
		if err := <-waiterDone; err != nil {
			t.Fatalf("waiter: %v", err)
		}
	}
	e1, e2 := <-s1done, <-s2done
	t.Logf("handoffCompleted=%t s1=%v s2=%v", completed, e1, e2)
	if !completed {
		t.Errorf("storage-only signaller could not reach a waiting true-dynamic call (wait cycle)")
	}
	if e1 != nil || e2 != nil {
		t.Errorf("storage-only calls failed: %v / %v", e1, e2)
	}
}
