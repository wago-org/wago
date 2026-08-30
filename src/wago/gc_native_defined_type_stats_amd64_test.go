//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_gcstats

package wago

import "testing"

func TestGCNativeNonFinalDefinedCastAndTestAvoidHelpers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		module []byte
	}{
		{"cast", gcNonFinalRefCastInstructionBenchmarkModule()},
		{"test", gcNonFinalRefTestInstructionBenchmarkModule()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), tc.module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{DisableCollection: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			instance.SetGCHelperStatsTracking(true)
			defer instance.SetGCHelperStatsTracking(false)
			const iterations = 1000
			got, err := instance.Invoke("run", iterations)
			if err != nil || len(got) != 1 || got[0] != iterations {
				t.Fatalf("run = %v, %v; want [%d]", got, err, iterations)
			}
			if stats := instance.GCHelperStats(); stats.Calls != 0 {
				t.Fatalf("steady-state non-final %s used synchronous GC helpers: %+v", tc.name, stats)
			}
		})
	}
}
