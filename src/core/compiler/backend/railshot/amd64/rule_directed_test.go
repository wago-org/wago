//go:build (linux || darwin || windows) && amd64 && !tinygo

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/ruleguide"
	"golang.org/x/sys/cpu"
)

func ruleGuideCompiler(features shared.AMD64Features) ruleguide.Compiler {
	return func(raw []byte, disabled bool, rule string) (ruleguide.Artifact, error) {
		savedSwap, savedSWAR := byteSwapCoverEnabled, swarMaskTestEnabled
		defer func() { byteSwapCoverEnabled, swarMaskTestEnabled = savedSwap, savedSWAR }()
		byteSwapCoverEnabled, swarMaskTestEnabled = true, true
		if disabled {
			switch rule {
			case "i32-bswap-tee":
				byteSwapCoverEnabled = false
			case "swar-mask-test":
				swarMaskTestEnabled = false
			default:
				return ruleguide.Artifact{}, fmt.Errorf("no kill switch for %s", rule)
			}
		}
		m, err := wasm.DecodeModule(raw)
		if err != nil {
			return ruleguide.Artifact{}, err
		}
		if err = wasm.ValidateModule(m); err != nil {
			return ruleguide.Artifact{}, err
		}
		var stats ModuleStats
		opts := CompileOptions{Workers: 1, DeferCodeMapping: true, AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"inline": false}}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		cm, err := CompileModuleWith(m, opts)
		if err != nil {
			return ruleguide.Artifact{}, err
		}
		a := ruleguide.Artifact{Code: cm.Code, Entry: cm.Entry[0], Selection: -1, FrameBytes: -1, Spills: -1, LiteralBytes: -1, Required: cm.RequiredAMD64Features, SelectedFeatures: uint32(features)}
		if diagnosticsEnabled {
			a.Selection = stats.Funcs[0].Peephole[rule]
			for kind, count := range stats.Funcs[0].Calls {
				if kind == callKindInline {
					a.InlineCalls += count
				} else {
					a.Calls += count
				}
			}
			a.FrameBytes = stats.Funcs[0].FrameBytes
			a.Spills = stats.Funcs[0].Spills
			a.LiteralBytes = stats.Funcs[0].NativeSize.LiteralPoolBytes
			for _, f := range stats.Funcs {
				a.Shared = append(a.Shared, f.SharedScalar)
			}
		}
		return a, nil
	}
}
func TestRuleDirectedRecipes(t *testing.T) {
	ruleguide.Run(t, "sse2", ruleGuideCompiler(0))
	if cpu.X86.HasAVX && cpu.X86.HasAVX2 {
		t.Run("avx2", func(t *testing.T) { ruleguide.Run(t, "avx2", ruleGuideCompiler(shared.AMD64AVX|shared.AMD64AVX2)) })
	}
}
func BenchmarkRuleDirectedYield(b *testing.B) { ruleguide.Yield(b, ruleGuideCompiler(0)) }
func BenchmarkRuleDirectedPairs(b *testing.B) { ruleguide.Benchmark(b, ruleGuideCompiler(0)) }
