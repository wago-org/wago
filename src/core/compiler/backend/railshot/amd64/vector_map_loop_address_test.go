//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestScalarMemoryRecurrenceUncachedCoalescedAddress(t *testing.T) {
	// Four arguments; six hot unrelated integer locals fill otherwise-free pins.
	b := []byte{1, 6, 0x7f}
	for x := byte(4); x < 10; x++ {
		b = append(b, 0x20, 0, 0x21, x)
	}
	for j := 0; j < 30; j++ {
		for x := byte(4); x < 10; x++ {
			b = append(b, 0x20, x, 0x41, 1, 0x6a, 0x21, x)
		}
	}
	// Two preloaded wide constants occupy the remaining free non-fixed registers.
	b = append(b, 3, 0x40)
	for _, c := range []int64{0x123456789abcdef, 0x23456789abcdef1} {
		b = append(b, 0x42, 1, 0x42)
		b = append(b, wasmtest.SLEB64(c)...)
		b = append(b, 0x7e, 0x1a)
	}
	b = append(b, 0x0b, 3, 0x40)
	// Accumulate into one invariant output cell; two source streams coalesce.
	b = append(b, 0x20, 1, 0x20, 1, 0x2b, 0, 0)
	for j := 0; j < 7; j++ {
		k := byte(j + 1)
		off := byte(0)
		if j == 0 {
			k = 2
			off = 8
		}
		b = append(b, 0x20, 0, 0x41, k, 0x6c, 0x2b, 0, off)
		b = append(b, 0xa0)
	}
	b = append(b, 0x39, 0, 0, 0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x20, 3, 0x47, 0x0d, 0, 0x0b)
	// Keep all those pins live across the target loop.
	b = append(b, 0x20, 4)
	for x := byte(5); x < 10; x++ {
		b = append(b, 0x20, x, 0x6a)
	}
	b = append(b, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	saved, savedFast := scalarMemoryRecurrenceEnabled, regionLoopTestFast
	defer func() { scalarMemoryRecurrenceEnabled, regionLoopTestFast = saved, savedFast }()
	var expected []byte
	for _, on := range []bool{false, true} {
		scalarMemoryRecurrenceEnabled = on
		regionLoopTestFast = on
		var stats ModuleStats
		result, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats)}, func(mem []byte) {
			for i := 0; i < len(mem)/8; i++ {
				binary.LittleEndian.PutUint64(mem[i*8:], math.Float64bits(float64(i)))
			}
		}, 128, 4096, 0, 4)
		if err != nil {
			t.Fatal(err)
		}
		got := math.Float64frombits(binary.LittleEndian.Uint64(mem[4096:]))
		if result != 6*(128+30) {
			t.Errorf("on=%v locals=%v want=948", on, result)
		}
		if !on {
			expected = append([]byte(nil), mem...)
		} else if !bytes.Equal(mem, expected) {
			t.Error("optimized loop changed raw memory")
		}
		if on && diagnosticsEnabled && stats.Funcs[0].Peephole["region-loop-scalar-memory-recurrence"] != 1 {
			t.Fatal("scalar recurrence not emitted", stats.Funcs[0].Peephole)
		}
		if got != 2372 {
			t.Errorf("on=%v got=%v want=2372", on, got)
		}
	}
}

// A coalesced child must reconstruct the same base as its parent: callers add
// the child's displacement separately. Cover both invariant and moving streams.
func TestRegionLoopUncachedCoalescedBase(t *testing.T) {
	for _, stride := range []uint32{0, 1, 8} {
		f := &fn{a: &encoder.Asm{}}
		e := regionLoopEmitter{f: f, gp: [4]Reg{RAX, RCX, RDX, RBX}}
		e.streams[0] = regionMemoryStream{parent: 0, reg: regNone, stride: stride}
		e.streams[1] = regionMemoryStream{parent: 0, reg: regNone, stride: stride, disp: 8}
		parent := e.address(0)
		expected := append([]byte(nil), f.a.B...)
		f.a.B = f.a.B[:0]
		child := e.address(1)
		if child != parent || !bytes.Equal(f.a.B, expected) {
			t.Errorf("stride=%d: child base %x differs from parent %x", stride, f.a.B, expected)
		}
	}
}
