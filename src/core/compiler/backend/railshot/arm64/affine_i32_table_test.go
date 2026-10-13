//go:build arm64

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestAffineI32TableCallSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, exported := range []bool{false, true} {
		for _, mutate := range []bool{false, true} {
			for _, inline := range []bool{false, true} {
				for _, immutable := range []bool{false, true} {
					for _, abi := range []bool{false, true} {
						t.Run(fmt.Sprintf("export%t/mutate%t/inline%t/immutable%t/abi%t", exported, mutate, inline, immutable, abi), func(t *testing.T) {
							m, err := wasm.DecodeModule(wasmtest.AffineI32Table(1, []uint32{1, 3}, 2, exported, false))
							if err != nil {
								t.Fatal(err)
							}
							if mutate {
								m.Code[2].BodyBytes = append([]byte{0x20, 1, 0xd0, 0x70, 0x26, 0}, m.Code[2].BodyBytes...)
							}
							var stats ModuleStats
							c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"inline": inline, "immutable-table": immutable, "reg-abi": abi}})
							if err != nil {
								t.Fatal(err)
							}
							c.CodeImage.Close()
							want := 0
							if !exported && !mutate && inline && immutable {
								want = 1
							}
							// call, prefix and loop sites qualify; the incompatible signature never does.
							for _, idx := range []int{2, 3, 5} {
								if got := stats.Funcs[idx].Peephole["affine-i32-table-call-inline"]; got != want {
									t.Fatalf("fn%d inlined %d, want %d", idx, got, want)
								}
							}
							if got := stats.Funcs[4].Peephole["affine-i32-table-call-inline"]; got != 0 {
								t.Fatalf("inlined wrong-signature call: %d", got)
							}
						})
					}
				}
			}
		}
	}
}
