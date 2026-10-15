//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func shiftedI32DotSelectionBody() []byte {
	return []byte{1, 1, 0x7f, 0x03, 0x40,
		0x20, 0, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a,
		0x20, 0, 0x20, 1, 0x6a, 0x22, 5, 0x41, 4, 0x6a, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a, 0x6c,
		0x20, 0, 0x41, 0x7c, 0x6a, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a,
		0x20, 5, 0x28, 2, 0, 0x41, 0x80, 0x7f, 0x6a, 0x6c,
		0x20, 4, 0x6a, 0x6a, 0x21, 4, 0x20, 0, 0x41, 8, 0x6a, 0x21, 0,
		0x20, 2, 0x20, 3, 0x41, 0x7e, 0x6a, 0x22, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 4, 0x0b}
}
func TestShiftedI32DotVectorLoopSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	b := shiftedI32DotSelectionBody()
	m, e := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...)))))
	if e != nil {
		t.Fatal(e)
	}
	for _, on := range []bool{false, true} {
		var stats ModuleStats
		c, e := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"integer-sum-loop": on}})
		if e != nil {
			t.Fatal(e)
		}
		c.CodeImage.Close()
		want := 0
		if on {
			want = 1
		}
		if got := stats.Funcs[0].Peephole["shifted-i32-dot-vector-loop"]; got != want {
			t.Fatalf("enabled=%t selected=%d want=%d", on, got, want)
		}
	}
}
func TestShiftedI32DotVectorLoopPressureAndEligibility(t *testing.T) {
	for _, mode := range []string{"gp", "gp-eight", "vector-seven", "interruptible", "inline", "exception", "calls", "constant-address", "constant-regional", "constant-owned", "constant-borrowed"} {
		t.Run(mode, func(t *testing.T) {
			f := gpClassEmitter()
			f.policy = currentCodegenPolicy()
			f.m = &wasm.Module{Memories: []wasm.MemType{{}}}
			f.localType = []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtI32}
			f.nParams = 5
			switch mode {
			case "gp":
				f.pinned = allAllocatableGP()
			case "gp-eight":
				f.pinned = allAllocatableGP()
				n := 0
				for _, reg := range gpAlloc {
					if n < 8 {
						f.pinned = f.pinned.remove(reg)
						n++
					}
				}
			case "constant-address", "constant-regional", "constant-owned", "constant-borrowed":
				cache := gpAlloc[len(gpAlloc)-1]
				f.pinned = allAllocatableGP()
				n := 0
				for _, reg := range gpAlloc {
					if reg != cache && n < 8 {
						f.pinned = f.pinned.remove(reg)
						n++
					}
				}
				f.pinned = f.pinned.remove(cache)
				f.reserved = f.reserved.add(cache)
				f.iconsts[0] = intConstReg{typ: mtI32, bits: 0x1234567, reg: cache, address: mode == "constant-address"}
				f.iconstN = 1
				if mode == "constant-regional" {
					f.intervalLast = make([]uint32, 6)
				}
				if mode == "constant-owned" {
					f.regUser[cache] = &elem{}
				}
				if mode == "constant-borrowed" {
					f.pinned = f.pinned.add(cache)
				}
			case "vector-seven":
				f.fpinned = ^regMask(0)
				n := 0
				for _, reg := range fpAllocRegs {
					if (reg < 8 || reg >= 16) && n < 7 {
						f.fpinned = f.fpinned.remove(reg)
						n++
					}
				}
			case "interruptible":
				f.interruptible = true
			case "inline":
				f.localBase = 1
			case "exception":
				f.moduleEH = true
			case "calls":
				f.usesCalls = true
			}
			pins, fpins, reserved, constants := f.pinned, f.fpinned, f.reserved, f.iconstN
			r := wasm.ReaderFrom(shiftedI32DotSelectionBody()[3:])
			_, _ = r.Byte()
			if done, e := f.tryShiftedI32DotLoop(&r); done || e != nil {
				t.Fatalf("done=%t err=%v", done, e)
			}
			if r.Offset() != 1 || len(f.a.B) != 0 || f.pinned != pins || f.fpinned != fpins || f.reserved != reserved || f.iconstN != constants {
				t.Fatal("fallback changed source, pins or emission")
			}
		})
	}
}

func TestShiftedI32DotBorrowsLiteralCache(t *testing.T) {
	requireCompilerDiagnostics(t)
	b := shiftedI32DotSelectionBody()
	b[1] = 12 // Keep a full permanent-local pin pool.
	prefix := append([]byte{0x41, 8, 0x21, 3, 0xfd, 0x0c}, make([]byte, 16)...)
	prefix = append(prefix, 0x1a) // Keep regional residency out of this pressure fixture.
	for x := byte(6); x < 13; x++ {
		prefix = append(prefix, 0x20, 0, 0x21, x)
	}
	b = append(append(append([]byte{}, b[:3]...), prefix...), b[3:]...)
	literal := append([]byte{0x41}, wasmtest.SLEB32(0x1234567)...)
	b = bytes.ReplaceAll(b, []byte{0x41, 0x80, 0x7f}, literal)
	b = append(b[:len(b)-1], append(literal, 0x6a, 0x0b)...)
	for x := byte(6); x < 13; x++ {
		b = append(b[:len(b)-1], 0x20, x, 0x6a, 0x0b)
	}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...)))))
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	if stats.Funcs[0].Peephole["vector-loop-borrowed-constants"] != 1 || stats.Funcs[0].Peephole["shifted-i32-dot-vector-loop"] != 1 {
		t.Fatalf("missing pressure relief: %v", stats.Funcs[0].Peephole)
	}
}
