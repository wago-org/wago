package wago

import (
	"context"
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
