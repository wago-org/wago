//go:build amd64 && !tinygo && (linux || darwin || windows)

package semanticcorpus

import (
	"testing"
	"time"

	wago "github.com/wago-org/wago"
)

// The non-destructive BMI2 rotate path must remain an optimization, not a
// correctness requirement. Exercise the published BLAKE3 vectors through the
// legacy destructive rotate lowering together with interval-region residency.
func TestBLAKE3LegacyRotateRollback(t *testing.T) {
	manifest := loadManifest(t)
	for _, mod := range manifest.Modules {
		if mod.ID != "blake3/hash" {
			continue
		}
		wasm, err := readArtifact(CorpusRoot(), mod)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := wago.Compile(wago.NewRuntimeConfig().WithOptimization("bmi2-rorx", false), wasm)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		defer compiled.Close()
		if err := runVectors(compiled, mod, time.Duration(mod.Limits.TimeoutMS)*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("blake3/hash semantic corpus case is missing")
}
