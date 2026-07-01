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
	c, err := Compile(bin)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, nil)
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

// Marshal/Load must preserve the start function and memory-sizing metadata, and
// the loaded module must still run its start.
func TestCompiledRoundtripPreservesStartAndMemory(t *testing.T) {
	bin := watToWasmCA(t, `(module
		(memory 2 5)
		(func $init (i32.store8 (i32.const 100000) (i32.const 7)))
		(start $init)
		(func (export "get") (result i32) (i32.load8_u (i32.const 100000))))`)
	c, err := Compile(bin)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	c2, err := Load(blob)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !c2.HasStart || c2.StartLocalFunc != c.StartLocalFunc {
		t.Fatalf("start metadata lost: HasStart=%v idx=%d", c2.HasStart, c2.StartLocalFunc)
	}
	if c2.MemMinPages != 2 || c2.MemMaxPages != 5 {
		t.Fatalf("memory metadata lost: min=%d max=%d", c2.MemMinPages, c2.MemMaxPages)
	}
	in, err := Instantiate(c2, nil) // runs start on the loaded module
	if err != nil {
		t.Fatalf("instantiate loaded: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("get")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 7 {
		t.Errorf("loaded module: get() = %d, want 7 (start ran into page 2)", AsI32(res[0]))
	}
}

// A trap in the start function aborts instantiation.
func TestStartFunctionTrapAbortsInstantiate(t *testing.T) {
	bin := watToWasmCA(t, `(module (func $boom unreachable) (start $boom))`)
	c, err := Compile(bin)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := Instantiate(c, nil); err == nil {
		t.Fatal("trapping start should abort instantiation")
	}
}
