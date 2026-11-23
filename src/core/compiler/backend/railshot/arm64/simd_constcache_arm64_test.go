//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSIMDV128ConstSplatMaterialization(t *testing.T) {
	tests := []struct {
		name string
		v    [16]byte
		base uint32
	}{
		{name: "byte", v: i8x16Bytes(-85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85), base: 0x4f05e400},
		{name: "half", v: i16x8Bytes(0x1234, 0x1234, 0x1234, 0x1234, 0x1234, 0x1234, 0x1234, 0x1234), base: 0x4e020c00},
		{name: "word", v: i32x4Bytes(0x12345678, 0x12345678, 0x12345678, 0x12345678), base: 0x4e040c00},
		{name: "dword", v: i64x2Bytes(0x0123456789abcdef, 0x0123456789abcdef), base: 0x4e080c00},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0x00}, simdConst(tc.v)...)
			body = append(body, simdOp(29)...) // i64x2.extract_lane
			body = append(body, 0x00, 0x0b)    // lane 0; end
			m := mod1(t, nil, []wasm.ValType{wasm.I64}, body)
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			found := false
			for at := 0; at+4 <= len(cm.Code); at += 4 {
				word := binary.LittleEndian.Uint32(cm.Code[at:])
				if word&0xfffffc00 == tc.base {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("compiled code does not contain expected splat encoding %#08x", tc.base)
			}
			if got, want := runArm64u(t, m), binary.LittleEndian.Uint64(tc.v[:8]); got != want {
				t.Fatalf("result %#016x, want %#016x", got, want)
			}
		})
	}
}

func TestSIMDV128ConstCacheArm64(t *testing.T) {
	requireCompilerDiagnostics(t)
	v := i64x2Bytes(0x0123456789abcdef, 0x76543210fedcba98)
	body := append([]byte{0x00}, simdConst(v)...)
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(81)...) // v128.xor
	body = append(body, simdOp(29)...) // i64x2.extract_lane
	body = append(body, 0x00, 0x0b)    // lane 0; end
	m := mod1(t, nil, []wasm.ValType{wasm.I64}, body)
	stats := &ModuleStats{}
	got, err := runArm64WrapperWithOptions(t, m, CompileOptions{
		Stats:         stats,
		Optimizations: map[string]bool{"v128-const-cache": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("result = %#x, want 0", got)
	}
	if n := stats.Funcs[0].Peephole["v128-const-cache"]; n != 1 {
		t.Fatalf("cache reservations = %d, want 1 (%v)", n, stats.Funcs[0].Peephole)
	}
	if n := stats.Funcs[0].Peephole["v128-const-cache-hit"]; n != 2 {
		t.Fatalf("cache hits = %d, want 2 (%v)", n, stats.Funcs[0].Peephole)
	}
}

func TestSIMDV128ConstCacheWithPinnedLocalsArm64(t *testing.T) {
	requireCompilerDiagnostics(t)
	values := [5][16]byte{
		i64x2Bytes(1, 2),
		i64x2Bytes(3, 4),
		i64x2Bytes(0x1111111111111111, 0x2222222222222222),
		i64x2Bytes(0x3333333333333333, 0x4444444444444444),
		i64x2Bytes(0x5555555555555555, 0x6666666666666666),
	}
	body := []byte{0x01, 0x02, 0x7b} // two v128 locals
	for i := 0; i < 2; i++ {
		body = append(body, simdConst(values[i])...)
		body = append(body, 0x21, byte(i)) // local.set i
	}
	for range 4 {
		body = append(body, 0x20, 0x00, 0x20, 0x01)
		body = append(body, simdOp(81)...)
		body = append(body, 0x1a) // drop
	}
	for i := 2; i < len(values); i++ {
		body = append(body, simdConst(values[i])...)
		body = append(body, simdConst(values[i])...)
		body = append(body, simdOp(81)...) // each pair produces zero
		if i != 2 {
			body = append(body, simdOp(81)...) // combine zero vectors
		}
	}
	body = append(body, simdOp(29)...)
	body = append(body, 0x00, 0x0b) // i64x2.extract_lane 0; end
	m := mod1(t, nil, []wasm.ValType{wasm.I64}, body)

	stats := compileWithStats(t, m, false).Funcs[0]
	if stats.PinnedLocals < 2 {
		t.Fatalf("pinned locals = %d, want at least 2", stats.PinnedLocals)
	}
	if got := stats.Peephole["v128-const-cache"]; got != 2 {
		t.Fatalf("cache reservations = %d, want 2 (%v)", got, stats.Peephole)
	}
	if got := runArm64u(t, m); got != 0 {
		t.Fatalf("result = %#x, want 0", got)
	}
}
