//go:build linux && amd64 && !tinygo

package wago

import "testing"

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

func TestStartTrapKeepsImportedActiveSegmentSideEffects(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	producer := MustCompile(watToWasmCA(t, `(module
		(type $t (func (result i32)))
		(memory (export "memory") 1)
		(table (export "table") 1 funcref)
		(func (export "get memory[0]") (result i32)
			(i32.load8_u (i32.const 0)))
		(func (export "get table[0]") (result i32)
			(call_indirect (type $t) (i32.const 0))))`))
	owner, err := Instantiate(producer)
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	defer owner.Close()
	memory, err := owner.ExportedMemory("memory")
	if err != nil {
		t.Fatalf("export memory: %v", err)
	}
	table, err := owner.ExportedTable("table")
	if err != nil {
		t.Fatalf("export table: %v", err)
	}

	consumer := MustCompile(watToWasmCA(t, `(module
		(import "Ms" "memory" (memory 1))
		(import "Ms" "table" (table 1 funcref))
		(data (i32.const 0) "hello")
		(elem (i32.const 0) $f)
		(func $f (result i32) (i32.const 0xdead))
		(func $main unreachable)
		(start $main))`))
	if _, err := Instantiate(consumer, Imports{"Ms.memory": memory, "Ms.table": table}); err == nil {
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
	got, err = owner.Invoke("get table[0]")
	if err != nil {
		t.Fatalf("call shared table entry after failed instantiate: %v", err)
	}
	if value := AsI32(got[0]); value != 0xdead {
		t.Fatalf("table[0]() = %#x, want 0xdead", value)
	}
}
