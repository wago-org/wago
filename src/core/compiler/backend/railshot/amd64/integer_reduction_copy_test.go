//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func integerReductionOldCopyFixture(t *testing.T, tee bool) *wasm.Module {
	body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40, 0x20, 1}
	if tee {
		body = append(body, 0x22, 3, 0xad, 0x20, 2, 0x7c, 0x21, 2)
	} else {
		body = append(body, 0x21, 3, 0x20, 1, 0xad, 0x20, 2, 0x7c, 0x21, 2)
	}
	body = append(body, 0x20, 1, 0x41, 3, 0x6a, 0x21, 1, 0x20, 0, 0x41, 0x7f, 0x6a, 0x22, 0, 0x0d, 0, 0x0b, 0x0b, 0x20, 3, 0xad, 0x0b)
	return mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, body)
}

func TestIntegerReductionOldRecurrenceCopy(t *testing.T) {
	saved := integerReductionLoopEnabled
	defer func() { integerReductionLoopEnabled = saved }()
	for _, tee := range []bool{false, true} {
		m := integerReductionOldCopyFixture(t, tee)
		for _, features := range []shared.AMD64Features{0, shared.AMD64SSE41, shared.AMD64ModernBaseline} {
			for _, n := range []uint32{0, 1, 2, 4, 31, 32} {
				for _, start := range []uint32{7, 0xfffffff8} {
					want := uint64(0)
					if n != 0 {
						want = uint64(start + 3*(n-1))
					}
					for _, on := range []bool{false, true} {
						integerReductionLoopEnabled = on
						var stats ModuleStats
						got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: optionalTestStats(&stats)}, nil, uint64(n), uint64(start), uint64(123))
						if err != nil || got != want {
							t.Fatal("old recurrence copy", tee, features, n, start, on, got, want, err)
						}
						if on && n >= 2 && diagnosticsEnabled && stats.Funcs[0].Peephole["integer-reduction-loop"] != 0 {
							t.Fatal("old copy must stay scalar", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
