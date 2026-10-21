//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestVariableShiftRegisterMeasurement(t *testing.T) {
	old := sharedScalarEnabled
	defer func() { sharedScalarEnabled = old }()
	for _, scalar := range []bool{false, true} {
		sharedScalarEnabled = scalar
		for _, wide := range []bool{false, true} {
			typ, op, xor := wasm.I32, byte(0x74), byte(0x73)
			if wide {
				typ, op, xor = wasm.I64, 0x86, 0x85
			}
			for k := 0; k < 3; k++ {
				for _, alias := range []bool{false, true} {
					t.Run(fmt.Sprintf("scalar=%v/wide=%v/op=%d/alias=%v", scalar, wide, k, alias), func(t *testing.T) {
						count := byte(1)
						if alias {
							count = 0
						}
						// Preserve the source after the shift, so a destructive source copy matters.
						m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, count, op + byte(k), 0x20, 0, xor, 0x0b})
						for _, features := range []shared.AMD64Features{0, shared.AMD64BMI2} {
							cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features})
							if err != nil {
								t.Fatal(err)
							}
							if cm.CodeImage != nil {
								defer cm.CodeImage.Close()
							}
							t.Logf("scalar=%v wide=%v features=%v BMI2=%v native-bytes=%d", scalar, wide, features, cm.RequiresBMI2, len(cm.Code))
							for _, x := range []uint64{0, 1, 31, 32, 63, 64, 0x80000000, 0x8000000000000000, ^uint64(0)} {
								for _, c := range []uint64{0, 1, 31, 32, 63, 64, 127, ^uint64(0)} {
									n := c
									if alias {
										n = x
									}
									n &= 63
									if !wide {
										n &= 31
										x = uint64(uint32(x))
									}
									want := x << n
									if k == 1 {
										if wide {
											want = uint64(int64(x) >> n)
										} else {
											want = uint64(uint32(int32(x) >> n))
										}
									} else if k == 2 {
										want = x >> n
									}
									want ^= x
									if !wide {
										want = uint64(uint32(want))
									}
									got := runCompiledAmd64u(t, cm, x, c)
									if !wide {
										got = uint64(uint32(got))
									}
									if got != want {
										t.Fatalf("x=%x c=%x got=%x want=%x", x, c, got, want)
									}
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestVariableShiftRetainsShorterCL(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { sharedScalarEnabled = old }()
	for k := byte(0); k < 3; k++ {
		// Both XOR results are owned low GP registers. A free RCX permits
		// MOV32+shift-CL (four bytes), shorter than the five-byte VEX form.
		m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 1, 0x73, 0x20, 2, 0x20, 3, 0x73, 0x74 + k, 0x0b})
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		cm, e := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64BMI2})
		if e != nil {
			t.Fatal(e)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if cm.RequiresBMI2 {
			t.Fatal("selected longer VEX shift for owned low registers")
		}
		for _, x := range []uint32{0, 1, 0x80000000, 0xffffffff} {
			for _, c := range []uint32{0, 1, 31, 32, 63, 0xffffffff} {
				n := c & 31
				want := x << n
				if k == 1 {
					want = uint32(int32(x) >> n)
				} else if k == 2 {
					want = x >> n
				}
				if got := uint32(runCompiledAmd64u(t, cm, uint64(x), 0, uint64(c), 0)); got != want {
					t.Fatalf("op=%d x=%x c=%x got=%x want=%x", k, x, c, got, want)
				}
			}
		}
	}
}
