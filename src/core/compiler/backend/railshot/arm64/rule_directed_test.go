//go:build (linux || darwin || windows) && arm64 && !tinygo

package arm64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/ruleguide"
)

func ruleGuideCompiler(raw []byte, disabled bool, rule string) (ruleguide.Artifact, error) {
	saved := swarMaskTestEnabled
	defer func() { swarMaskTestEnabled = saved }()
	swarMaskTestEnabled = true
	if disabled {
		if rule != "swar-mask-test" {
			return ruleguide.Artifact{}, fmt.Errorf("no kill switch for %s", rule)
		}
		swarMaskTestEnabled = false
	}
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		return ruleguide.Artifact{}, err
	}
	if err = wasm.ValidateModule(m); err != nil {
		return ruleguide.Artifact{}, err
	}
	var stats ModuleStats
	opts := CompileOptions{Workers: 1, DeferCodeMapping: true, Optimizations: map[string]bool{"inline": false}}
	if diagnosticsEnabled {
		opts.Stats = &stats
	}
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		return ruleguide.Artifact{}, err
	}
	a := ruleguide.Artifact{Code: cm.Code, Entry: cm.Entry[0], Selection: -1, FrameBytes: -1, Spills: -1, LiteralBytes: -1}
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
	if rule == "i32-bswap-tee" {
		a.Selection = -2
	} // numerical execution, no ARM64 rule claim.
	return a, nil
}
func TestRuleDirectedRecipes(t *testing.T)    { ruleguide.Run(t, "arm64", ruleGuideCompiler) }
func BenchmarkRuleDirectedYield(b *testing.B) { ruleguide.Yield(b, ruleGuideCompiler) }
func BenchmarkRuleDirectedPairs(b *testing.B) { ruleguide.Benchmark(b, ruleGuideCompiler) }
