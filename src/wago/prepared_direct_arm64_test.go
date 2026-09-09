//go:build arm64 && !tinygo && (linux || darwin || windows)

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

func TestPreparedDirectARM64IgnoresUnusedModuleMemory(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x00})), // one zero-page memory
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("add", 0, 0),
			wasmtest.ExportEntry("size", 0, 1),
			wasmtest.ExportEntry("call_size", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("memory-independent function did not select the ARM64 direct prepared entry")
	}
	if !compiled.directPreparedLightAt(0) {
		t.Fatal("caller-clobber-only function did not select the light ARM64 entry thunk")
	}
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("straight-line caller-clobber-only function did not select the bounded ARM64 entry thunk")
	}
	if compiled.directPreparedAt(1) {
		t.Fatal("memory.size function selected the ARM64 direct prepared entry")
	}
	if compiled.directPreparedAt(2) {
		t.Fatal("function calling memory.size selected the ARM64 direct prepared entry")
	}
	directRollback, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-direct-entry", false), module)
	if err != nil {
		t.Fatalf("compile direct rollback: %v", err)
	}
	if directRollback.directPreparedAt(0) || directRollback.directPreparedLightAt(0) || directRollback.directPreparedBoundedAt(0) {
		t.Fatalf("direct rollback retained direct/light/bounded metadata = %v/%v/%v", directRollback.directPreparedAt(0), directRollback.directPreparedLightAt(0), directRollback.directPreparedBoundedAt(0))
	}
	rollbackInstance, err := Instantiate(directRollback, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate direct rollback: %v", err)
	}
	defer rollbackInstance.Close()
	rollbackFunction, err := rollbackInstance.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare direct rollback: %v", err)
	}
	if rollbackFunction.directIntFast || rollbackFunction.directIntLight || rollbackFunction.directIntBounded {
		t.Fatalf("direct rollback prepared direct/light/bounded path = %v/%v/%v", rollbackFunction.directIntFast, rollbackFunction.directIntLight, rollbackFunction.directIntBounded)
	}
	if got, err := rollbackFunction.Invoke2(20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("direct rollback add(20,22) = %v, %v; want 42", got, err)
	}
	rollback, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-light-entry", false), module)
	if err != nil {
		t.Fatalf("compile rollback: %v", err)
	}
	if !rollback.directPreparedAt(0) || rollback.directPreparedLightAt(0) || !rollback.directPreparedBoundedAt(0) {
		t.Fatalf("light rollback direct/light/bounded selection = %v/%v/%v, want true/false/true", rollback.directPreparedAt(0), rollback.directPreparedLightAt(0), rollback.directPreparedBoundedAt(0))
	}
	boundedRollback, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-bounded-entry", false), module)
	if err != nil {
		t.Fatalf("compile bounded rollback: %v", err)
	}
	if !boundedRollback.directPreparedAt(0) || !boundedRollback.directPreparedLightAt(0) || boundedRollback.directPreparedBoundedAt(0) {
		t.Fatalf("bounded rollback direct/light/bounded selection = %v/%v/%v, want true/true/false", boundedRollback.directPreparedAt(0), boundedRollback.directPreparedLightAt(0), boundedRollback.directPreparedBoundedAt(0))
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
	if !fn.directIntFast {
		t.Fatal("memory-independent function did not prepare the ARM64 direct integer entry")
	}
	if !fn.directIntLight {
		t.Fatal("memory-independent function did not retain the light entry proof")
	}
	if !fn.directIntBounded {
		t.Fatal("memory-independent function did not retain the bounded entry proof")
	}
	got, err := fn.Invoke2(20, 22)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("add(20,22) = %v, %v; want 42", got, err)
	}
}

func TestPreparedDirectARM64CallIndirectAndTrapRecovery(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), callIndirectModule(2, 1, 2))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("call_indirect caller did not select the ARM64 direct prepared entry")
	}
	if compiled.directPreparedLightAt(0) {
		t.Fatal("call_indirect caller selected caller-clobber-only entry thunk")
	}
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("acyclic immutable-table dispatch did not select bounded entry thunk")
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
	if !fn.directIntFast || !fn.isolatedFast {
		t.Fatalf("direct/isolated selection = %v/%v, want true/true", fn.directIntFast, fn.isolatedFast)
	}
	if !fn.directIntBounded {
		t.Fatal("acyclic immutable-table dispatch did not retain bounded entry proof")
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
		t.Fatal("out-of-bounds direct prepared call_indirect did not trap")
	}
	if got, err := fn.Invoke(0, 20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("call after trap = %v, %v; want 42", got, err)
	}
}

func TestPreparedDirectARM64ContinuationTrapRecovery(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      []byte
		wantLight bool
	}{
		{
			name:      "light",
			code:      wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6d, 0x03, 0x40, 0x0b, 0x0b}),
			wantLight: true,
		},
		{
			name: "full-save",
			code: func() []byte {
				body := []byte{
					0x01, 0x01, 0x7f, // one i32 local
					0x20, 0x00, 0x21, 0x02,
					0x20, 0x02, 0x20, 0x01, 0x6d,
					0x03, 0x40, 0x0b, // an empty loop makes scheduler retention ineligible
					0x0b,
				}
				return append(wasmtest.ULEB(uint32(len(body))), body...)
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("div", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(tc.code)),
			)
			compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if !compiled.directPreparedAt(0) || compiled.directPreparedLightAt(0) != tc.wantLight || compiled.directPreparedBoundedAt(0) {
				t.Fatalf("direct/light/bounded selection = %v/%v/%v, want true/%v/false",
					compiled.directPreparedAt(0), compiled.directPreparedLightAt(0), compiled.directPreparedBoundedAt(0), tc.wantLight)
			}
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			defer in.Close()
			fn, err := in.PrepareFunction("div")
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if !fn.directIntFast || fn.directIntLight != tc.wantLight || fn.directIntBounded {
				t.Fatalf("prepared direct/light/bounded path = %v/%v/%v, want true/%v/false",
					fn.directIntFast, fn.directIntLight, fn.directIntBounded, tc.wantLight)
			}
			if _, err := fn.Invoke2(I32(7), I32(0)); err == nil {
				t.Fatal("division by zero did not unwind through the prepared continuation")
			}
			got, err := fn.Invoke2(I32(8), I32(2))
			if err != nil || len(got) != 1 || AsI32(got[0]) != 4 {
				t.Fatalf("post-trap div(8,2) = %v, %v; want [4], nil", got, err)
			}
		})
	}
}

