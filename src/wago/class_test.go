package wago

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// counterModule builds an import-free module with one exported mutable i32 global
// and an exported run() -> i32 that increments the global and returns the new
// value. A fresh instance always starts the global at 0, so reset behavior is
// observable.
func counterModule(t *testing.T) *Module {
	t.Helper()
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	glob := []byte{0x7f, 0x01, 0x41, 0x00, 0x0b} // i32 mutable, init i32.const 0
	body := []byte{
		0x23, 0x00, // global.get 0
		0x41, 0x01, // i32.const 1
		0x6a,       // i32.add
		0x24, 0x00, // global.set 0
		0x23, 0x00, // global.get 0
		0x0b, // end
	}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), // one func, type 0
		wasmtest.Section(6, wasmtest.Vec(glob)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := NewRuntime().Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return m
}

func TestClassAcquireReleaseReset(t *testing.T) {
	rt := NewRuntime()
	mod := counterModule(t)
	class, err := rt.Class(mod, ClassOptions{
		Name: "counter",
		Pool: PoolOptions{MinInstances: 1, MaxInstances: 2, Reset: ResetMemorySnapshot},
	})
	if err != nil {
		t.Fatalf("class: %v", err)
	}
	defer class.Close()

	lease, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	out, err := lease.Instance().Call(context.Background(), "run")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out[0].I32() != 1 {
		t.Fatalf("first run = %d, want 1", out[0].I32())
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	// After release the pooled instance is reset: run() starts from 0 again.
	lease2, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	out, err = lease2.Instance().Call(context.Background(), "run")
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if out[0].I32() != 1 {
		t.Fatalf("run after reset = %d, want 1 (state reset)", out[0].I32())
	}
	lease2.Release()
}

func TestClassMemorySnapshotReusesEligibleInstance(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)))
	defer rt.Close()
	mod, err := rt.Compile(benchClassResetModule(1))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	class, err := rt.Class(mod, ClassOptions{Pool: PoolOptions{
		MinInstances: 1,
		MaxInstances: 1,
		Reset:        ResetMemorySnapshot,
	}})
	if err != nil {
		t.Fatalf("class: %v", err)
	}
	defer class.Close()

	lease, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	first := lease.Instance()
	if _, err := first.Invoke("run"); err != nil {
		t.Fatalf("run: %v", err)
	}
	first.Memory().Bytes()[1] = 0x7f
	if err := lease.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	next, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire after reset: %v", err)
	}
	defer next.Release()
	if next.Instance() != first {
		t.Fatal("ResetMemorySnapshot replaced an eligible one-page instance")
	}
	if got := next.Instance().Memory().Bytes()[1]; got != 0 {
		t.Fatalf("memory after reset = %#x, want zero", got)
	}
	out, err := next.Instance().Invoke("run")
	if err != nil {
		t.Fatalf("run after reset: %v", err)
	}
	if got := AsI32(out[0]); got != 1 {
		t.Fatalf("global after reset produced %d, want 1", got)
	}
}

type resetRequirementExt struct {
	id           string
	module       string
	instantiated atomic.Int32
	closed       atomic.Int32
}

func (e *resetRequirementExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: e.id, Version: "1.0.0", Stability: Stable}
}

func (e *resetRequirementExt) Register(reg *Registry) error {
	reg.RequireReinstantiation()
	reg.Hooks().AfterInstantiate(func(*InstantiateContext, *Instance) error {
		e.instantiated.Add(1)
		return nil
	})
	reg.Hooks().BeforeClose(func(*InstanceContext) { e.closed.Add(1) })
	if e.module != "" {
		reg.ImportModule(e.module).Func("f", func(HostModule, []uint64, []uint64) {})
	}
	return nil
}

func TestClassExtensionRequirementDowngradesMemorySnapshot(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)))
	ext := &resetRequirementExt{id: "test.reset-required"}
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(benchClassResetModule(1))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	class, err := rt.Class(mod, ClassOptions{Pool: PoolOptions{MinInstances: 1, MaxInstances: 1, Reset: ResetMemorySnapshot}})
	if err != nil {
		t.Fatalf("class: %v", err)
	}
	defer class.Close()
	if got := class.ResetPolicy(); got != ResetReinstantiate {
		t.Fatalf("effective reset = %v, want reinstantiate", got)
	}
	lease, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	first := lease.Instance()
	first.Memory().Bytes()[1] = 0x7f
	if err := lease.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	next, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	defer next.Release()
	if next.Instance() == first {
		t.Fatal("extension-required reset reused the physical instance")
	}
	if got := next.Instance().Memory().Bytes()[1]; got != 0 {
		t.Fatalf("fresh memory byte = %#x, want zero", got)
	}
	if got := ext.closed.Load(); got != 1 {
		t.Fatalf("close hooks after release = %d, want 1", got)
	}
	if got := ext.instantiated.Load(); got != 2 {
		t.Fatalf("instantiate hooks after replacement = %d, want 2", got)
	}
}

