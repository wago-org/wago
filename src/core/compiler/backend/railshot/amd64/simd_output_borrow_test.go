//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestSIMDOutputBorrowPreservesPinnedInputs(t *testing.T) {
	requireCompilerDiagnostics(t)
	previous := simdOutputBorrowEnabled
	defer func() { simdOutputBorrowEnabled = previous }()
	a := [16]byte{0, 1, 0x80, 0x7f, 0xff, 0xa3, 7, 0x89, 0, 0, 0, 0x80, 0xff, 0xff, 0xff, 0x7f}
	b := [16]byte{0x9a, 0x31, 7, 0, 0xff, 0, 0xff, 0x7f, 0, 0x80, 0x44, 0x50, 1, 2, 3, 4}
	cases := []struct {
		name                     string
		sub                      uint32
		shift, variable, shuffle bool
	}{
		{name: "not", sub: 77}, {name: "abs8", sub: 96}, {name: "neg8", sub: 97},
		{name: "abs16", sub: 128}, {name: "neg16", sub: 129}, {name: "abs32", sub: 160}, {name: "neg32", sub: 161},
		{name: "f32ceil", sub: 103}, {name: "f32floor", sub: 104}, {name: "f32trunc", sub: 105}, {name: "f32nearest", sub: 106},
		{name: "f64ceil", sub: 116}, {name: "f64floor", sub: 117}, {name: "f64trunc", sub: 122}, {name: "f64nearest", sub: 148},
		{name: "convert_i32_f32", sub: 250}, {name: "convert_low_i32_f64", sub: 254},
		{name: "convert_u32_f32", sub: 251}, {name: "convert_low_u32_f64", sub: 255},
		{name: "shift16_imm", sub: 141, shift: true}, {name: "shift32_imm", sub: 171, shift: true}, {name: "shift64_imm", sub: 205, shift: true},
		{name: "shift16_var", sub: 140, shift: true, variable: true}, {name: "shift32_var", sub: 172, shift: true, variable: true}, {name: "shift64_var", sub: 203, shift: true, variable: true},
		{name: "shuffle", sub: 13, shuffle: true},
	}
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, compact := range []bool{false, true} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("features=%x/compact=%v/%s", features, compact, tc.name), func(t *testing.T) {
					// param 0 is the dynamic shift count; locals 1/2/3 are vectors.
					body := []byte{1, 3, 0x7b}
					body = append(body, v128ConstBytes(a)...)
					body = append(body, 0x21, 1)
					body = append(body, v128ConstBytes(b)...)
					body = append(body, 0x21, 2, 0x20, 1)
					if tc.shuffle {
						body = append(body, 0x20, 2)
					}
					if tc.shift {
						if tc.variable {
							body = append(body, 0x20, 0)
						} else {
							body = append(body, 0x41, 0x43)
						} // -61 wraps by width
					}
					body = append(body, simdOp(tc.sub)...)
					if tc.shuffle {
						body = append(body, []byte{0, 17, 2, 19, 4, 21, 6, 23, 8, 25, 10, 27, 12, 29, 14, 31}...)
					}
					body = append(body, 0x21, 3)
					for i, v := range [][16]byte{a, b} {
						body = append(body, 0x20, byte(i+1))
						body = append(body, v128ConstBytes(v)...)
						body = append(body, simdOp(81)...)
						body = append(body, simdOp(83)...)
						body = append(body, 0x04, 0x40, 0x00, 0x0b) // trap if an original changed
					}
					body = append(body, 0x20, 3, 0x0b)
					m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}, body)
					opts := CompileOptions{AMD64Features: features, AMD64FeaturesSet: true, CompactNative: compact, Optimizations: map[string]bool{"v128-pins": true, "reg-abi": true}}
					var arg [16]byte
					binary.LittleEndian.PutUint64(arg[:8], 0xffffffff00000043)
					simdOutputBorrowEnabled = false
					want := runAmd64V128WithOptions(t, m, &arg, opts)
					simdOutputBorrowEnabled = true
					got := runAmd64V128WithOptions(t, m, &arg, opts)
					if got != want {
						t.Fatalf("on=%x off=%x", got, want)
					}
					var stats ModuleStats
					opts.Stats = &stats
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if stats.Funcs[0].PinnedLocals == 0 {
						t.Fatal("fixture did not pin inputs")
					}
					selected := stats.Funcs[0].Peephole["simd-output-borrow"] + stats.Funcs[0].Peephole["simd-shuffle-output-borrow"] + stats.Funcs[0].Peephole["simd-convert-input-borrow"]
					// Signed conversions already borrow in the retained compiler; they
					// are controls, and must not be attributed to this experiment.
					if tc.sub == 250 || tc.sub == 254 {
						if selected != 0 {
							t.Fatal("existing borrowing control selected new rule")
						}
					} else if selected == 0 {
						t.Fatal("fixture did not select borrowing")
					}
				})
			}
		}
	}
}
