//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPrivateConstantAddressBoundsEndAtConditionalWrite(t *testing.T) {
	old := privateConstantsEnabled
	t.Cleanup(func() { privateConstantsEnabled = old })
	// Two direct load addresses retain the original register carrier. Both are
	// within the initial page. A later conditional definition must restore the
	// ordinary dynamic full-access check, including partial-page overrun.
	body := []byte{1, 1, 0x7f, 0x23, 0, 0x41, 7, 0x6b, 0x21, 1, 0x20, 1, 0x28, 0, 0, 0x1a, 0x20, 1, 0x28, 0, 4, 0x1a, 0x20, 0, 0x04, 0x40, 0x20, 0, 0x21, 1, 0x0b, 0x20, 1, 0x28, 0, 0, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const, I32: 17}}}}}
	for _, enabled := range []bool{false, true} {
		privateConstantsEnabled = enabled
		for _, input := range []uint64{0, 1, 65532, 65533, 65536, 0xfeedface00000001} {
			var stats ModuleStats
			opts := CompileOptions{}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runPrivateGlobalCells(t, m, opts, input, 17)
			trap := uint32(input) > 65532
			if (err != nil) != trap || !trap && got != 0 {
				t.Fatalf("enabled=%v input=%x got=%x err=%v wantTrap=%v", enabled, input, got, err, trap)
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["private-local-address-bound"] > 0) != enabled {
				t.Fatalf("enabled=%v stats=%v", enabled, stats.Funcs[0].Peephole)
			}
		}
	}
}

// Exercise source-offset matching with signed multi-byte immediates and wrapped
// sums at both page edges. The proof must cover the full second four-byte load.
func TestPrivateConstantInitializerAddressWrapAndFullAccess(t *testing.T) {
	old := privateConstantsEnabled
	t.Cleanup(func() { privateConstantsEnabled = old })
	for _, global := range []int32{0, 17, -1, 65528, 65532, 2147483647, -2147483648} {
		for _, delta := range []int32{7, -7, 127, -129} {
			for _, op := range []byte{0x6a, 0x6b} {
				body := []byte{1, 1, 0x7f, 0x23, 0, 0x41}
				body = append(body, wasmtest.SLEB32(delta)...)
				body = append(body, op, 0x21, 1, 0x20, 1, 0x28, 0, 0, 0x1a, 0x20, 1, 0x28, 0, 4, 0x0b)
				m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
				m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const, I32: global}}}}}
				address := uint32(global) + uint32(delta)
				if op == 0x6b {
					address = uint32(global) - uint32(delta)
				}
				for _, enabled := range []bool{false, true} {
					privateConstantsEnabled = enabled
					var stats ModuleStats
					opts := CompileOptions{}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runPrivateGlobalCells(t, m, opts, 0, uint64(uint32(global)))
					trap := address > 65528
					if (err != nil) != trap || !trap && got != 0 {
						t.Fatalf("global=%d delta=%d op=%x enabled=%v address=%d got=%x err=%v wantTrap=%v", global, delta, op, enabled, address, got, err, trap)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["private-local-constant-fact"] > 0) != enabled {
						t.Fatalf("source initializer fact missing: global=%d delta=%d op=%x enabled=%v stats=%v", global, delta, op, enabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
