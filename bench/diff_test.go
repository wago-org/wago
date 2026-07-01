package wagobench

import (
	"testing"

	"github.com/wago-org/wago/src/wago"
)

// TestX64DifferentialCorpus compiles every corpus module with both the amd64 and
// the x64 (WARP-port) backend, instantiates both, and asserts that every exported
// function returns identical results (or both trap) across a range of integer
// argument vectors. A divergence means the x64 backend miscompiled something.
func TestX64DifferentialCorpus(t *testing.T) {
	mods := loadCorpusT(t)
	if len(mods) == 0 {
		t.Skip("no corpus modules present")
	}
	defaultArgVectors := [][]uint64{
		{}, {0}, {1}, {2}, {5}, {0xFFFFFFFF}, {7, 3}, {0, 1}, {100, 7}, {3, 3, 3},
	}
	for _, m := range mods {
		t.Run(m.name(), func(t *testing.T) {
			cAMD, err := wago.CompileWithConfig(wago.NewRuntimeConfig().WithX64(false), m.bytes)
			if err != nil {
				t.Fatalf("amd64 compile: %v", err)
			}
			cX64, err := wago.CompileWithConfig(wago.NewRuntimeConfig().WithX64(true), m.bytes)
			if err != nil {
				t.Fatalf("x64 compile: %v", err)
			}
			inAMD, err := wago.Instantiate(cAMD, nil)
			if err != nil {
				t.Skipf("amd64 instantiate (imports?): %v", err)
			}
			defer inAMD.Close()
			inX64, err := wago.Instantiate(cX64, nil)
			if err != nil {
				t.Fatalf("x64 instantiate: %v", err)
			}
			defer inX64.Close()

			manifestArgs := map[string][][]uint64{}
			for _, e := range m.Exec {
				args := make([]uint64, len(e.Args))
				for i, a := range e.Args {
					args[i] = wago.I32(a)
				}
				manifestArgs[e.Export] = append(manifestArgs[e.Export], args)
			}
			for _, export := range cAMD.ExportedFunctions() {
				params, _, err := cAMD.Signature(export)
				if err != nil {
					continue
				}
				argVectors := manifestArgs[export]
				if len(argVectors) == 0 {
					argVectors = defaultArgVectors
				}
				for _, av := range argVectors {
					if len(av) != len(params) {
						continue
					}
					rAMD, eAMD := inAMD.Invoke(export, av...)
					rX64, eX64 := inX64.Invoke(export, av...)
					if (eAMD == nil) != (eX64 == nil) {
						t.Fatalf("%s(%v): trap mismatch amd64=%v x64=%v", export, av, eAMD, eX64)
					}
					if eAMD != nil {
						continue // both trapped
					}
					if len(rAMD) != len(rX64) {
						t.Fatalf("%s(%v): result count amd64=%d x64=%d", export, av, len(rAMD), len(rX64))
					}
					for i := range rAMD {
						if rAMD[i] != rX64[i] {
							t.Fatalf("%s(%v) result[%d]: amd64=%#x x64=%#x", export, av, i, rAMD[i], rX64[i])
						}
					}
				}
			}
		})
	}
}

// loadCorpusT is loadCorpus for a *testing.T.
func loadCorpusT(t *testing.T) []corpusModule {
	return loadCorpus(t)
}
