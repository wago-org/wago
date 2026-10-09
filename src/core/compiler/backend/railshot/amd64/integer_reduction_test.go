//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The unchanged 311-byte wasm.fyi artifact is kept here for a permanent oracle.
//
//go:embed testdata/integer_reduction_linear_regression.wasm
var integerReductionOriginal []byte

func integerReductionFixture(t *testing.T, signed bool, result uint32) *wasm.Module {
	// Parameters: count, induction, i64 initial sum. Locals: map i32 and i64.
	// A zero count skips the do-while. Each lane adds a wrapped i32 map.
	ext := byte(0xad)
	if signed {
		ext = 0xac
	}
	body := []byte{2, 1, 0x7f, 1, 0x7e,
		0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0,
		0x03, 0x40,
		0x20, 1, 0x41, 7, 0x6c, 0x41, 1, 0x6a, 0x22, 3, ext, 0x22, 4,
		0x20, 2, 0x7c, 0x21, 2,
		0x20, 1, 0x41, 3, 0x6a, 0x21, 1,
		0x20, 0, 0x41, 0x7f, 0x6a, 0x22, 0, 0x0d, 0, 0x0b, 0x0b,
		0x20, byte(result)}
	if result == 0 || result == 1 || result == 3 {
		body = append(body, 0xad)
	}
	body = append(body, 0x0b)
	return mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, body)
}

func TestIntegerReductionMapsTailsOverflowAndExitLocals(t *testing.T) {
	saved := integerReductionLoopEnabled
	defer func() { integerReductionLoopEnabled = saved }()
	for _, signed := range []bool{false, true} {
		for result := uint32(0); result < 5; result++ {
			m := integerReductionFixture(t, signed, result)
			for _, features := range []shared.AMD64Features{0, shared.AMD64SSE41, shared.AMD64ModernBaseline} {
				for _, start := range []uint64{0, 0x7ffffff0, 0xfffffff0, 0xdeadbeef80000000} {
					for _, count := range []uint64{0, 1, 2, 3, 4, 7, 15, 16, 17, 31} {
						var want uint64
						for _, on := range []bool{false, true} {
							integerReductionLoopEnabled = on
							var stats ModuleStats
							cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: optionalTestStats(&stats)})
							if err != nil {
								t.Fatal(err)
							}
							got := runCompiledAmd64u(t, cm, count, start, ^uint64(0)-17)
							if cm.CodeImage != nil {
								cm.CodeImage.Close()
							}
							if !on {
								want = got
							} else if got != want {
								t.Fatal("mismatch", signed, result, features, start, count, got, want)
							}
							if on && features.Has(shared.AMD64SSE41) && diagnosticsEnabled && stats.Funcs[0].Peephole["integer-reduction-loop"] != 1 {
								t.Fatal("no vector path", stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestIntegerReductionOriginalArtifact(t *testing.T) {
	saved := integerReductionLoopEnabled
	defer func() { integerReductionLoopEnabled = saved }()
	hash := sha256.Sum256(integerReductionOriginal)
	if hex.EncodeToString(hash[:]) != "f75b7e038eb5bf4efe2bf0cad87c8bbcdd69d82a65c6ddc844d4daf9d573d5b4" {
		t.Fatal("original artifact changed")
	}
	m, err := wasm.DecodeModule(integerReductionOriginal)
	if err != nil {
		t.Fatal(err)
	}
	for _, features := range []shared.AMD64Features{0, shared.AMD64SSE41, shared.AMD64ModernBaseline} {
		for _, count := range []uint64{2, 3, 4, 7, 15, 16, 17, 31, 4096} {
			var want uint64
			for _, on := range []bool{false, true} {
				integerReductionLoopEnabled = on
				var stats ModuleStats
				got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: optionalTestStats(&stats)}, nil, count)
				if err != nil {
					t.Fatal(features, count, on, err)
				}
				if !on {
					want = got
				} else if got != want {
					t.Fatal("original artifact mismatch", features, count, got, want)
				}
				if count == 4096 && got != 4156416353 {
					t.Fatal("original oracle", got)
				}
				if on && features.Has(shared.AMD64SSE41) && diagnosticsEnabled && stats.Funcs[0].Peephole["integer-reduction-loop"] != 1 {
					t.Fatal("original loop not admitted", stats.Funcs[0].Peephole)
				}
			}
		}
	}
}
