//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
)

// Unbounded recursion must trap ("stack fence breached") once the fence is
// installed, rather than faulting the process.
func TestStackOverflowTraps(t *testing.T) {
	m := watToModule(t, `(module
		(func $loop (call $loop))
		(func (export "run") (call $loop)))`)
	cm, err := CompileModuleWith(m, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16)
	defer jm.Close()
	jm.SetStackFence(eng.StackLimit()) // arm the guard
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, base, _ := runtime.MapCode(cm.Code)
	defer runtime.Unmap(mem)
	sa, rs, tp := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)

	err = eng.Call(base+uintptr(cm.Entry[1]), sa, jm.LinearMemory(), tp, rs)
	if err == nil {
		t.Fatal("unbounded recursion returned without trapping")
	}
	if !strings.Contains(err.Error(), "stack fence") {
		t.Fatalf("got %v, want a stack-fence trap", err)
	}
}

// With no fence installed (fence == 0), the check is inert: a bounded recursion
// still runs to completion (the guard never false-trips).
func TestStackFenceInertWhenUnset(t *testing.T) {
	m := watToModule(t, `(module
		(func $sum (param $n i32) (result i32)
			(if (result i32) (i32.eqz (local.get $n))
				(then (i32.const 0))
				(else (i32.add (local.get $n)
					(call $sum (i32.sub (local.get $n) (i32.const 1)))))))
		(func (export "run") (param i32) (result i32) local.get 0 call $sum))`)
	cm, _ := CompileModuleWith(m, CompileOptions{})
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16) // fence left at 0
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, base, _ := runtime.MapCode(cm.Code)
	defer runtime.Unmap(mem)
	sa, rs, tp := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
	binary.LittleEndian.PutUint64(sa, 100)
	if err := eng.Call(base+uintptr(cm.Entry[1]), sa, jm.LinearMemory(), tp, rs); err != nil {
		t.Fatalf("bounded recursion trapped unexpectedly: %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(rs)); got != 5050 { // sum 1..100
		t.Fatalf("sum(100) = %d, want 5050", got)
	}
}
