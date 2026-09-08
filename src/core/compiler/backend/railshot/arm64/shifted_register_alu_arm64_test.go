//go:build arm64

package arm64

import (
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func shiftedRegisterALUModuleARM64(t testing.TB, typ wasm.ValType, shiftOp, aluOp byte, aliasShift bool) *wasm.Module {
	const count = byte(13)
	const (
		localGet = byte(0x20)
		localSet = byte(0x21)
		i32Const = byte(0x41)
		i64Const = byte(0x42)
		end      = byte(0x0b)
	)
	other, shifted := byte(0), byte(1)
	if aliasShift {
		other, shifted = 1, 0 // local.set 0 then aliases the shifted source.
	}
	constOp := i32Const
	if typ == wasm.I64 {
		constOp = i64Const
	}
	body := []byte{0x00,
		localGet, other,
		localGet, shifted, constOp, count, shiftOp,
		aluOp,
		localSet, 0,
		localGet, 0,
		end,
	}
	return mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
}

func TestShiftedRegisterALUArm64(t *testing.T) {
	type tc struct {
		name          string
		shift32, op32 byte
		shift64, op64 byte
		want32        func(uint32, uint32) uint32
		want64        func(uint64, uint64) uint64
	}
	cases := []tc{
		{"add-lsr", 0x76, 0x6a, 0x88, 0x7c, func(x, y uint32) uint32 { return x + y>>13 }, func(x, y uint64) uint64 { return x + y>>13 }},
		{"sub-lsl", 0x74, 0x6b, 0x86, 0x7d, func(x, y uint32) uint32 { return x - y<<13 }, func(x, y uint64) uint64 { return x - y<<13 }},
		{"and-asr", 0x75, 0x71, 0x87, 0x83, func(x, y uint32) uint32 { return x & uint32(int32(y)>>13) }, func(x, y uint64) uint64 { return x & uint64(int64(y)>>13) }},
		{"or-rotl", 0x77, 0x72, 0x89, 0x84, func(x, y uint32) uint32 { return x | bits.RotateLeft32(y, 13) }, func(x, y uint64) uint64 { return x | bits.RotateLeft64(y, 13) }},
		{"xor-rotr", 0x78, 0x73, 0x8a, 0x85, func(x, y uint32) uint32 { return x ^ bits.RotateLeft32(y, -13) }, func(x, y uint64) uint64 { return x ^ bits.RotateLeft64(y, -13) }},
	}
	values := [][2]uint64{
		{0, 0}, {1, 1}, {0x89abcdef, 0xfedcba98},
		{0xffffffffffffffff, 0x8000000000000001}, {0x0123456789abcdef, 0xf0e1d2c3b4a59687},
	}
	for _, c := range cases {
		for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
			shiftOp, aluOp := c.shift32, c.op32
			if typ == wasm.I64 {
				shiftOp, aluOp = c.shift64, c.op64
			}
			for _, aliasShift := range []bool{false, true} {
				name := c.name + "-i32"
				if typ == wasm.I64 {
					name = c.name + "-i64"
				}
				if aliasShift {
					name += "-dest-alias-shift"
				}
				t.Run(name, func(t *testing.T) {
					m := shiftedRegisterALUModuleARM64(t, typ, shiftOp, aluOp, aliasShift)
					onStats := &ModuleStats{}
					on, err := CompileModuleWith(m, CompileOptions{Stats: onStats, Optimizations: map[string]bool{"shifted-register-alu": true}})
					if err != nil {
						t.Fatal(err)
					}
					if on.CodeImage != nil {
						t.Cleanup(func() { on.CodeImage.Close() })
					}
					if got := onStats.Funcs[0].Peephole["shifted-register-alu"]; got != 1 {
						t.Fatalf("shifted-register hits = %d, want 1; stats=%v", got, onStats.Funcs[0].Peephole)
					}
					offStats := &ModuleStats{}
					off, err := CompileModuleWith(m, CompileOptions{Stats: offStats, Optimizations: map[string]bool{"shifted-register-alu": false}})
					if err != nil {
						t.Fatal(err)
					}
					if off.CodeImage != nil {
						t.Cleanup(func() { off.CodeImage.Close() })
					}
					if got := offStats.Funcs[0].Peephole["shifted-register-alu"]; got != 0 {
						t.Fatalf("disabled shifted-register hits = %d, want 0", got)
					}
					if onStats.Funcs[0].CodeBytes+4 > offStats.Funcs[0].CodeBytes {
						t.Fatalf("enabled code = %d bytes, disabled = %d; want at least one instruction removed", onStats.Funcs[0].CodeBytes, offStats.Funcs[0].CodeBytes)
					}
					for _, v := range values {
						x, y := v[0], v[1]
						args := []uint64{x, y}
						if aliasShift {
							args[0], args[1] = y, x
						}
						got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"shifted-register-alu": true}}, args...)
						if err != nil {
							t.Fatal(err)
						}
						if typ == wasm.I32 {
							want := c.want32(uint32(x), uint32(y))
							if uint32(got) != want {
								t.Fatalf("(%#x,%#x) = %#x, want %#x", x, y, uint32(got), want)
							}
						} else {
							want := c.want64(x, y)
							if got != want {
								t.Fatalf("(%#x,%#x) = %#x, want %#x", x, y, got, want)
							}
						}
					}
				})
			}
		}
	}
}
