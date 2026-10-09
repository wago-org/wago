//go:build linux && amd64 && wago_profile && wago_codegenstats

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestSIMDOutputBorrowWithLiveVectorPressure(t *testing.T) {
	previous := simdOutputBorrowEnabled
	defer func() { simdOutputBorrowEnabled = previous }()
	for _, features := range []shared.AMD64Features{0, shared.AMD64SSSE3, shared.AMD64ModernBaseline} {
		for _, compact := range []bool{false, true} {
			for _, op := range []struct {
				sub   uint32
				align bool
			}{{sub: 77}, {sub: 171}, {sub: 251}, {sub: 13}, {sub: 13, align: true}} {
				sub := op.sub
				t.Run(fmt.Sprintf("features=%x/compact=%v/op=%d/align=%v", features, compact, sub, op.align), func(t *testing.T) {
					// Seventeen vectors plus one error accumulator; parameter 0 is unused.
					body := []byte{2, 17, 0x7b, 1, 0x7f}
					var vectors [16][16]byte
					for i := range vectors {
						for j := range vectors[i] {
							vectors[i][j] = byte(i*19 + j*7)
						}
						body = append(body, v128ConstBytes(vectors[i])...)
						body = append(body, 0x21, byte(i+1))
					}
					// Keep sixteen older values live while lowering the borrowed operation.
					salt := [16]byte{0xd3, 0x76, 9, 0xff, 0x81, 0x4a, 0x22, 7, 6, 5, 4, 3, 2, 1, 0x80, 0x17}
					for i := 1; i <= 16; i++ {
						body = append(body, 0x20, byte(i))
						body = append(body, v128ConstBytes(salt)...)
						body = append(body, simdOp(81)...)
					}
					body = append(body, 0x20, 1)
					if sub == 171 {
						body = append(body, 0x41, 3)
					}
					if sub == 13 {
						body = append(body, 0x20, 2)
					}
					body = append(body, simdOp(sub)...)
					if sub == 13 {
						if op.align {
							for lane := byte(14); lane < 30; lane++ {
								body = append(body, lane)
							}
						} else {
							body = append(body, []byte{0, 17, 2, 19, 4, 21, 6, 23, 8, 25, 10, 27, 12, 29, 14, 31}...)
						}
					}
					body = append(body, 0x21, 17)
					addMismatch := func(v [16]byte) {
						body = append(body, v128ConstBytes(v)...)
						body = append(body, simdOp(81)...)
						body = append(body, simdOp(83)...)
						body = append(body, 0x20, 18, 0x72, 0x21, 18)
					}
					for i := 15; i >= 0; i-- {
						addMismatch(applyV128BooleanBytes(81, vectors[i], salt))
					}
					// Check both the old stack values and every original local.
					for i := range vectors {
						body = append(body, 0x20, byte(i+1))
						addMismatch(vectors[i])
					}
					body = append(body, 0x20, 18, 0x04, 0x40, 0x00, 0x0b, 0x20, 17, 0x0b)
					m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}, body)
					opts := CompileOptions{AMD64Features: features, AMD64FeaturesSet: true, CompactNative: compact, Optimizations: map[string]bool{"v128-pins": true, "reg-abi": true}}
					simdOutputBorrowEnabled = false
					want := runAmd64V128WithOptions(t, m, nil, opts)
					simdOutputBorrowEnabled = true
					got := runAmd64V128WithOptions(t, m, nil, opts)
					if got != want {
						t.Fatalf("on=%x off=%x", got, want)
					}
					var stats ModuleStats
					opts.Stats = &stats
					opts.Profile = true
					opts.SourceMaps = true
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					fs := stats.Funcs[0]
					vectorSpills := 0
					for _, site := range fs.CodeSites {
						if site.Kind == "vector-spill" {
							vectorSpills++
						}
					}
					if vectorSpills == 0 {
						t.Fatal("fixture did not create register pressure")
					}
					if fs.Peephole["simd-output-borrow"]+fs.Peephole["simd-shuffle-output-borrow"]+fs.Peephole["simd-convert-input-borrow"]+fs.Peephole["simd-shuffle-align"] == 0 {
						t.Fatal("pressure fixture did not select borrowing")
					}
				})
			}
		}
	}
}
