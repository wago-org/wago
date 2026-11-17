//go:build amd64 && wago_codegenstats

package amd64

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Diagnostic-only, exact full artifact. This does not replace semantic execution.
func TestCombinedActualGemmDiagnostic(t *testing.T) {
	path := os.Getenv("WAGO_TEST_GEMM_ARTIFACT")
	if path == "" {
		t.Skip("explicit exact GEMM artifact required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if digest != "8ba9ee8cafd9bf19b1547fe9127c861505daeecfe84c0516801776fddfcabd79" {
		t.Fatal("unexpected artifact", digest)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: shared.AMD64KnownFeatures, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	funcs := map[int]map[string]int{}
	total := map[string]int{}
	for i, s := range stats.Funcs {
		if s == nil {
			continue
		}
		for k, v := range s.Peephole {
			if strings.HasPrefix(k, "region-") || k == "bounded-vector-poll" {
				if funcs[i] == nil {
					funcs[i] = map[string]int{}
				}
				funcs[i][k] = v
				total[k] += v
			}
		}
	}
	record := struct {
		Artifact    string
		Bounded     bool
		Groups      int
		NativeSHA   string
		NativeBytes int
		Functions   map[int]map[string]int
	}{digest, boundedVectorPolls, boundedVectorPollGroups, fmt.Sprintf("%x", sha256.Sum256(cm.Code)), len(cm.Code), funcs}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(encoded))
	if path := os.Getenv("WAGO_TEST_GEMM_NATIVE_OUTPUT"); path != "" {
		if err := os.WriteFile(path, cm.Code, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if boundedVectorPolls && boundedAdjacentYMM {
		if total["region-loop-wide-adjacent"] != 1 || total["region-loop-wide-checked-tail"] != 1 {
			t.Fatal("actual GEMM YMM checked tail missing", total)
		}
	}
	if boundedVectorPolls {
		if total["region-emitted-interruptible-adjacent"] == 0 {
			t.Fatal("actual GEMM emitted no adjacent region", total)
		}
	} else if total["region-reject-interruptible-disabled"] == 0 || total["region-emitted-interruptible-adjacent"] != 0 {
		t.Fatal("disabled admission not observed", total)
	}
}
