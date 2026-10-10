//go:build wago_codegenstats

package amd64

import (
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestRepeatLinearLoadPHPCodegen reports actual backend admissions and spill
// traffic in the same PHP module used by the real-command probe.
func TestRepeatLinearLoadPHPCodegen(t *testing.T) {
	data, err := os.ReadFile("../../../../../../corpus/workloads/applications/php/php.wasm")
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModuleWithFeatures(data, wasm.ValidationFeatures{})
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.CodeImage != nil {
		defer compiled.CodeImage.Close()
	}
	var selected, functions, spills, reloads, bodyBytes int
	for _, f := range stats.Funcs {
		if f.Peephole["adjacent-repeat-linear-load"] > 0 {
			functions++
		}
		selected += f.Peephole["adjacent-repeat-linear-load"]
		spills += f.Spills
		reloads += f.Reloads
		bodyBytes += f.CodeBytes
	}
	t.Logf("enabled=%t selected_pairs=%d selected_functions=%d native_bytes=%d body_bytes=%d spills=%d reloads=%d", repeatLinearLoadExperiment, selected, functions, len(compiled.Code), bodyBytes, spills, reloads)
	if repeatLinearLoadExperiment && selected == 0 {
		t.Fatal("no backend pairs selected from PHP source candidates")
	}
	if !repeatLinearLoadExperiment && selected != 0 {
		t.Fatal("experimental rewrite selected with flag disabled")
	}
}
