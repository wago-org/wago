//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSIMDReductionBorrowPreservesPinnedVector(t *testing.T) {
	vectors := [][16]byte{{}, {1}, {0x80, 0, 0xff, 0x80, 0, 1, 0x80, 0x7f, 0xff, 0x80, 1, 0x80, 0, 0xff, 0x80, 0xff}}
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, sub := range []uint32{83, 100, 132, 164, 196} {
			for vi, v := range vectors {
				t.Run(fmt.Sprintf("features=%x/sub=%d/vector=%d", features, sub, vi), func(t *testing.T) {
					var want uint64
					if sub == 83 {
						for _, b := range v {
							if b != 0 {
								want = 1
							}
						}
					} else {
						width := map[uint32]int{100: 1, 132: 2, 164: 4, 196: 8}[sub]
						for lane := 0; lane < 16/width; lane++ {
							if v[(lane+1)*width-1]&0x80 != 0 {
								want |= 1 << lane
							}
						}
					}
					// Keep the original vector live after reduction and verify every bit.
					body := []byte{2, 1, 0x7b, 1, 0x7f}
					body = append(body, v128ConstBytes(v)...)
					body = append(body, 0x21, 0, 0x20, 0)
					body = append(body, simdOp(sub)...)
					body = append(body, 0x21, 1, 0x20, 0)
					body = append(body, v128ConstBytes(v)...)
					body = append(body, simdOp(81)...)
					body = append(body, simdOp(83)...)
					body = append(body, 0x04, 0x7f, 0x41, 0x7f, 0x05, 0x20, 1, 0x0b, 0x0b)
					m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
					for _, enabled := range []bool{false, true} {
						var stats ModuleStats
						cm, err := CompileModuleWith(m, CompileOptions{AMD64Features: features, AMD64FeaturesSet: true, Stats: &stats, Optimizations: map[string]bool{"v128-pins": true, "reg-abi": true, "simd-reduction-borrow": enabled}})
						if err != nil {
							t.Fatal(err)
						}
						if cm.CodeImage != nil {
							defer cm.CodeImage.Close()
						}
						// i16x8.bitmask packs in place in an owned register so
						// allocation cannot clobber a borrowed reduction scratch.
						wantBorrow := enabled && sub != 132
						if (stats.Funcs[0].Peephole["simd-reduction-borrow"] > 0) != wantBorrow {
							t.Fatalf("borrow selection enabled=%v stats=%v", enabled, stats.Funcs[0].Peephole)
						}
						if stats.Funcs[0].PinnedLocals == 0 {
							t.Fatal("fixture did not pin locals")
						}
						if got := runCompiledAmd64u(t, cm); got != want {
							t.Fatalf("enabled=%v got=%x want=%x input=%x/%x", enabled, got, want, binary.LittleEndian.Uint64(v[:8]), binary.LittleEndian.Uint64(v[8:]))
						}
					}
				})
			}
		}
	}
}
