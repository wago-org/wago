//go:build amd64 && !tinygo && (linux || darwin || windows)

package wago

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestPreparedIntCallBlockDefaultAMD64(t *testing.T) {
	if preparedIntCallBlockDefault {
		t.Fatal("AMD64 call block defaults on; the value-argument thunk is faster")
	}
}

func TestPreparedBoundedAMD64SelectionAndExecution(t *testing.T) {
	beforeCallBlock := preparedIntCallBlockEnabled
	beforePreboundContext := preparedIntPreboundContextEnabled
	preparedIntCallBlockEnabled = true
	preparedIntPreboundContextEnabled = true
	defer func() {
		preparedIntCallBlockEnabled = beforeCallBlock
		preparedIntPreboundContextEnabled = beforePreboundContext
	}()

	add := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), add)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) || !compiled.directPreparedBoundedAt(0) {
		t.Fatalf("straight-line add direct/bounded = %v/%v, want true/true", compiled.directPreparedAt(0), compiled.directPreparedBoundedAt(0))
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntFast || !fn.directIntBounded {
		t.Fatalf("prepared direct/bounded = %v/%v, want true/true", fn.directIntFast, fn.directIntBounded)
	}
	if fn.directIntMode != preparedIntCallBlock {
		t.Fatal("bounded prepared function did not select the immutable call block")
	}
	if fn.directIntMode == preparedIntCallPrebound {
		t.Fatal("explicit call-block override also selected prebound-context entry")
	}
	if got, err := fn.Invoke2(20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("add(20,22) = %v, %v; want 42", got, err)
	}

	preparedIntCallBlockEnabled = false
	prebound, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare call-block rollback: %v", err)
	}
	if prebound.directIntMode == preparedIntCallBlock {
		t.Fatal("call-block rollback retained the call block")
	}
	if prebound.directIntMode != preparedIntCallPrebound {
		t.Fatal("call-block rollback did not select prebound-context entry")
	}
	if got, err := prebound.Invoke2(20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("call-block rollback add(20,22) = %v, %v; want 42", got, err)
	}

	preparedIntPreboundContextEnabled = false
	legacy, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare prebound-context rollback: %v", err)
	}
	if legacy.directIntMode == preparedIntCallBlock {
		t.Fatal("prebound-context rollback selected a call block")
	}
	if legacy.directIntMode == preparedIntCallPrebound {
		t.Fatal("prebound-context rollback retained prebound entry")
	}
	if got, err := legacy.Invoke2(20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("prebound-context rollback add(20,22) = %v, %v; want 42", got, err)
	}

	rollback, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-bounded-entry", false), add)
	if err != nil {
		t.Fatalf("compile rollback: %v", err)
	}
	if !rollback.directPreparedAt(0) || rollback.directPreparedBoundedAt(0) {
		t.Fatalf("rollback direct/bounded = %v/%v, want true/false", rollback.directPreparedAt(0), rollback.directPreparedBoundedAt(0))
	}
	directRollback, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-direct-entry", false), add)
	if err != nil {
		t.Fatalf("compile direct rollback: %v", err)
	}
	if directRollback.directPreparedAt(0) || directRollback.directPreparedBoundedAt(0) {
		t.Fatalf("direct rollback direct/bounded = %v/%v, want false/false", directRollback.directPreparedAt(0), directRollback.directPreparedBoundedAt(0))
	}
}

func TestPreparedBoundedAMD64AllowsGCProgress(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), benchAddOneModule())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntBounded {
		t.Fatal("test function did not select bounded entry")
	}

	var stop atomic.Bool
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		for !stop.Load() {
			got, err := fn.Invoke1(I32(41))
			if err != nil {
				done <- err
				return
			}
			if len(got) != 1 || AsI32(got[0]) != 42 {
				done <- fmt.Errorf("bounded invoke = %v, want [42]", got)
				return
			}
		}
		done <- nil
	}()
	<-started
	for range 8 {
		runtime.GC()
	}
	stop.Store(true)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bounded native entry prevented GC or goroutine progress")
	}
}

func TestPreparedBoundedAMD64RejectsLoop(t *testing.T) {
	loop := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("loop", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), loop)
	if err != nil {
		t.Fatalf("compile loop: %v", err)
	}
	if !compiled.directPreparedAt(0) || compiled.directPreparedBoundedAt(0) {
		t.Fatalf("loop direct/bounded = %v/%v, want true/false", compiled.directPreparedAt(0), compiled.directPreparedBoundedAt(0))
	}
}

func TestPreparedBoundedAMD64RejectsInlinedCall(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("caller", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if compiled.directPreparedBoundedAt(1) {
		t.Fatal("caller admitted to bounded entry after inlining")
	}
}

func TestPreparedBoundedAMD64CallIndirectAndTrapRecovery(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), callIndirectModule(2, 1, 2))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) || !compiled.directPreparedBoundedAt(0) {
		t.Fatalf("call_indirect caller direct/bounded = %v/%v, want true/true", compiled.directPreparedAt(0), compiled.directPreparedBoundedAt(0))
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("caller")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntFast || !fn.directIntBounded || !fn.isolatedFast {
		t.Fatalf("direct/bounded/isolated = %v/%v/%v, want true/true/true", fn.directIntFast, fn.directIntBounded, fn.isolatedFast)
	}
	for _, tc := range []struct {
		idx, want uint64
	}{{0, 13}, {1, 7}} {
		got, err := fn.Invoke(tc.idx, 10, 3)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatalf("caller(%d,10,3) = %v, %v; want %d", tc.idx, got, err, tc.want)
		}
	}
	if _, err := fn.Invoke(2, 10, 3); err == nil {
		t.Fatal("out-of-bounds bounded call_indirect did not trap")
	}
	if got, err := fn.Invoke(0, 20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("call after trap = %v, %v; want 42", got, err)
	}
}
