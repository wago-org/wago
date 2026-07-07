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
