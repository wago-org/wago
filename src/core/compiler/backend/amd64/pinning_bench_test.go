//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
)

// benchLoopWAT sums i*i across a long loop, with the hot accumulator/counter as
// high-index declared locals behind several lightly-used params — the case where
// hotness-aware pinning keeps the loop-carried locals in registers and the
// legacy first-N heuristic spills them to the frame.
const benchLoopWAT = `(module (func (export "f")
  (param $n i32) (param $p1 i32) (param $p2 i32) (param $p3 i32) (result i32)
  (local $i i32) (local $acc i32)
  (block $done (loop $L
    local.get $i local.get $n i32.ge_s br_if $done
    local.get $acc local.get $i local.get $i i32.mul i32.add local.set $acc
    local.get $i i32.const 1 i32.add local.set $i
    br $L))
  local.get $acc local.get $p1 i32.add
  local.get $p2 i32.add local.get $p3 i32.add))`

func benchPinningMode(b *testing.B, mode LocalPinningMode) {
	m := watToModule(b, benchLoopWAT)
	code, err := CompileFunctionWith(m, 0, CompileOptions{LocalPinning: mode})
	if err != nil {
		b.Fatalf("compile: %v", err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	binary.LittleEndian.PutUint64(serArgs[0:], 20000) // n
	binary.LittleEndian.PutUint64(serArgs[8:], 1)
	binary.LittleEndian.PutUint64(serArgs[16:], 2)
	binary.LittleEndian.PutUint64(serArgs[24:], 3)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
			b.Fatalf("call: %v", err)
		}
	}
}

func BenchmarkLocalPinningHotness(b *testing.B) { benchPinningMode(b, PinHotness) }
func BenchmarkLocalPinningFirstN(b *testing.B)  { benchPinningMode(b, PinFirstN) }
func BenchmarkLocalPinningNone(b *testing.B)    { benchPinningMode(b, PinNone) }
