//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestPrivateConstantVersionsStopAtWritesAndLoopUpdates(t *testing.T) {
	old := privateConstantsEnabled
	t.Cleanup(func() { privateConstantsEnabled = old })
	for _, mode := range []string{"write-after-loop", "write-in-loop", "write-in-conditional"} {
		body := []byte{1, 2, 0x7f, 0x23, 0, 0x41, 7, 0x6b, 0x21, 1}
		switch mode {
		case "write-after-loop":
			body = append(body, 0x03, 0x7f, 0x20, 1, 0x41, 0, 0x0d, 0, 0x0b, 0x20, 0, 0x21, 1, 0x20, 1, 0x6a, 0x0b)
		case "write-in-loop":
			body = append(body, 0x20, 0, 0x21, 2, 0x02, 0x40, 0x20, 2, 0x45, 0x0d, 0, 0x03, 0x40, 0x20, 1, 0x41, 3, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6b, 0x22, 2, 0x0d, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
		case "write-in-conditional":
			body = append(body, 0x20, 0, 0x04, 0x40, 0x41, 31, 0x21, 1, 0x05, 0x20, 1, 0x1a, 0x0b, 0x20, 1, 0x0b)
		}
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const, I32: 17}}}}}
		for _, enabled := range []bool{false, true} {
			privateConstantsEnabled = enabled
			for _, input := range []uint64{0, 1, 2, 8, 0xdeadbeef00000002} {
				want := uint64(10)
				switch mode {
				case "write-after-loop":
					want += uint64(uint32(input))
				case "write-in-loop":
					want += 3 * uint64(uint32(input))
				case "write-in-conditional":
					if uint32(input) != 0 {
						want = 31
					}
				}
				var stats ModuleStats
				opts := CompileOptions{}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runPrivateGlobalCells(t, m, opts, input, 17)
				if err != nil || got != want {
					t.Fatalf("mode=%s enabled=%v input=%x got=%x want=%x err=%v", mode, enabled, input, got, want, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["private-local-constant-fact"] > 0) != (enabled && mode != "write-in-loop") {
					t.Fatalf("mode=%s enabled=%v stats=%v", mode, enabled, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}
