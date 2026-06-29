//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// runI64Mem compiles function 0, runs it with one i32 arg, and returns the i64
// result plus the post-exec linear memory so stores can be inspected.
func runI64Mem(t *testing.T, m *wasm.Module, arg int32) (int64, []byte) {
	t.Helper()
	code, err := CompileFunction(m, 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
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
	binary.LittleEndian.PutUint32(serArgs, uint32(arg))
	if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	lm := append([]byte(nil), jm.LinearMemory()...) // copy before Close unmaps
	return int64(binary.LittleEndian.Uint64(results)), lm
}

// TestI64SubwidthLoads stores a known 8-byte pattern at addr 0 then loads it
// back with each narrow op; the signed forms must sign-extend to all 64 bits.
func TestI64SubwidthLoads(t *testing.T) {
	// Memory at [0] = 0x90A0B0C0D0E0F080 (little-endian bytes 80 F0 E0 D0 ...).
	// Pre-seed it by storing the i64 first, then load narrow slices.
	cases := []struct {
		name string
		op   string
		want int64
	}{
		{"load8_u", "i64.load8_u", 0x80},
		{"load8_s", "i64.load8_s", -128},         // sign-extend 0x80
		{"load16_u", "i64.load16_u", 0xF080},     //
		{"load16_s", "i64.load16_s", -3968},      // sign-extend 0xF080
		{"load32_u", "i64.load32_u", 0xD0E0F080}, //
		{"load32_s", "i64.load32_s", -790564736}, // sign-extend 0xD0E0F080
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wat := `(module (memory 1) (func (export "f") (param i32) (result i64)
				i32.const 0 i64.const 0x90A0B0C0D0E0F080 i64.store
				i32.const 0 ` + c.op + `))`
			m := watToModule(t, wat)
			got, _ := runI64Mem(t, m, 0)
			if got != c.want {
				t.Fatalf("%s = %#x, want %#x", c.op, uint64(got), uint64(c.want))
			}
		})
	}
}

// TestI64SubwidthStores writes a full-width i64 into memory then overwrites a
// narrow slice; the untouched bytes must remain, the written bytes must update.
func TestI64SubwidthStores(t *testing.T) {
	cases := []struct {
		name string
		op   string
		want uint64 // expected 8 bytes at addr 0 afterward
	}{
		{"store8", "i64.store8", 0xFFFFFFFFFFFFFF11},
		{"store16", "i64.store16", 0xFFFFFFFFFFFF2211},
		{"store32", "i64.store32", 0xFFFFFFFF44332211},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// seed [0..8) = all 0xFF, then narrow-store 0x...44332211.
			wat := `(module (memory 1) (func (export "f") (param i32) (result i64)
				i32.const 0 i64.const -1 i64.store
				i32.const 0 i64.const 0x8877665544332211 ` + c.op + `
				i64.const 0))`
			m := watToModule(t, wat)
			_, lm := runI64Mem(t, m, 0)
			got := binary.LittleEndian.Uint64(lm)
			if got != c.want {
				t.Fatalf("%s mem = %#x, want %#x", c.op, got, c.want)
			}
		})
	}
}
