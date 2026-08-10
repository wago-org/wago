//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func crossInstanceTrapCode(err error) TrapCode {
	var trap *TrapError
	if errors.As(err, &trap) {
		return trap.Code
	}
	return TrapNone
}

func TestHostMediatedCrossInstanceCallRestoresCallerMemoryContext(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	ctx := context.Background()
	memoryOwner, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module (memory (export "memory") 1))`))
	if err != nil {
		t.Fatal(err)
	}
	defer memoryOwner.Close()
	callerMemory, err := memoryOwner.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	var caller *Instance

	callee, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(import "env" "host" (func $host))
		(memory (export "memory") 1)
		(func (export "ping") (call $host)))`), WithImports(Imports{
		"env.host": HostFunc(func(mod HostModule, _ []uint64, _ []uint64) {
			if _, callErr := caller.InvokeFromHost(ctx, mod, "middle"); callErr != nil {
				panic(HostTrap{Err: callErr})
			}
		}),
	}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer callee.Close()
	calleeMemory, err := callee.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	calleeMemory.Bytes()[1000+6159], calleeMemory.Bytes()[10000+6159] = 9, 8

	caller, err = rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(import "env" "ping" (func $ping))
		(import "env" "nested" (func $nested))
		(import "env" "memory" (memory 1))
		(func (export "run") (result i32)
			(local $src i32) (local $dst i32)
			(i32.const 1000) (local.set $src)
			(i32.const 10000) (local.set $dst)
			(i32.store8 (i32.const 7159) (i32.const 0))
			(i32.store8 (i32.const 16159) (i32.const 2))
			(call $ping)
			(memory.copy (local.get $dst) (local.get $src) (i32.const 6160))
			(i32.load8_u (i32.const 16159)))
		(func (export "middle") (call $nested))
		(func (export "alloc")))`), WithImports(Imports{
		"env.ping": HostFunc(func(mod HostModule, _, _ []uint64) {
			if _, callErr := callee.InvokeFromHost(ctx, mod, "ping"); callErr != nil {
				panic(HostTrap{Err: callErr})
			}
		}),
		"env.nested": HostFunc(func(mod HostModule, _ []uint64, _ []uint64) {
			if _, callErr := caller.InvokeFromHost(ctx, mod, "alloc"); callErr != nil {
				panic(HostTrap{Err: callErr})
			}
		}),
		"env.memory": callerMemory,
	}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()

	got, err := caller.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || AsI32(got[0]) != 0 {
		t.Fatalf("run = %v, want caller memory byte 0", got)
	}
	if calleeMemory.Bytes()[10000+6159] != 8 {
		t.Fatalf("callee memory was modified after returning to caller: byte = %d", calleeMemory.Bytes()[10000+6159])
	}
}

func TestCrossInstanceImportedTablePreservesFourArguments(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	ctx := context.Background()

	target, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(memory (export "mem") 1)
		(func (export "target") (param i32 i32 i32 i32) (result i32)
			(i32.store (i32.const 0) (local.get 0))
			(i32.store (i32.const 4) (local.get 1))
			(i32.store (i32.const 8) (local.get 2))
			(i32.store (i32.const 12) (local.get 3))
			(local.get 2)))`))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	mem, err := target.ExportedMemory("mem")
	if err != nil {
		t.Fatal(err)
	}
	fn, err := target.ExportedFunc("target")
	if err != nil {
		t.Fatal(err)
	}

	tableOwner, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(type $sig (func (param i32 i32 i32 i32) (result i32)))
		(import "env" "mem" (memory 1))
		(import "env" "target" (func $target (type $sig)))
		(table (export "table") 1 funcref)
		(elem (i32.const 0) func $target))`), WithImports(Imports{"env.mem": mem, "env.target": fn}))
	if err != nil {
		t.Fatal(err)
	}
	defer tableOwner.Close()
	table, err := tableOwner.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}

	caller, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(type $sig (func (param i32 i32 i32 i32) (result i32)))
		(import "env" "mem" (memory 1))
		(import "env" "table" (table 1 funcref))
		(func (export "call") (result i32)
			(call_indirect (type $sig)
				(i32.const 11) (i32.const 22) (i32.const 33) (i32.const 44)
				(i32.const 0))))`), WithImports(Imports{"env.mem": mem, "env.table": table}))
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()

	got, err := caller.Invoke("call")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || AsI32(got[0]) != 33 {
		t.Fatalf("result = %v, want 33", got)
	}
	for i, want := range []uint32{11, 22, 33, 44} {
		if got := binary.LittleEndian.Uint32(mem.Bytes()[i*4:]); got != want {
			t.Fatalf("argument %d = %d, want %d", i, got, want)
		}
	}
}

