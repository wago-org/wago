//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
)

// growWAT: memory min 1 / max 4 pages, with size/grow/store/load exports
// (function indices 0..3).
const growWAT = `(module
  (memory 1 4)
  (func (export "size") (result i32) memory.size)
  (func (export "grow") (param i32) (result i32) local.get 0 memory.grow)
  (func (export "store") (param i32 i32) local.get 0 local.get 1 i32.store)
  (func (export "load") (param i32) (result i32) local.get 0 i32.load))`

func TestMemorySizeAndGrow(t *testing.T) {
	m := watToModule(t, growWAT)
	cm, err := CompileModuleWith(m, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemoryGrowable(1*65536, 4*65536)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, baseAddr, _ := runtime.MapCode(cm.Code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)

	call := func(fn int, args ...int32) (int32, bool) {
		for i, a := range args {
			binary.LittleEndian.PutUint64(serArgs[i*8:], uint64(uint32(a)))
		}
		err := eng.Call(baseAddr+uintptr(cm.Entry[fn]), serArgs, jm.LinearMemory(), trap, results)
		if err != nil {
			return 0, true
		}
		return int32(binary.LittleEndian.Uint32(results)), false
	}
	const size, grow, store, load = 0, 1, 2, 3

	if got, _ := call(size); got != 1 {
		t.Fatalf("initial memory.size = %d, want 1", got)
	}
	// Accessing page 2 before growing must trap (offset 100000 > 65536).
	if _, trapped := call(load, 100000); !trapped {
		t.Error("load at 100000 before grow should trap (out of bounds)")
	}
	// grow(2) returns the old size (1) and raises size to 3.
	if got, _ := call(grow, 2); got != 1 {
		t.Fatalf("memory.grow(2) = %d, want old size 1", got)
	}
	if got, _ := call(size); got != 3 {
		t.Fatalf("memory.size after grow = %d, want 3", got)
	}
	// The newly available pages are usable.
	if _, trapped := call(store, 100000, 12345); trapped {
		t.Fatal("store at 100000 after grow trapped")
	}
	if got, trapped := call(load, 100000); trapped || got != 12345 {
		t.Fatalf("load at 100000 after grow = %d trapped=%v, want 12345", got, trapped)
	}
	// grow beyond max (3 + 2 = 5 > 4) returns -1 and leaves the size unchanged.
	if got, _ := call(grow, 2); got != -1 {
		t.Fatalf("memory.grow past max = %d, want -1", got)
	}
	if got, _ := call(size); got != 3 {
		t.Fatalf("memory.size after failed grow = %d, want 3", got)
	}
	// grow(0) is a no-op that returns the current size.
	if got, _ := call(grow, 0); got != 3 {
		t.Fatalf("memory.grow(0) = %d, want 3", got)
	}
}

// On a fixed (non-growable) memory, grow always fails.
func TestMemoryGrowFixedFails(t *testing.T) {
	m := watToModule(t, growWAT)
	cm, _ := CompileModuleWith(m, CompileOptions{})
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 * 65536) // max == initial
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, baseAddr, _ := runtime.MapCode(cm.Code)
	defer runtime.Unmap(mem)
	serArgs, results, trap := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
	binary.LittleEndian.PutUint64(serArgs, 1)
	eng.Call(baseAddr+uintptr(cm.Entry[1]), serArgs, jm.LinearMemory(), trap, results)
	if got := int32(binary.LittleEndian.Uint32(results)); got != -1 {
		t.Fatalf("grow on fixed memory = %d, want -1", got)
	}
}
