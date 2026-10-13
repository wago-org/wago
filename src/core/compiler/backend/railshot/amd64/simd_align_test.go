//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSIMDAlignShufflePreservesInputs(t *testing.T) {
	requireCompilerDiagnostics(t)
	a := [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 0x80, 0xff, 10, 11, 12, 13, 14, 15}
	b := [16]byte{16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	for _, features := range []shared.AMD64Features{0, shared.AMD64SSSE3, shared.AMD64ModernBaseline} {
		for _, compact := range []bool{false, true} {
			for _, same := range []bool{false, true} {
				for offset := byte(0); offset <= 16; offset++ {
					t.Run(fmt.Sprintf("features=%x/compact=%v/same=%v/offset=%d", features, compact, same, offset), func(t *testing.T) {
						body := []byte{1, 3, 0x7b} // input vectors and result
						body = append(body, v128ConstBytes(a)...)
						body = append(body, 0x21, 0)
						body = append(body, v128ConstBytes(b)...)
						body = append(body, 0x21, 1, 0x20, 0, 0x20)
						if same {
							body = append(body, 0)
						} else {
							body = append(body, 1)
						}
						body = append(body, simdOp(13)...)
						var want [16]byte
						second := b
						if same {
							second = a
						}
						for i := range want {
							lane := offset + byte(i)
							body = append(body, lane)
							if lane < 16 {
								want[i] = a[lane]
							} else {
								want[i] = second[lane-16]
							}
						}
						body = append(body, 0x21, 2)
						for i, original := range [][16]byte{a, b} {
							body = append(body, 0x20, byte(i))
							body = append(body, v128ConstBytes(original)...)
							body = append(body, simdOp(81)...)
							body = append(body, simdOp(83)...)
							body = append(body, 0x04, 0x40, 0, 0x0b) // trap if a local changed
						}
						body = append(body, 0x20, 2, 0x0b)
						m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
						opts := CompileOptions{AMD64Features: features, AMD64FeaturesSet: true, CompactNative: compact}
						if got := runAmd64V128WithOptions(t, m, nil, opts); got != want {
							t.Fatalf("got %x, want %x", got, want)
						}
						var stats ModuleStats
						opts.Stats = &stats
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							t.Fatal(err)
						}
						defer cm.CodeImage.Close()
						selected := stats.Funcs[0].Peephole["simd-shuffle-align"]
						if features == 0 && selected != 0 || features != 0 && selected != 1 {
							t.Fatalf("align hits=%d for features=%x", selected, features)
						}
					})
				}
			}
		}
	}
}

func TestSIMDAlignShuffleRejectsNoncontiguousMask(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{0, 0x20, 0, 0x20, 0}
	body = append(body, simdOp(13)...)
	for i := byte(0); i < 16; i++ {
		body = append(body, i)
	}
	body[len(body)-1] = 17
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}, body)
	stats := compileWithStats(t, m, false)
	if stats.Funcs[0].Peephole["simd-shuffle-align"] != 0 {
		t.Fatal("noncontiguous mask selected PALIGNR")
	}
	var input [16]byte
	for i := range input {
		input[i] = byte(i * 13)
	}
	want := input
	want[15] = input[1]
	if got := runAmd64V128(t, m, &input); got != want {
		t.Fatalf("got %x, want %x", got, want)
	}
}
