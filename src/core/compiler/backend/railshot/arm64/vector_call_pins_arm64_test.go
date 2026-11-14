//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestVectorLocalAcrossDirectCall(t *testing.T) {
	value := i64x2Bytes(0x123456789abcdef, 0x76543210fedcba9)
	caller := append([]byte{1, 1, 0x7b}, simdConst(value)...)
	caller = append(caller, 0x21, 0, 0x10, 1, 0x1a) // set local, call, drop
	caller = append(caller, 0x20, 0)
	caller = append(caller, simdOp(29)...)
	caller = append(caller, 0, 0x20, 0)
	caller = append(caller, simdOp(29)...)
	caller = append(caller, 1, 0x85, 0x0b)
	// Exercise a real callee with several vector locals, rather than relying on
	// the low half of a callee-saved vector register surviving the call.
	callee := []byte{1, 8, 0x7b}
	for i := 0; i < 8; i++ {
		callee = append(callee, simdConst(i64x2Bytes(int64(i+1), int64(i+11)))...)
		callee = append(callee, 0x21, byte(i))
	}
	callee = append(callee, 0x20, 0)
	for i := 1; i < 8; i++ {
		callee = append(callee, 0x20, byte(i))
		callee = append(callee, simdOp(81)...)
	}
	callee = append(callee, simdOp(29)...)
	callee = append(callee, 1, 0x0b)
	m := modFuncs(t, funcDef{nil, []wasm.ValType{wasm.I64}, caller}, funcDef{nil, []wasm.ValType{wasm.I64}, callee})
	want := binary.LittleEndian.Uint64(value[:8]) ^ binary.LittleEndian.Uint64(value[8:])
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		opts := CompileOptions{Optimizations: map[string]bool{"inline": false, "v128-pins": enabled}}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runArm64WrapperWithOptions(t, m, opts)
		if err != nil || got != want {
			t.Fatalf("v128 pins %v: got %#x, want %#x, err %v", enabled, got, want, err)
		}
		if diagnosticsEnabled && (stats.Funcs[0].PinnedLocals > 0) != enabled {
			t.Fatalf("v128 pins %v: caller pinned %d locals", enabled, stats.Funcs[0].PinnedLocals)
		}
	}
}

func TestVectorLocalAcrossPinPreservingSIMDLeaf(t *testing.T) {
	value := i64x2Bytes(0x123456789abcdef, 0x76543210fedcba9)
	caller := append([]byte{1, 1, 0x7b}, simdConst(value)...)
	caller = append(caller, 0x21, 0, 0x10, 1, 0x1a, 0x20, 0)
	caller = append(caller, simdOp(29)...)
	caller = append(caller, 0, 0x20, 0)
	caller = append(caller, simdOp(29)...)
	caller = append(caller, 1, 0x85, 0x0b)
	callee := []byte{0} // no declared locals; integer result signature
	for i := 0; i < 12; i++ {
		callee = append(callee, simdConst(i64x2Bytes(int64(i+1), int64(i+11)))...)
	}
	for i := 1; i < 12; i++ {
		callee = append(callee, simdOp(81)...)
	}
	callee = append(callee, simdOp(29)...)
	callee = append(callee, 1, 0x0b)
	m := modFuncs(t, funcDef{nil, []wasm.ValType{wasm.I64}, caller}, funcDef{nil, []wasm.ValType{wasm.I64}, callee})
	want := binary.LittleEndian.Uint64(value[:8]) ^ binary.LittleEndian.Uint64(value[8:])
	for _, enabled := range []bool{false, true} {
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"inline": false, "v128-pins": enabled}})
		if err != nil || got != want {
			t.Fatalf("v128 pins %v: got %#x, want %#x, err %v", enabled, got, want, err)
		}
	}
}
