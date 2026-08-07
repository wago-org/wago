//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"testing"
)

// The start function runs during Instantiate, after memory/globals/data are set
// up, so its side effects are visible to the first Invoke.
func TestStartFunctionRuns(t *testing.T) {
	bin := watToWasmCA(t, `(module
		(memory 1)
		(func $init (i32.store8 (i32.const 0) (i32.const 42)))
		(start $init)
		(func (export "get") (result i32) (i32.load8_u (i32.const 0))))`)
	c, err := Compile(nil, bin)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("get")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 42 {
		t.Errorf("start function did not run: get() = %d, want 42", AsI32(res[0]))
	}
}

// A trap in the start function aborts instantiation.
func TestStartFunctionTrapAbortsInstantiate(t *testing.T) {
	bin := watToWasmCA(t, `(module (func $boom unreachable) (start $boom))`)
	c, err := Compile(nil, bin)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := Instantiate(c, InstantiateOptions{}); err == nil {
		t.Fatal("trapping start should abort instantiation")
	}
}

func TestRuntimeStartTrapKeepsImportedMemoryAndTableSideEffects(t *testing.T) {
	if !requireExternalWAT(t) {
		return
	}
	t.Setenv("WAGO_BOUNDS", "explicit")
	rt := NewRuntime()
	defer rt.Close()
	producer, err := rt.Compile(watToWasmCA(t, `(module
		(type $t (func (result i32)))
		(memory (export "memory") 1)
		(table (export "table") 1 funcref)
		(func (export "get memory[0]") (type $t)
			(i32.load8_u (i32.const 0)))
		(func (export "get table[0]") (type $t)
			(call_indirect (type $t) (i32.const 0))))`))
	if err != nil {
		t.Fatalf("compile producer: %v", err)
	}
	owner, err := rt.Instantiate(context.Background(), producer)
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	defer owner.Close()
	memory, err := owner.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	table, err := owner.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := rt.Compile(watToWasmCA(t, `(module
		(import "Ms" "memory" (memory 1))
		(import "Ms" "table" (table 1 funcref))
		(data (i32.const 0) "hello")
		(elem (i32.const 0) $f)
		(func $f (result i32) (i32.const 0xdead))
		(func $main unreachable)
		(start $main))`))
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	if in, err := rt.Instantiate(context.Background(), consumer, WithImports(Imports{"Ms.memory": memory, "Ms.table": table})); err == nil || in != nil {
		t.Fatalf("trapping runtime instantiate = %v, %v; want nil instance and trap", in, err)
	}
	if got := tableTestCallI32(t, owner, "get memory[0]"); got != 104 {
		t.Fatalf("memory[0] after runtime-owned start trap = %d, want 104", got)
	}
	if got := tableTestCallI32(t, owner, "get table[0]"); got != 0xdead {
		t.Fatalf("table[0] after runtime-owned start trap = %d, want 0xdead", got)
	}
}

func TestStartTrapKeepsImportedActiveSegmentSideEffects(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	producer := MustCompile(watToWasmCA(t, `(module
		(memory (export "memory") 1)
		(func (export "get memory[0]") (result i32)
			(i32.load8_u (i32.const 0))))`))
	owner, err := Instantiate(producer)
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	defer owner.Close()
	memory, err := owner.ExportedMemory("memory")
	if err != nil {
		t.Fatalf("export memory: %v", err)
	}

	// A shared-memory importer may compute over the shared linear pages (funcref
	// tables/globals are rejected as basedata aliases; that retention-on-failure
	// path is covered by TestImportedThenLocalFailedInstantiationRetainsSharedTableWrites).
	// Its active data segment writes land in the shared memory before the start
	// function runs, so they persist even when start traps and instantiation aborts.
	consumer := MustCompile(watToWasmCA(t, `(module
		(import "Ms" "memory" (memory 1))
		(data (i32.const 0) "hello")
		(func $main unreachable)
		(start $main))`))
	if _, err := Instantiate(consumer, Imports{"Ms.memory": memory}); err == nil {
		t.Fatal("trapping start should abort consumer instantiation")
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("close failed consumer module: %v", err)
	}

	got, err := owner.Invoke("get memory[0]")
	if err != nil {
		t.Fatalf("read shared memory after failed instantiate: %v", err)
	}
	if value := AsI32(got[0]); value != 104 {
		t.Fatalf("memory[0] = %d, want 104 ('h')", value)
	}
}