func TestCrossInstanceImportedTableTargetMayCallHost(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	ctx := context.Background()
	target, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(import "env" "host" (func $host (param i32) (result i32)))
		(memory (export "mem") 1)
		(func (export "target") (param i32 i32 i32 i32) (result i32)
			(local.get 2) (call $host))
		(func (export "void-target") (param i32 i32 i32 i32)
			(local.get 2) (call $host) drop))`), WithImports(Imports{"env.host": HostFunc(func(_ HostModule, params, results []uint64) { results[0] = params[0] })}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	mem, err := target.ExportedMemory("mem")
	if err != nil {
		t.Fatal(err)
	}
	fn, err := target.ExportedFunc("target")
	if err != nil {
		t.Fatal(err)
	}
	voidFn, err := target.ExportedFunc("void-target")
	if err != nil {
		t.Fatal(err)
	}
	tableOwner, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(type $sig (func (param i32 i32 i32 i32) (result i32)))
		(type $void-sig (func (param i32 i32 i32 i32)))
		(import "env" "mem" (memory 1))
		(import "env" "target" (func $target (type $sig)))
		(import "env" "void-target" (func $void-target (type $void-sig)))
		(table (export "table") 1 funcref)
		(table (export "void-table") 1 funcref)
		(elem (table 0) (i32.const 0) func $target)
		(elem (table 1) (i32.const 0) func $void-target))`), WithImports(Imports{"env.mem": mem, "env.target": fn, "env.void-target": voidFn}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer tableOwner.Close()
	table, err := tableOwner.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	voidTable, err := tableOwner.ExportedTable("void-table")
	if err != nil {
		t.Fatal(err)
	}
	caller, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(type $sig (func (param i32 i32 i32 i32) (result i32)))
		(import "env" "mem" (memory 1))
		(import "env" "table" (table 1 funcref))
		(func (export "call") (result i32)
			(call_indirect (type $sig) (i32.const 11) (i32.const 22) (i32.const 33) (i32.const 44) (i32.const 0))))`), WithImports(Imports{"env.mem": mem, "env.table": table}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	got, err := caller.Invoke("call")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || AsI32(got[0]) != 33 {
		t.Fatalf("result = %v, want 33", got)
	}
	callerFn, err := caller.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	outer, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(import "env" "call" (func $call (result i32)))
		(func (export "call") (result i32)
			(call $call)
			(i32.const 9)
			(i32.add)))`), WithImports(Imports{"env.call": callerFn}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	got, err = outer.Invoke("call")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("nested table result = %v, want 42", got)
	}
	voidCaller, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(type $sig (func (param i32 i32 i32 i32)))
		(import "env" "mem" (memory 1))
		(import "env" "table" (table 1 funcref))
		(func (export "call") (result i32)
			(call_indirect (type $sig) (i32.const 11) (i32.const 22) (i32.const 33) (i32.const 44) (i32.const 0))
			(i32.const 55)))`), WithImports(Imports{"env.mem": mem, "env.table": voidTable}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer voidCaller.Close()
	got, err = voidCaller.Invoke("call")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || AsI32(got[0]) != 55 {
		t.Fatalf("void result = %v, want 55", got)
	}
}

func TestNestedCrossInstanceCallsMayResumeAfterHostCall(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	ctx := context.Background()

	hostCalls := 0
	memoryOwner, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module (memory (export "memory") 1))`), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer memoryOwner.Close()
	sharedMemory, err := memoryOwner.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	c, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(import "env" "host" (func $host (param i32) (result i32)))
		(import "env" "memory" (memory 1))
		(func (export "call") (param i32) (result i32)
			(local.get 0)
			(call $host)
			(i32.const 3)
			(i32.add)))`), WithImports(Imports{"env.host": HostFunc(func(_ HostModule, params, results []uint64) {
		hostCalls++
		results[0] = params[0]
	}), "env.memory": sharedMemory}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cCall, err := c.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}

	b, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(import "env" "next" (func $next (param i32) (result i32)))
		(import "env" "memory" (memory 1))
		(func (export "call") (param i32) (result i32)
			(local.get 0)
			(i32.const 2)
			(i32.add)
			(call $next)
			(i32.const 5)
			(i32.add)))`), WithImports(Imports{"env.next": cCall, "env.memory": sharedMemory}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	bCall, err := b.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}

	a, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(import "env" "next" (func $next (param i32) (result i32)))
		(import "env" "memory" (memory 1))
		(func (export "call") (param i32) (result i32)
			(local.get 0)
			(call $next)
			(i32.const 7)
			(i32.add)))`), WithImports(Imports{"env.next": bCall, "env.memory": sharedMemory}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	got, err := a.Invoke("call", I32(11))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || AsI32(got[0]) != 28 {
		t.Fatalf("result = %v, want 28", got)
	}
	if hostCalls != 1 {
		t.Fatalf("host calls = %d, want 1", hostCalls)
	}
}

func TestCrossInstanceCycleMayResumeAfterHostCall(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	ctx := context.Background()

	memoryOwner, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module (memory (export "memory") 1))`), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer memoryOwner.Close()
	sharedMemory, err := memoryOwner.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	shim, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(type $callback (func (param i32 i32 i32 i32) (result i32)))
		(import "env" "memory" (memory 1))
		(table (export "table") 1 funcref)
		(func (export "call") (param i32 i32 i32 i32) (result i32)
			(local.get 0) (local.get 1) (local.get 2) (local.get 3)
			(i32.const 0)
			(call_indirect (type $callback))))`), WithImports(Imports{"env.memory": sharedMemory}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer shim.Close()
	table, err := shim.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	shimCall, err := shim.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	middle, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(import "env" "shim" (func $shim (param i32 i32 i32 i32) (result i32)))
		(import "env" "memory" (memory 1))
		(func (export "call") (param i32 i32 i32 i32) (result i32)
			(local.get 0) (local.get 1) (local.get 2) (local.get 3)
			(call $shim)
			(i32.const 5)
			(i32.add))
		(func (export "allocate") (param i32) (result i32)
			(local.get 0)
			(i32.const 1)
			(i32.add)))`), WithImports(Imports{"env.shim": shimCall, "env.memory": sharedMemory}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer middle.Close()
	middleCall, err := middle.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	middleAllocate, err := middle.ExportedFunc("allocate")
	if err != nil {
		t.Fatal(err)
	}
	hostCalls := 0
	adapter, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(import "env" "middle" (func $middle (param i32 i32 i32 i32) (result i32)))
		(import "env" "allocate" (func $allocate (param i32) (result i32)))
		(import "env" "host" (func $host (param i32 i32 i32 i32)))
		(import "env" "memory" (memory 1))
		(func (export "run") (result i32)
			(i32.const 11) (i32.const 22) (i32.const 33) (i32.const 44)
			(call $middle)
			(i32.const 7)
			(i32.add))
		(func (export "callback") (param i32 i32 i32 i32) (result i32)
			(i32.const 8)
			(call $allocate)
			(drop)
			(local.get 0) (local.get 1) (local.get 2) (local.get 3)
			(call $host)
			(local.get 2)))`), WithImports(Imports{
		"env.middle":   middleCall,
		"env.allocate": middleAllocate,
		"env.memory":   sharedMemory,
		"env.host": HostFunc(func(_ HostModule, params, results []uint64) {
			hostCalls++
		}),
	}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	callback, err := adapter.ExportedFunc("callback")
	if err != nil {
		t.Fatal(err)
	}
	initializer, err := rt.Instantiate(ctx, mustCompileWat(rt, t, `(module
		(type $callback (func (param i32 i32 i32 i32) (result i32)))
		(import "env" "table" (table 1 funcref))
		(import "env" "callback" (func $callback (type $callback)))
		(elem (i32.const 0) func $callback))`), WithImports(Imports{"env.table": table, "env.callback": callback}), WithSynchronousHostCalls())
	if err != nil {
		t.Fatal(err)
	}
	defer initializer.Close()

	got, err := adapter.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || AsI32(got[0]) != 45 {
		t.Fatalf("result = %v, want 45", got)
	}
	if hostCalls != 1 {
		t.Fatalf("host calls = %d, want 1", hostCalls)
	}
}

func funcImportEntry(module, name string, typeIdx uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, 0x00) // ExternFunc
	return append(out, wasmtest.ULEB(typeIdx)...)
}

// TestCrossInstanceMemoryShared: A owns a memory with data; B imports A's memory,
// writes into it, and A observes the write (shared bytes).
func TestCrossInstanceMemoryShared(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit") // pin the explicit-bounds path (guard-page sharing is covered in memory_guardpage_test.go)
	// A: memory 1; data at offset 10 = {1,2,3}; load(a)->i32 = i32.load8_u; store(a,v).
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})), // 1 memory, min 1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("load", 0, 0),
			wasmtest.ExportEntry("store", 0, 1),
			wasmtest.ExportEntry("mem", 2, 0), // memory export
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b}),             // local.get 0; i32.load8_u; end
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x3a, 0x00, 0x00, 0x0b}), // local.get0; local.get1; i32.store8; end
		)),
		// data: offset 10, bytes {1,2,3}
		wasmtest.Section(11, wasmtest.Vec(append([]byte{0x00, 0x41, 0x0a, 0x0b, 0x03}, 0x01, 0x02, 0x03))),
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	memImport, err := inA.ExportedMemory("mem")
	if err != nil {
		t.Fatalf("export mem: %v", err)
	}

	// B imports env.mem; write(a,v) = i32.store8; load(a)->i32.
	memEntry := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
	memEntry = append(memEntry, 0x02, 0x00, 0x01) // ExternMem, min 1
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("write", 0, 0),
			wasmtest.ExportEntry("load", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x3a, 0x00, 0x00, 0x0b}), // store8
			wasmtest.Code([]byte{0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b}),             // load8_u
		)),
	)
	inB, err := Instantiate(MustCompile(modB), InstantiateOptions{Imports: Imports{"env.mem": memImport}})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()

	// B sees A's data (byte 11 = 2).
	if r, _ := inB.Invoke("load", I32(11)); AsI32(r[0]) != 2 {
		t.Fatalf("B.load(11) = %d, want 2 (A's data)", AsI32(r[0]))
	}
	// B writes byte 11 = 99 -> A observes.
	if _, err := inB.Invoke("write", I32(11), I32(99)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inA.Invoke("load", I32(11)); AsI32(r[0]) != 99 {
		t.Fatalf("A.load(11) = %d, want 99 (B's write)", AsI32(r[0]))
	}
	// A writes byte 20 = 55 -> B observes.
	if _, err := inA.Invoke("store", I32(20), I32(55)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inB.Invoke("load", I32(20)); AsI32(r[0]) != 55 {
		t.Fatalf("B.load(20) = %d, want 55 (A's write)", AsI32(r[0]))
	}
}

// TestCrossInstanceGlobalShared: A exports a mutable i32 global g (=10) plus
// get/set functions; B imports A.g and reads/writes it. The two instances share
// one cell, so writes are mutually visible.
func TestCrossInstanceGlobalShared(t *testing.T) {
	// A: global0 = (mut i32) 10; getg()->i32 = global.get 0; setg(i32) = global.set 0.
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 0x01, 0x41, 0x0a, 0x0b})), // (mut i32) (i32.const 10)
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", 3, 0),
			wasmtest.ExportEntry("getg", 0, 0),
			wasmtest.ExportEntry("setg", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),             // global.get 0; end
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}), // local.get 0; global.set 0; end
		)),
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	gImport, err := inA.ExportedGlobalObject("g")
	if err != nil {
		t.Fatalf("export g: %v", err)
	}

	// B imports env.g (mut i32); read()->i32 = global.get 0; write(i32) = global.set 0.
	gEntry := append(wasmtest.Name("env"), wasmtest.Name("g")...)
	gEntry = append(gEntry, 0x03, 0x7f, 0x01) // ExternGlobal, i32, mutable
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(gEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("read", 0, 0),
			wasmtest.ExportEntry("write", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),             // global.get 0; end
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}), // local.get 0; global.set 0; end
		)),
	)
	inB, err := Instantiate(MustCompile(modB), InstantiateOptions{Imports: Imports{"env.g": gImport}})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()

	// B sees A's initial value.
	if r, _ := inB.Invoke("read"); AsI32(r[0]) != 10 {
		t.Fatalf("B.read = %d, want 10", AsI32(r[0]))
	}
	// B writes -> A observes (shared cell).
	if _, err := inB.Invoke("write", I32(99)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inA.Invoke("getg"); AsI32(r[0]) != 99 {
		t.Fatalf("A.getg = %d, want 99 (B's write)", AsI32(r[0]))
	}
	// A writes -> B observes.
	if _, err := inA.Invoke("setg", I32(7)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inB.Invoke("read"); AsI32(r[0]) != 7 {
		t.Fatalf("B.read = %d, want 7 (A's write)", AsI32(r[0]))
	}
}

// TestCrossInstanceCallNoArgs: instance A exports f()->i32 = 42; instance B
// imports env.f and calls it, returning its result. Exercises the native
// context-swap end to end.
func TestCrossInstanceFunctionImportRetainsProducerResources(t *testing.T) {
	producerCode := MustCompile(benchAddOneModule())
	defer producerCode.Close()
	producer, err := Instantiate(producerCode, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	target, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatalf("ExportedFunc: %v", err)
	}
	consumerCode := MustCompile(benchReturningImportModule())
	defer consumerCode.Close()
	consumer, err := Instantiate(consumerCode, InstantiateOptions{Imports: Imports{"env.f": target}})
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close producer: %v", err)
	}
	producer.lifeMu.Lock()
	released := producer.resourcesClosed
	producer.lifeMu.Unlock()
	if released {
		_ = consumer.Close()
		t.Fatal("producer resources released while a function importer still held dispatch pointers")
	}
	got, err := consumer.Invoke("g", I32(7))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 8 {
		_ = consumer.Close()
		t.Fatalf("consumer call after producer close = %v, %v; want 8", got, err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("Close consumer: %v", err)
	}
	producer.lifeMu.Lock()
	released = producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !released {
		t.Fatal("producer resources remained live after final function importer closed")
	}
}

func TestCrossInstanceCallNoArgs(t *testing.T) {
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", 0, 0),
			wasmtest.ExportEntry("trap", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}), // i32.const 42; end
			wasmtest.Code([]byte{0x00, 0x0b}),       // unreachable; end
		)),
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	fExport, err := inA.ExportedFunc("f")
	if err != nil {
		t.Fatalf("export f: %v", err)
	}

	imp := funcImportEntry("env", "f", 0)
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))), // call 0; end
	)
	cB, err := Compile(nil, modB)
	if err != nil {
		t.Fatalf("compile B: %v", err)
	}
	if !cB.dynamicImports || len(cB.Code) == 0 {
		t.Fatalf("B should compile returning imports through dynamic dispatch")
	}
	inB, err := Instantiate(cB, InstantiateOptions{Imports: Imports{"env.f": fExport}})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()
	r, err := inB.Invoke("call")
	if err != nil {
		t.Fatalf("invoke call: %v", err)
	}
	if AsI32(r[0]) != 42 {
		t.Fatalf("cross-instance call returned %d, want 42", AsI32(r[0]))
	}
	if _, err := inA.Invoke("trap"); crossInstanceTrapCode(err) != TrapUnreachable {
		t.Fatalf("producer trap after cross-instance entry = %v, want unreachable", err)
	}
	preparedTrap, err := inA.PrepareFunction("trap")
	if err != nil {
		t.Fatalf("prepare producer trap: %v", err)
	}
	if _, err := inB.Invoke("call"); err != nil {
		t.Fatalf("second cross-instance call: %v", err)
	}
	if _, err := preparedTrap.Invoke(); crossInstanceTrapCode(err) != TrapUnreachable {
		t.Fatalf("prepared producer trap after cross-instance entry = %v, want unreachable", err)
	}
}

// TestCrossInstanceCallArgs: A exports add(i32,i32)->i32; B calls it as
// addBoth() = add(20, 22). Exercises argument marshaling across the swap.
func TestCrossInstanceCallArgs(t *testing.T) {
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}))), // local.get 0; local.get 1; i32.add; end
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	addExport, err := inA.ExportedFunc("add")
	if err != nil {
		t.Fatalf("export add: %v", err)
	}

	// B: type0 = (i32,i32)->i32 (the import); type1 = ()->i32 (addBoth).
	imp := funcImportEntry("env", "add", 0)
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), // local func addBoth, type 1
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("addBoth", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x14, 0x41, 0x16, 0x10, 0x00, 0x0b}))), // i32.const 20; i32.const 22; call 0; end
	)
	inB, err := Instantiate(MustCompile(modB), InstantiateOptions{Imports: Imports{"env.add": addExport}})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()
	r, err := inB.Invoke("addBoth")
	if err != nil {
		t.Fatalf("invoke addBoth: %v", err)
	}
	if AsI32(r[0]) != 42 {
		t.Fatalf("cross-instance add returned %d, want 42", AsI32(r[0]))
	}
}

func TestCrossInstanceIndirectCallReloadsModulePinnedGlobal(t *testing.T) {
	set77Body := append([]byte{0x41}, wasmtest.SLEB32(77)...)
	set77Body = append(set77Body, 0x24, 0x00, 0x0b) // i32.const 77; global.set 0; end

	// The caller's block/loop contains three global.get operations under one loop,
	// giving imported mutable global 0 enough static hotness for the module-global
	// pin heuristic. The indirect call then crosses to A and mutates the same cell;
	// the final global.get must reload the caller's module-pinned register from the
	// shared cell instead of observing the stale prologue value.
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x05, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", 3, 0),
			wasmtest.ExportEntry("set77", 0, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(set77Body))),
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	setExport, err := inA.ExportedFunc("set77")
	if err != nil {
		t.Fatalf("export set77: %v", err)
	}
	gExport, err := inA.ExportedGlobalObject("g")
	if err != nil {
		t.Fatalf("export g: %v", err)
	}

	globalImport := wasmtest.GlobalImportEntry("env", "g", wasm.I32, true)
	body := []byte{
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x23, 0x00, 0x1a, // global.get 0; drop
		0x23, 0x00, 0x1a, // global.get 0; drop
		0x23, 0x00, 0x1a, // global.get 0; drop
		0x0c, 0x01, // br 1 (exit block after one iteration)
		0x0b,       // end loop
		0x0b,       // end block
		0x41, 0x00, // i32.const 0 (table index)
		0x11, 0x00, 0x00, // call_indirect type 0 table 0
		0x23, 0x00, // global.get 0
		0x0b, // end
	}
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(funcImportEntry("env", "set", 0), globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})), // funcref table min=1
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})), // elem (i32.const 0) [imported set]
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	inB, err := Instantiate(MustCompile(modB), InstantiateOptions{Imports: Imports{"env.set": setExport, "env.g": gExport}})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()
	res, err := inB.Invoke("call")
	if err != nil {
		t.Fatalf("invoke B.call: %v", err)
	}
	if got := AsI32(res[0]); got != 77 {
		t.Fatalf("B.call = %d, want 77 from cross-instance indirect callee", got)
	}
}

func TestCrossInstanceCallMultiValueImport(t *testing.T) {
	wantI64 := int64(0x1020304050607080)
	wantI32 := int32(0x10203040)

	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I64},
			[]wasm.ValType{wasm.I64, wasm.I32},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("reorder", 0, 0))),
		// local.get 1; local.get 0; end
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x00, 0x0b}))),
	)
	inA, err := Instantiate(MustCompile(modA), nil)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	reorderExport, err := inA.ExportedFunc("reorder")
	if err != nil {
		t.Fatalf("export reorder: %v", err)
	}

	imp := funcImportEntry("env", "reorder", 0)
	body := []byte{0x41}
	body = append(body, wasmtest.SLEB32(wantI32)...)
	body = append(body, 0x42)
	body = append(body, wasmtest.SLEB64(wantI64)...)
	body = append(body, 0x10, 0x00, 0x0b) // call imported reorder; end
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64, wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64, wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	inB, err := Instantiate(MustCompile(modB), Imports{"env.reorder": reorderExport})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()

	raw, err := inB.Invoke("call")
	if err != nil {
		t.Fatalf("invoke cross-instance multi-value call: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("cross-instance call returned %d slot(s), want 2", len(raw))
	}
	if got := AsI64(raw[0]); got != wantI64 {
		t.Fatalf("cross-instance i64 result = %d, want %d", got, wantI64)
	}
	if got := AsI32(raw[1]); got != wantI32 {
		t.Fatalf("cross-instance i32 result = %d, want %d", got, wantI32)
	}

	out, err := inB.Call(context.Background(), "call")
	if err != nil {
		t.Fatalf("typed call cross-instance multi-value: %v", err)
	}
	if len(out) != 2 || out[0].Type() != ValI64 || out[0].I64() != wantI64 || out[1].Type() != ValI32 || out[1].I32() != wantI32 {
		t.Fatalf("typed cross-instance call = %v, want i64(%d), i32(%d)", out, wantI64, wantI32)
	}
}

func TestCrossInstanceCallV128(t *testing.T) {
	vec := V128{0xde, 0xad, 0xbe, 0xef, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))), // local.get 0; end
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	idExport, err := inA.ExportedFunc("id")
	if err != nil {
		t.Fatalf("export id: %v", err)
	}

	imp := funcImportEntry("env", "id", 0)
	body := append([]byte{0xfd, 0x0c}, vec[:]...) // v128.const vec
	body = append(body, 0x10, 0x00, 0x0b)         // call 0; end
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	cB := MustCompile(modB)
	if !cB.dynamicImports || len(cB.Code) == 0 {
		t.Fatal("v128 function import should compile through dynamic dispatch")
	}
	inB, err := Instantiate(cB, InstantiateOptions{Imports: Imports{"env.id": idExport}})
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer inB.Close()
	res, err := inB.Invoke("call")
	if err != nil {
		t.Fatalf("invoke call: %v", err)
	}
	if got := hostV128FromSlots(res[0], res[1]); got != vec {
		t.Fatalf("cross-instance v128 result = % x, want % x", got, vec)
	}

	// Re-exporting the imported function exercises Instance.invokeLocal's public
	// slot accounting for v128 params/results.
	modReexport := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
	)
	inReexport, err := Instantiate(MustCompile(modReexport), InstantiateOptions{Imports: Imports{"env.id": idExport}})
	if err != nil {
		t.Fatalf("instantiate re-export: %v", err)
	}
	defer inReexport.Close()
	lo, hi := hostV128Slots(vec)
	res, err = inReexport.Invoke("id", lo, hi)
	if err != nil {
		t.Fatalf("invoke re-exported id: %v", err)
	}
	if got := hostV128FromSlots(res[0], res[1]); got != vec {
		t.Fatalf("re-exported v128 result = % x, want % x", got, vec)
	}
}
