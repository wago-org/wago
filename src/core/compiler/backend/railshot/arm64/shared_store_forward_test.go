//go:build arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestFixedSharedStoreForwardBoundaries(t *testing.T) {
	requireCompilerDiagnostics(t)
	if profileEnabled {
		t.Skip("profiling retains individual memory accesses")
	}
	for _, barrier := range []string{"none", "local-write", "fence", "growable", "different-address", "different-offset", "load8", "atomic-load"} {
		t.Run(barrier, func(t *testing.T) {
			body := []byte{0, 0x20, 0, 0x20, 1, 0x41, 5, 0x6a, 0x36, 2, 0}
			if barrier == "local-write" {
				body = append(body, 0x41, 16, 0x21, 0)
			}
			if barrier == "fence" {
				body = append(body, 0xfe, 3, 0)
			}
			address := byte(0)
			if barrier == "different-address" {
				address = 1
			}
			body = append(body, 0x20, address)
			switch barrier {
			case "different-offset":
				body = append(body, 0x28, 2, 4)
			case "load8":
				body = append(body, 0x2d, 0, 0)
			case "atomic-load":
				body = append(body, 0xfe, 0x10, 2, 0)
			default:
				body = append(body, 0x28, 2, 0)
			}
			body = append(body, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			m.Memories[0].Shared = true
			m.Memories[0].Limits.HasMax = true
			m.Memories[0].Limits.Max = 1
			if barrier == "growable" {
				m.Memories[0].Limits.Max = 2
			}
			for _, on := range []bool{false, true} {
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"store-forward": on}})
				if err != nil {
					t.Fatal(err)
				}
				if barrier == "none" {
					loads, stores := 0, 0
					const regs = uint32(31<<16 | 31<<5 | 31)
					for i := 0; i+4 <= len(cm.Code); i += 4 {
						word := binary.LittleEndian.Uint32(cm.Code[i:]) &^ regs
						if word == 0xb8606800 {
							loads++
						}
						if word == 0xb8206800 {
							stores++
						}
					}
					wantLoads := 1
					if on {
						wantLoads = 0
					}
					if stores != 1 || loads != wantLoads {
						t.Fatalf("on=%t actual stores=%d loads=%d want1,%d", on, stores, loads, wantLoads)
					}
				}
				cm.CodeImage.Close()
				want := 0
				if on && barrier == "none" {
					want = 1
				}
				if got := stats.Funcs[0].Peephole["fixed-shared-store-load-fwd"]; got != want {
					t.Fatalf("on=%t hits=%d want=%d", on, got, want)
				}
			}
		})
	}
}
