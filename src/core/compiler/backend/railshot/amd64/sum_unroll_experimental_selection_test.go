//go:build linux && amd64 && wago_sumunroll

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"testing"
)

func selectSumUnroll(t testing.TB) {
	t.Helper()
	saved := sumUnrollExperiment
	t.Cleanup(func() { sumUnrollExperiment = saved })
	factor, chains := 0, 0
	switch v := os.Getenv("WAGO_SUM_VARIANT"); v {
	case "", "baseline":
	case "A":
		factor, chains = 8, 4
	case "B":
		factor, chains = 8, 8
	case "C":
		factor, chains = 2, 2
	case "D":
		factor, chains = 16, 4
	case "E":
		factor, chains = 16, 8
	default:
		t.Fatalf("unknown sum variant %q", v)
	}
	sumUnrollExperiment.factor = factor
	sumUnrollExperiment.chains = chains
	sumUnrollExperiment.budget = 512
}

func sumUnrollBaseline(t testing.TB, m *wasm.Module) *sumNative {
	saved := sumUnrollExperiment
	sumUnrollExperiment.factor = 0
	defer func() { sumUnrollExperiment = saved }()
	return sumUnrollNative(t, m, CompileOptions{})
}

func TestSumUnrollSelectedPath(t *testing.T) {
	selectSumUnroll(t)
	requireCompilerDiagnostics(t)
	for _, pressure := range []int{0, 12} {
		var stats ModuleStats
		cm, err := CompileModuleWith(sumUnrollModule(t, pressure), CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		wantExperimental := sumUnrollExperiment.factor != 0 && !(pressure == 12 && sumUnrollExperiment.chains == 8)
		got := stats.Funcs[0].Peephole["experimental-linear-sum"] != 0
		if got != wantExperimental {
			t.Fatalf("pressure=%d selection=%v want=%v: %v", pressure, got, wantExperimental, stats.Funcs[0].Peephole)
		}
	}
}