func TestPreparedDirectARM64BoundedEntryRejectsLoop(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) || !compiled.directPreparedLightAt(0) {
		t.Fatalf("loop direct/light selection = %v/%v, want true/true", compiled.directPreparedAt(0), compiled.directPreparedLightAt(0))
	}
	if compiled.directPreparedBoundedAt(0) {
		t.Fatal("loop selected the no-scheduler-release bounded entry")
	}
}

func TestPreparedDirectARM64BoundedEntryRejectsRecursiveCallGraph(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("recursive function did not retain ordinary scheduler-releasing direct entry")
	}
	if compiled.directPreparedBoundedAt(0) {
		t.Fatal("recursive call graph selected no-scheduler-release bounded entry")
	}
}

func TestPreparedDirectARM64BoundedEntryRejectsInlinedCall(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(1) {
		t.Fatal("inlined caller did not retain ordinary scheduler-releasing direct entry")
	}
	if compiled.directPreparedBoundedAt(1) {
		t.Fatal("caller admitted to bounded entry after inlining")
	}
}

func TestPreparedDirectARM64BoundedEntryRejectsBulkAndTableMutation(t *testing.T) {
	tableSection := func() []byte {
		table := append([]byte{0x70, 0x01}, wasmtest.ULEB(1)...)
		table = append(table, wasmtest.ULEB(65535)...)
		return wasmtest.Section(4, wasmtest.Vec(table))
	}
	tableModule := func(body []byte, sections ...[]byte) []byte {
		moduleSections := [][]byte{
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			tableSection(),
		}
		moduleSections = append(moduleSections, sections...)
		moduleSections = append(moduleSections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		return wasmtest.Module(moduleSections...)
	}

	tests := []struct {
		name       string
		module     []byte
		wantDirect bool
	}{
		{name: "memory.copy", module: benchBulkMemoryModule(0x0a)},
		{name: "memory.fill", module: benchBulkMemoryModule(0x0b)},
		{name: "table.copy", module: tableModule([]byte{0x41, 0x00, 0x41, 0x00, 0x20, 0x00, 0xfc, 0x0e, 0x00, 0x00, 0x0b}), wantDirect: true},
		{name: "table.init", module: tableModule(
			[]byte{0x41, 0x00, 0x41, 0x00, 0x20, 0x00, 0xfc, 0x0c, 0x00, 0x00, 0x0b},
			wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElem(0))),
		), wantDirect: true},
		{name: "table.grow", module: tableModule([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x1a, 0x0b}), wantDirect: true},
		{name: "table.fill", module: tableModule([]byte{0x41, 0x00, 0xd0, 0x70, 0x20, 0x00, 0xfc, 0x11, 0x00, 0x0b}), wantDirect: true},
	}
	configs := []struct {
		name   string
		bounds BoundsCheckMode
	}{
		{name: "explicit", bounds: BoundsChecksExplicit},
	}
	if GuardPageSupported() {
		configs = append(configs, struct {
			name   string
			bounds BoundsCheckMode
		}{name: "signals", bounds: BoundsChecksSignalsBased})
	}
	for _, config := range configs {
		for _, tc := range tests {
			t.Run(config.name+"/"+tc.name, func(t *testing.T) {
				compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(config.bounds), tc.module)
				if err != nil {
					t.Fatalf("compile: %v", err)
				}
				if compiled.directPreparedAt(0) != tc.wantDirect {
					t.Fatalf("ordinary direct entry = %v, want %v", compiled.directPreparedAt(0), tc.wantDirect)
				}
				if compiled.directPreparedBoundedAt(0) {
					t.Fatal("bulk or table-mutating function selected the no-scheduler-release bounded entry")
				}
			})
		}
	}
}

func TestPreparedDirectARM64BoundedEntryAllowsFullRegisterSave(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 local
		0x20, 0x00, // local.get 0
		0x21, 0x01, // local.set 1
		0x20, 0x01, // local.get 1
		0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) || compiled.directPreparedLightAt(0) || !compiled.directPreparedBoundedAt(0) {
		t.Fatalf("declared-local direct/light/bounded selection = %v/%v/%v, want true/false/true", compiled.directPreparedAt(0), compiled.directPreparedLightAt(0), compiled.directPreparedBoundedAt(0))
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
	if !fn.directIntBounded || fn.directIntLight {
		t.Fatalf("prepared bounded/light selection = %v/%v, want true/false", fn.directIntBounded, fn.directIntLight)
	}
	got, err := fn.Invoke1(I32(42))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("f(42) = %v, %v; want [42], nil", got, err)
	}
}

func TestPreparedDirectARM64BoundedEntryAllowsGCProgress(t *testing.T) {
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