func TestRejectedExtensionDoesNotChangeClassResetEligibility(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)))
	// Own env without declaring a reset requirement.
	if err := rt.Use(classImportExt{id: "test.reset-module-owner", module: "env"}); err != nil {
		t.Fatalf("Use owner: %v", err)
	}
	rejected := &resetRequirementExt{id: "test.reset-rejected", module: "env"}
	if err := rt.Use(rejected); !errors.Is(err, ErrExtensionConflict) {
		t.Fatalf("Use rejected = %v, want ErrExtensionConflict", err)
	}
	mod, err := rt.Compile(benchClassResetModule(1))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	class, err := rt.Class(mod, ClassOptions{Pool: PoolOptions{MinInstances: 1, MaxInstances: 1, Reset: ResetMemorySnapshot}})
	if err != nil {
		t.Fatalf("class: %v", err)
	}
	defer class.Close()
	if got := class.ResetPolicy(); got != ResetMemorySnapshot {
		t.Fatalf("rejected declaration changed reset to %v", got)
	}
	lease, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	first := lease.Instance()
	if err := lease.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	next, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	defer next.Release()
	if next.Instance() != first {
		t.Fatal("rejected reset declaration disabled eligible snapshot reuse")
	}
}

type classImportExt struct {
	id     string
	module string
}

func (e classImportExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: e.id, Version: "1.0.0", Stability: Stable}
}

func (e classImportExt) Register(reg *Registry) error {
	reg.ImportModule(e.module).Func("f", func(HostModule, []uint64, []uint64) {})
	return nil
}

func TestClassResetRequirementConcurrentUseAndRelease(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)))
	mod, err := rt.Compile(benchClassResetModule(1))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	class, err := rt.Class(mod, ClassOptions{Pool: PoolOptions{MinInstances: 1, MaxInstances: 1, Reset: ResetMemorySnapshot}})
	if err != nil {
		t.Fatalf("class: %v", err)
	}
	defer class.Close()
	lease, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		if err := rt.Use(&resetRequirementExt{id: "test.reset-concurrent"}); err != nil {
			t.Errorf("Use: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		if err := lease.Release(); err != nil {
			t.Errorf("Release: %v", err)
		}
	}()
	close(start)
	wg.Wait()

	if got := class.ResetPolicy(); got != ResetReinstantiate {
		t.Fatalf("effective reset after Use = %v, want reinstantiate", got)
	}
	candidate, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire candidate: %v", err)
	}
	physical := candidate.Instance()
	if err := candidate.Release(); err != nil {
		t.Fatalf("release candidate: %v", err)
	}
	fresh, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire fresh: %v", err)
	}
	defer fresh.Release()
	if fresh.Instance() == physical {
		t.Fatal("class reused an instance after the reset requirement committed")
	}
}

func TestClassMemorySnapshotFallsBackAboveMeasuredCrossover(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)))
	defer rt.Close()
	mod, err := rt.Compile(benchClassResetModule(2))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	class, err := rt.Class(mod, ClassOptions{Pool: PoolOptions{
		MinInstances: 1,
		MaxInstances: 1,
		Reset:        ResetMemorySnapshot,
	}})
	if err != nil {
		t.Fatalf("class: %v", err)
	}
	defer class.Close()

	lease, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	first := lease.Instance()
	if err := lease.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	next, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	defer next.Release()
	if next.Instance() == first {
		t.Fatal("two-page ResetMemorySnapshot did not use the faster reinstantiation fallback")
	}
}

func TestClassCapacityBlocksUntilRelease(t *testing.T) {
	rt := NewRuntime()
	class, err := rt.Class(counterModule(t), ClassOptions{
		Pool: PoolOptions{MaxInstances: 1},
	})
	if err != nil {
		t.Fatalf("class: %v", err)
	}
	defer class.Close()

	l1, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	// At capacity: a second acquire with an already-expired deadline must fail.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := class.Acquire(ctx); err == nil {
		t.Fatal("expected acquire at capacity to time out")
	}
	// After releasing, acquire succeeds.
	if err := l1.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	l2, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	l2.Release()
}

func TestClassRequiresMaxInstances(t *testing.T) {
	rt := NewRuntime()
	if _, err := rt.Class(counterModule(t), ClassOptions{}); err == nil {
		t.Fatal("expected error for MaxInstances <= 0")
	}
}
