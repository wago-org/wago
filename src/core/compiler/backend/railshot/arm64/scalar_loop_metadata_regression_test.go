//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"bytes"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestScalarBranchLoopBoundsPrefixMetadata(t *testing.T) {
	if profileEnabled {
		t.Skip("unrolling excludes profiling")
	}
	for _, kind := range []string{"traps", "relocations", "exits"} {
		t.Run(kind, func(t *testing.T) {
			f := branchLoopProofFixture()
			original := append([]byte{}, f.a.B...)
			const prefix = 4096
			f.a.B = append(make([]byte, prefix), original...)
			f.ctrl[1].controlSite = prefix
			f.ctrl[0] = ctrlFrame{kind: cfBlock}
			f.appendFrameEnd(&f.ctrl[0], prefix+4, true)
			for i := 0; i < 1024; i++ {
				switch kind {
				case "traps":
					f.sc.trapSites[trapDivZero] = append(f.sc.trapSites[trapDivZero], trapSite{branch: uint32(i * 4)})
				case "relocations":
					f.relocs = append(f.relocs, callReloc{at: uint32(i * 4)})
				case "exits":
					f.appendFrameEnd(&f.ctrl[0], i*4, true)
				}
			}
			before := append([]byte{}, f.a.B...)
			if f.unrollScalarBranches(&f.ctrl[1]) || !bytes.Equal(before, f.a.B) {
				t.Fatal("unbounded prefix metadata admitted or changed code on fallback")
			}
		})
	}
}

func scalarDivisionHistoryModule(t testing.TB, loops int) *wasm.Module {
	body := []byte{1, 3, 0x7f}
	for i := 0; i < loops; i++ {
		body = append(body, 0x02, 0x40, 0x03, 0x40,
			0x20, 3, 0x20, 0, 0x4f, 0x0d, 1,
			0x20, 1, 0x20, 2, 0x6e, 0x21, 5,
			0x20, 4, 0x20, 5, 0x41, 0, 0x47, 0x6a, 0x21, 4,
			0x20, 5, 0x41, 1, 0x71, 0x04, 0x40, 0x20, 4, 0x41, 3, 0x6a, 0x21, 4, 0x0b,
			0x20, 2, 0x41, 1, 0x6b, 0x21, 2, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b)
	}
	body = append(body, 0x20, 4, 0x0b)
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
	return modFuncs(t, funcDef{params, []wasm.ValType{wasm.I32}, body}, funcDef{params, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x0b}})
}

func BenchmarkScalarLoopDivisionHistory(b *testing.B) {
	for _, loops := range []int{4000, 16000} {
		m := scalarDivisionHistoryModule(b, loops)
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("loops=%d/unroll=%t", loops, enabled), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					c, err := CompileModuleWith(m, CompileOptions{Workers: 1, Optimizations: map[string]bool{"scalar-loop-unroll": enabled}})
					if err != nil {
						b.Fatal(err)
					}
					if c.CodeImage != nil {
						c.CodeImage.Close()
					}
				}
			})
		}
	}
}
