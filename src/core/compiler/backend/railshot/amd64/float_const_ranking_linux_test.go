//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestFloatConstRankingControlAndBits(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := floatConstRankingEnabled
	defer func() { floatConstRankingEnabled = old }()
	for _, bits := range []uint64{math.Float64bits(1.5), 0x8000000000000000, 0x7ff8000000001234, 0x7ff0000000004321} {
		body := []byte{0} // no declared locals
		literal := func(bits uint64) {
			body = append(body, 0x44)
			body = binary.LittleEndian.AppendUint64(body, bits)
		}
		for _, value := range []float64{101, 102} {
			literal(math.Float64bits(value))
			body = append(body, 0x1a)
		}
		body = append(body, 0x20, 0, 0x04, 0x7c) // if condition, f64 result
		for i := 0; i < 3; i++ {
			literal(bits)
			literal(math.Float64bits(.5))
			body = append(body, 0xa2, 0x1a)
		}
		literal(bits)
		body = append(body, 0x05)
		literal(math.Float64bits(.5))
		body = append(body, 0x0b, 0xbd, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, body)
		for _, features := range []shared.AMD64Features{0, shared.AMD64AVX, shared.AMD64ModernBaseline} {
			for _, compact := range []bool{false, true} {
				for _, enabled := range []bool{false, true} {
					floatConstRankingEnabled = enabled
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, AMD64FeaturesSet: true, AMD64Features: features, CompactNative: compact, Stats: &stats})
					if err != nil {
						t.Fatal(err)
					}
					// Repeated invocation and both control arms exercise prologue
					// initialization independently of which arm first reads a cache.
					for _, condition := range []uint64{0, 1, 0, 1} {
						want := bits
						if condition == 0 {
							want = math.Float64bits(.5)
						}
						if got := runCompiledAmd64u(t, cm, condition); got != want {
							t.Fatal("cache changed raw bits", features, compact, enabled, condition, got, want)
						}
					}
					if (stats.Funcs[0].Peephole["float-preload-ranked"] == 1) != enabled {
						t.Fatal("ranking admission", enabled, stats.Funcs[0].Peephole)
					}
					if cm.CodeImage != nil {
						cm.CodeImage.Close()
					}
				}
			}
		}
	}
}

func TestFloatConstRankingLargeBodyKeepsLegacy(t *testing.T) {
	old := floatConstRankingEnabled
	floatConstRankingEnabled = true
	defer func() { floatConstRankingEnabled = old }()
	code := floatRankingCode(101, 102)
	code = code[:len(code)-1]
	use := floatRankingCode(.5)
	for len(code) <= floatConstRankingMaxBytes {
		code = append(code, use[:len(use)-1]...)
	}
	code = append(code, 0x0b)
	f := fn{a: &encoderamd64.Asm{}, s: newStack()}
	f.preloadFloatConsts(code)
	if len(f.fconsts) != 2 || uint64(f.fconsts[0].bits) != math.Float64bits(101) || uint64(f.fconsts[1].bits) != math.Float64bits(102) {
		t.Fatal("large-body legacy choice changed", f.fconsts)
	}
}
