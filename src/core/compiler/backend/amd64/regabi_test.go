//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// runModuleI32 compiles a whole module and invokes one function by index through
// the host (wrapper) ABI — which, for register-ABI functions, enters via the
// adapter at offset 0.
func runModuleI32Opts(t *testing.T, m *wasm.Module, opts CompileOptions, funcIdx int, args ...int32) int32 {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, base, _ := runtime.MapCode(cm.Code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	for i, a := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], uint64(uint32(a)))
	}
	fn := base + uintptr(cm.Entry[funcIdx])
	if err := eng.Call(fn, serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return int32(binary.LittleEndian.Uint32(results))
}

const fibrecWAT = `(module (func $fibrec (export "fibrec") (param $n i32) (result i32)
  local.get $n
  i32.const 2
  i32.lt_s
  if (result i32)
    local.get $n
  else
    local.get $n i32.const 1 i32.sub call $fibrec
    local.get $n i32.const 2 i32.sub call $fibrec
    i32.add
  end))`

// Recursive calls through the register ABI must match the wrapper ABI exactly.
func TestRegABIFibrec(t *testing.T) {
	m := watToModule(t, fibrecWAT)
	want := map[int32]int32{0: 0, 1: 1, 2: 1, 7: 13, 10: 55, 15: 610}
	for n, w := range want {
		reg := runModuleI32Opts(t, m, CompileOptions{RegisterCallABI: true}, 0, n)
		wrap := runModuleI32Opts(t, m, CompileOptions{}, 0, n)
		if reg != w || wrap != w {
			t.Errorf("fibrec(%d): regABI=%d wrapper=%d want=%d", n, reg, wrap, w)
		}
	}
}

// A leaf register-ABI function is reached from the host through its adapter.
func TestRegABILeafViaAdapter(t *testing.T) {
	m := watToModule(t, `(module (func (export "f") (param i32 i32) (result i32)
		local.get 0 local.get 1 i32.sub))`)
	if got := runModuleI32Opts(t, m, CompileOptions{RegisterCallABI: true}, 0, 30, 12); got != 18 {
		t.Errorf("sub(30,12) = %d, want 18", got)
	}
}

// Multi-argument register calls (exercises argument placement / move resolution).
func TestRegABIMultiArgCall(t *testing.T) {
	m := watToModule(t, `(module
		(func $sub3 (param i32 i32 i32) (result i32)
			local.get 0 local.get 1 i32.sub local.get 2 i32.sub)
		(func $main (export "main") (param i32) (result i32)
			local.get 0 i32.const 100 i32.const 5 call $sub3))`)
	// main(n) = n - 100 - 5
	for _, n := range []int32{0, 200, -50} {
		reg := runModuleI32Opts(t, m, CompileOptions{RegisterCallABI: true}, 1, n)
		if reg != n-105 {
			t.Errorf("main(%d) = %d, want %d", n, reg, n-105)
		}
	}
}

// A function with a float in its signature must fall back to the wrapper path
// and still produce correct results when register ABI is requested.
func TestRegABIFloatFallback(t *testing.T) {
	m := watToModule(t, `(module
		(func $addf (param f64 f64) (result f64) local.get 0 local.get 1 f64.add)
		(func $main (export "main") (param i32) (result i32)
			local.get 0 i32.const 7 i32.add))`)
	if got := runModuleI32Opts(t, m, CompileOptions{RegisterCallABI: true}, 1, 35); got != 42 {
		t.Errorf("main(35) = %d, want 42", got)
	}
}
