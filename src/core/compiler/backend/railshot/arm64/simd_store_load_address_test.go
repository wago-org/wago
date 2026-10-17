//go:build arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestSIMDStoreLoadAddressReuse(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, narrow := range []bool{false, true} {
					typ := wasm.I32
					if wide {
						typ = wasm.I64
					}
					body := []byte{0, 0x20, 0, 0xfd, 0x0c}
					body = append(body, make([]byte, 16)...)
					body = append(body, 0xfd, 0x0b, 0, 0, 0x20, 0, 0xfd, 0, 0, 0, 0xfd, 0x1b, 3, 0x0b)
					m := modMem(t, 1, []wasm.ValType{typ}, []wasm.ValType{wasm.I32}, body)
					m.Memories[0].Shared = shared
					m.Memories[0].Limits.Addr64 = wide
					m.Memories[0].Limits.HasMax = true
					m.Memories[0].Limits.Max = 2
					var stats ModuleStats
					c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Profile: profileEnabled, SourceMaps: profileEnabled, Optimizations: map[string]bool{"simd-store-load-address": enabled, "simd-load-extract": narrow}})
					if err != nil {
						t.Fatal(err)
					}
					want := 0
					if enabled {
						want = 1
					}
					if got := stats.Funcs[0].Peephole["simd-store-load-address"]; got != want {
						t.Fatalf("wide=%t shared=%t enabled=%t narrow=%t: address reuses=%d, want %d", wide, shared, enabled, narrow, got, want)
					}

					if enabled {
						wantOpcode := uint32(0x3ce06800)
						if narrow {
							wantOpcode = 0xb8606800
						}
						found := false
						for offset := 0; offset+4 <= len(c.Code); offset += 4 {
							if binary.LittleEndian.Uint32(c.Code[offset:])&0xffe0fc00 == wantOpcode {
								found = true
								break
							}
						}
						if !found {
							t.Fatal("address reuse omitted the actual guest load")
						}
					}

					c.CodeImage.Close()
				}
			}
		}
	}
}
