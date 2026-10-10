//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestNarrowStoreConstantRoundTrip(t *testing.T) {
	for _, v := range []int32{-1, -17, -65536, -65537, 0x12345678} {
		for _, tc := range []struct {
			store, load byte
			mask        uint64
		}{{0x3a, 0x2d, 255}, {0x3b, 0x2f, 65535}, {0x36, 0x28, 0xffffffff}} {
			for _, off := range []uint32{0, 8, 0x1234} {
				body := []byte{0, 0x20, 0, 0x41}
				body = append(body, wasmtest.SLEB32(v)...)
				body = append(body, tc.store, 0)
				body = append(body, wasmtest.ULEB(off)...)
				body = append(body, 0x20, 0, tc.load, 0)
				body = append(body, wasmtest.ULEB(off)...)
				body = append(body, 0x0b)
				m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
				for _, guard := range []bool{false, true} {
					for _, addr := range []uint64{0, 1, 123} {
						got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, CompactNative: false}, addr)
						want := uint64(uint32(v)) & tc.mask
						if err != nil || uint64(uint32(got)) != want {
							t.Fatalf("v=%d off=%d addr=%d guard=%v got=%x want=%x err=%v", v, off, addr, guard, got, want, err)
						}
					}
				}
			}
		}
	}
}

func TestNarrowStoreImmediatePolicy(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x41, 0x6f, 0x36, 2, 0, 0x20, 0, 0x28, 2, 0, 0x0b})
	for _, enabled := range []bool{false, true} {
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{CompactNative: false, Stats: stats, Optimizations: map[string]bool{"single-negative-move32": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		hits := stats.Funcs[0].Peephole["single-negative-move32"]
		if enabled && hits != 1 || !enabled && hits != 0 {
			t.Fatalf("enabled=%v hits=%d", enabled, hits)
		}
	}
}
