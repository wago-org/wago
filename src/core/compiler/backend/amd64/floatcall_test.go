//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
)

// A float result returned by a call must land in an XMM register, so a
// subsequent float op consumes it correctly (regression: it was pushed as an
// integer and read from the wrong register file).
func TestCallFloatResult(t *testing.T) {
	// f() = f32.neg(callee()) where callee() returns 1.5 → -1.5
	m := watToModule(t, `(module
		(func $c (result f32) f32.const 1.5)
		(func (export "f") (result f32) call $c f32.neg)
		(func $cd (result f64) f64.const 2.25)
		(func (export "g") (result f64) call $cd f64.neg)
		(func $id (param f32) (result f32) local.get 0)
		(func (export "h") (result f32) f32.const 3.5 call $id f32.abs))`)
	cm, err := CompileModuleWith(m, CompileOptions{})
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
	sa, rs, tp := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
	call := func(fn int) uint64 {
		if err := eng.Call(base+uintptr(cm.Entry[fn]), sa, jm.LinearMemory(), tp, rs); err != nil {
			t.Fatalf("call: %v", err)
		}
		return binary.LittleEndian.Uint64(rs)
	}
	if got := math.Float32frombits(uint32(call(1))); got != -1.5 { // f
		t.Errorf("f32.neg(call) = %v, want -1.5", got)
	}
	if got := math.Float64frombits(call(3)); got != -2.25 { // g
		t.Errorf("f64.neg(call) = %v, want -2.25", got)
	}
	if got := math.Float32frombits(uint32(call(5))); got != 3.5 { // h: abs(id(3.5))
		t.Errorf("f32.abs(call arg passthrough) = %v, want 3.5", got)
	}
}
