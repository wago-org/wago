//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func linearSumLoopModuleAMD64(t *testing.T) *wasm.Module {
	t.Helper()
	// (param i32 counter) (result i64), locals: i32 addr, i64 acc.
	body := []byte{
		0x02, 0x01, 0x7f, 0x01, 0x7e,
		0x42, 0x00, 0x21, 0x02,
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x00, 0x45, 0x0d, 0x01,
		0x20, 0x02, 0x20, 0x01, 0x29, 0x03, 0x00, 0x7c, 0x21, 0x02,
		0x20, 0x01, 0x41, 0x08, 0x6a, 0x21, 0x01,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, 0x0c, 0x00,
		0x0b, 0x0b,
		0x20, 0x02, 0x0b,
	}
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, body)
}

func TestLinearSumLoopBoundsHoistAndUnrollAMD64(t *testing.T) {
	m := linearSumLoopModuleAMD64(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	cm.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["counted-loop-bounds-hoist"]; got != 1 {
		t.Fatalf("bounds hoists = %d, want 1 (all: %v)", got, stats.Funcs[0].Peephole)
	}
	if got := stats.Funcs[0].Peephole["linear-sum-unroll4"]; got != 1 {
		t.Fatalf("unrolls = %d, want 1 (all: %v)", got, stats.Funcs[0].Peephole)
	}

	setup := func(mem []byte) {
		for i := 0; i < len(mem)/8; i++ {
			binary.LittleEndian.PutUint64(mem[i*8:], uint64(i+1))
		}
	}
	for _, n := range []uint64{0, 1, 2, 3, 4, 5, 7, 8, 9, 31, 512, 8192} {
		got, _, err := runMemAmd64(t, m, setup, n)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		want := n * (n + 1) / 2
		if got != want {
			t.Fatalf("n=%d: sum=%d, want %d", n, got, want)
		}
	}
	for _, n := range []uint64{8193, 1<<32 - 1} {
		if _, _, err := runMemAmd64(t, m, nil, n); err == nil {
			t.Fatalf("n=%d: expected out-of-bounds trap", n)
		}
	}
}

func TestLinearSumLoopOptimizationCanBeDisabledAMD64(t *testing.T) {
	m := linearSumLoopModuleAMD64(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Stats:         &stats,
		Optimizations: map[string]bool{"linear-sum-loop": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	cm.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["counted-loop-bounds-hoist"]; got != 0 {
		t.Fatalf("disabled bounds hoists = %d, want 0", got)
	}
	if got := stats.Funcs[0].Peephole["linear-sum-unroll4"]; got != 0 {
		t.Fatalf("disabled unrolls = %d, want 0", got)
	}
}

func TestLinearSumLoopBoundsHoistRejectsSharedMemoryAMD64(t *testing.T) {
	m := linearSumLoopModuleAMD64(t)
	m.Memories[0].Shared = true
	m.Memories[0].Limits.HasMax = true
	m.Memories[0].Limits.Max = 1
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := stats.Peephole["counted-loop-bounds-hoist"]; got != 0 {
		t.Fatalf("shared-memory bounds hoists = %d, want 0", got)
	}
	if got := stats.Peephole["linear-sum-unroll4"]; got != 0 {
		t.Fatalf("shared-memory unrolls = %d, want 0", got)
	}
}

func linearSumWrappingModuleAMD64(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{
		0x01, 0x01, 0x7e,
		0x42, 0x00, 0x21, 0x02,
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x01, 0x45, 0x0d, 0x01,
		0x20, 0x02, 0x20, 0x00, 0x29, 0x03, 0x00, 0x7c, 0x21, 0x02,
		0x20, 0x00, 0x41, 0x08, 0x6a, 0x21, 0x00,
		0x20, 0x01, 0x41, 0x01, 0x6b, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b,
		0x20, 0x02, 0x0b,
	}
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
}

func TestLinearSumLoopPreservesMemory32WraparoundAMD64(t *testing.T) {
	m := linearSumWrappingModuleAMD64(t)
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer cm.CodeImage.Close()
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(1 << 32)
	if err != nil {
		t.Skipf("4 GiB sparse memory unavailable: %v", err)
	}
	defer jm.Close()
	linear := jm.CurrentBytes()
	binary.LittleEndian.PutUint64(linear[:8], 7)
	binary.LittleEndian.PutUint64(linear[len(linear)-8:], 11)
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	args, results := arena.Alloc(16), arena.Alloc(8)
	trap := arena.Alloc(coreruntime.TrapBufferBytes)
	binary.LittleEndian.PutUint32(args, 0xfffffff8)
	binary.LittleEndian.PutUint32(args[8:], 2)
	if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("valid wrapping reduction trapped: %v", err)
	}
	if got := binary.LittleEndian.Uint64(results); got != 18 {
		t.Fatalf("wrapping reduction = %d, want 18", got)
	}
	binary.LittleEndian.PutUint32(args, 0xfffffff9)
	binary.LittleEndian.PutUint32(args[8:], 1)
	if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err == nil {
		t.Fatal("unaligned access crossing the memory32 boundary did not trap")
	}
}
