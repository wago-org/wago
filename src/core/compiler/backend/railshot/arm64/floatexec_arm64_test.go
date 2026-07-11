//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"math"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestFloatExec runs f(x i32) = i32(f32(x) + 2.0) under qemu — the whole float
// pipeline (i32→f32 convert, f32 const load, f32 add, f32→i32 trunc) happens in
// V-registers, with int args/result so the spike trampoline can drive it.
func TestFloatExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// local.get 0; f32.convert_i32_s; f32.const 2.0; f32.add; i32.trunc_f32_s
	body := []byte{
		0x00,
		0x20, 0x00, // local.get 0
		0xb2,                         // f32.convert_i32_s
		0x43, 0x00, 0x00, 0x00, 0x40, // f32.const 2.0
		0x92, // f32.add
		0xa8, // i32.trunc_f32_s
		0x0b,
	}
	m := mod1(t, i32, i32, body)
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
	for _, x := range []uintptr{5, 0, 40} {
		if got := arm64spike.Call2(entry, x, 0); uint32(got) != uint32(x+2) {
			t.Fatalf("f(%d) = %d, want %d", x, uint32(got), x+2)
		}
	}
}

func TestArm64UnsignedI64ToFloatFullRange(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   byte
		typ  wasm.ValType
		in   uint64
		want uint64
	}{
		{"f32_2p63", 0xb5, wasm.F32, 1 << 63, uint64(math.Float32bits(float32(uint64(1) << 63)))},
		{"f32_max", 0xb5, wasm.F32, ^uint64(0), uint64(math.Float32bits(float32(^uint64(0))))},
		{"f64_2p63", 0xba, wasm.F64, 1 << 63, math.Float64bits(float64(uint64(1) << 63))},
		{"f64_max", 0xba, wasm.F64, ^uint64(0), math.Float64bits(float64(^uint64(0)))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0x00, 0x42}, wasmtest.SLEB64(int64(tc.in))...)
			body = append(body, tc.op, 0x0b)
			m := mod1(t, nil, []wasm.ValType{tc.typ}, body)
			out := runArm64Result(t, m, 8)
			got := binary.LittleEndian.Uint64(out)
			if tc.typ == wasm.F32 {
				got &= 0xffffffff
			}
			if got != tc.want {
				t.Fatalf("conversion bits = %#x, want %#x", got, tc.want)
			}
		})
	}
}
