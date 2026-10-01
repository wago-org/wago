//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func wideIndependentFixture(t *testing.T, arithmetic byte, zeroCounter bool) *wasm.Module {
	body := []byte{1, 1, 0x7c, 0x20, 4, 0x21, 6, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x2b, 0, 0, 0x20, 4, arithmetic, 0x20, 5, 0xa0, 0x22, 6, 0x39, 0, 0,
		0x20, 0, 0x41, 8, 0x6a, 0x21, 0, 0x20, 1, 0x41, 8, 0x6a, 0x21, 1,
		0x20, 2, 0x41, 1, 0x6a, 0x22, 2}
	if !zeroCounter {
		body = append(body, 0x20, 3, 0x47)
	}
	body = append(body, 0x0d, 0, 0x0b,
		0x41, 0, 0x20, 2, 0x36, 2, 0, 0x41, 4, 0x20, 0, 0x36, 2, 0, 0x41, 8, 0x20, 1, 0x36, 2, 0,
		0x20, 6, 0xbd, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.F64, wasm.F64}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestWideIndependentPlanAndAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved, whole, tail := regionWideIndependentEnabled, regionLoopEnabled, regionWideCheckedTailEnabled
	defer func() {
		regionWideIndependentEnabled, regionLoopEnabled, regionWideCheckedTailEnabled = saved, whole, tail
	}()
	regionLoopEnabled = false
	regionWideCheckedTailEnabled = true
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, on := range []bool{false, true} {
			regionWideIndependentEnabled = on
			var stats ModuleStats
			cm, err := CompileModuleWith(wideIndependentFixture(t, 0xa2, false), CompileOptions{Stats: &stats, AMD64FeaturesSet: true, AMD64Features: features})
			if err != nil {
				t.Fatal(err)
			}
			want := on && features&shared.AMD64AVX != 0
			if (stats.Funcs[0].Peephole["region-loop-wide-independent"] != 0) != want {
				t.Fatal("wide independent admission", features, on, stats.Funcs[0].Peephole)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
		}
	}
	for _, tc := range []struct {
		adjacent, scalar, wide bool
		count                  uint32
	}{{false, false, false, 2}, {false, false, true, 4}, {true, false, false, 1}, {true, false, true, 2}, {false, true, false, 1}} {
		p := regionLoopPlan{adjacent: tc.adjacent, scalar: tc.scalar, wide: tc.wide}
		if p.iterationsPerVector() != tc.count {
			t.Fatal("group factor", tc)
		}
	}
}
