//go:build wago_profile

package profilecmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/profcapture"
)

func TestExperimentSummarizesAlternatingValidatedPairs(t *testing.T) {
	var captures []capture
	for i, costs := range [][2]int64{{100, 90}, {110, 92}, {120, 130}} {
		for side, cost := range costs {
			revision := "baseline"
			if side == 1 {
				revision = "candidate"
			}
			captures = append(captures, capture{manifest: profcapture.Manifest{
				Backend: "none", Phase: "execute", PhaseIsolation: "no-sampling",
				Revision: revision, WorkloadHash: "same-contract", Target: "linux/amd64",
				Iterations: 10, ActualNS: cost * 10,
				Phases: []profcapture.Phase{{Name: "execute", Start: int64(i*2 + side + 1)}},
			}})
		}
	}
	result, err := compareExperiment(captures)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pairs) != 3 || result.MedianBeforeNS != 110 || result.MedianAfterNS != 92 || result.MedianPairedDeltaNS != -10 || result.MedianPairedPercent != -10 || result.ImprovedPairs != 2 {
		t.Fatalf("incorrect paired distribution: %+v", result)
	}
	var buf bytes.Buffer
	if err := writeExperiment(&buf, result, true); err != nil {
		t.Fatal(err)
	}
	var decoded experimentResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil || len(decoded.Pairs) != 3 {
		t.Fatalf("invalid experiment JSON: %v %v", err, decoded)
	}
	captures[3].manifest.Phases[0].Start = 2
	if _, err := compareExperiment(captures); err == nil || !strings.Contains(err.Error(), "alternating") {
		t.Fatalf("accepted out-of-order run: %v", err)
	}
}

func TestExperimentRejectsChangedWorkload(t *testing.T) {
	var captures []capture
	for i := 0; i < 4; i++ {
		captures = append(captures, capture{manifest: profcapture.Manifest{
			Backend: "none", Phase: "execute", WorkloadHash: "same", Iterations: 1,
			ActualNS: 100, Revision: "v1",
		}})
	}
	captures[2].manifest.WorkloadHash = "different"
	captures[3].manifest.WorkloadHash = "different"
	if _, err := compareExperiment(captures); err == nil || !strings.Contains(err.Error(), "pair 2") {
		t.Fatalf("accepted mismatched work: %v", err)
	}
}

func TestExperimentRejectsChangedConfigurationWithinBuild(t *testing.T) {
	var captures []capture
	for i := 0; i < 4; i++ {
		captures = append(captures, capture{manifest: profcapture.Manifest{
			Backend: "none", Phase: "execute", WorkloadHash: "same", ModuleHash: "module",
			Iterations: 1, ActualNS: 100, Revision: "v1",
			Config: map[string]any{"bounds": "explicit"},
		}})
	}
	captures[2].manifest.Config["bounds"] = "signals-based"
	if _, err := compareExperiment(captures); err == nil || !strings.Contains(err.Error(), "effective configuration") {
		t.Fatalf("accepted changed baseline configuration: %v", err)
	}
}
