//go:build wago_profile

package profilecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/profcapture"
	"github.com/wago-org/wago/profile"
)

func TestReportsRejectPendingCollectors(t *testing.T) {
	for _, complete := range []bool{false, true} {
		dir := t.TempDir()
		data, err := json.Marshal(profcapture.Manifest{Version: 1, Complete: complete, CollectorPending: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadCapture(dir); err == nil || !strings.Contains(err.Error(), "pending") {
			t.Fatalf("accepted unfinished collector: %v", err)
		}
	}
}

func TestDiffRejectsDifferentStackSamplingCosts(t *testing.T) {
	base := capture{manifest: profcapture.Manifest{Backend: "perf", Phase: "execute", Iterations: 1, RequestedRate: 99}}
	for _, change := range []func(*profcapture.Manifest){
		func(m *profcapture.Manifest) { m.RawStackBytes = 8192 },
		func(m *profcapture.Manifest) { m.StackCollection = "perf-dwarf" },
		func(m *profcapture.Manifest) { m.RequestedRate = 499 },
	} {
		other := base
		change(&other.manifest)
		if err := diff(base, other, true); err == nil || !strings.Contains(err.Error(), "stack collection or sampling rates") {
			t.Fatal(err)
		}
	}
}

func comparisonFixture() (capture, capture) {
	a := capture{manifest: profcapture.Manifest{
		Backend: "perf", Phase: "execute", Iterations: 10, ActualNS: 2000,
		Event: "cpu-clock:u", PhaseIsolation: "collector-start-stop", RequestedRate: 99,
	}, report: profile.Report{
		Unit: "nanoseconds", Samples: 10, Weight: 1000, UnknownSamples: 1, UnknownWeight: 100,
		Rows: []profile.Row{
			{ModuleID: "module", ArtifactID: "baseline", Function: 0, Name: "hot", Samples: 7, Weight: 700},
			{ModuleID: "module", ArtifactID: "baseline", Function: -1, Kind: "runtime-helper", Samples: 2, Weight: 200},
		},
	}, events: []wago.CodeProfileEvent{{Image: &wago.CodeProfileImage{
		ModuleID: "module", ArtifactID: "baseline", Functions: []wago.CodeProfileFunction{{Index: 0, Name: "hot", NativeBytes: 80}},
	}}}}
	b := capture{manifest: a.manifest, report: profile.Report{
		Unit: "nanoseconds", Samples: 16, Weight: 1600, UnknownSamples: 2, UnknownWeight: 200,
		Rows: []profile.Row{
			{ModuleID: "module", ArtifactID: "candidate", Function: 0, Name: "hot", Samples: 10, Weight: 1000},
			{ModuleID: "module", ArtifactID: "candidate", Function: -1, Kind: "runtime-helper", Samples: 4, Weight: 400},
		},
	}, events: []wago.CodeProfileEvent{{Image: &wago.CodeProfileImage{
		ModuleID: "module", ArtifactID: "candidate", Functions: []wago.CodeProfileFunction{{Index: 0, Name: "hot", NativeBytes: 64}},
	}}}}
	b.manifest.Iterations, b.manifest.ActualNS = 20, 5000
	return a, b
}

func TestComparisonKeepsElapsedCPUHelpersAndUnknownSeparate(t *testing.T) {
	a, b := comparisonFixture()
	r, err := compareCaptures(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Elapsed != changedCost(200, 250) || r.CPU.Total != changedCost(100, 80) || r.CPU.Functions != changedCost(70, 50) || r.CPU.Helpers != changedCost(20, 20) || r.CPU.Unknown != changedCost(10, 10) {
		t.Fatalf("cost per equivalent work was lost: %+v, %+v", r, r.CPU)
	}
	if len(r.Functions) != 1 || r.Functions[0].Delta != -20 || r.Functions[0].BeforeCompiler.NativeBytes != 80 || r.Functions[0].AfterCompiler.NativeBytes != 64 {
		t.Fatalf("function/compiler join: %+v", r.Functions)
	}
	var out bytes.Buffer
	if err := writeComparison(&out, r, true); err != nil {
		t.Fatal(err)
	}
	var roundtrip comparison
	if err := json.Unmarshal(out.Bytes(), &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.Version != 1 || roundtrip.BeforeIterations != 10 || roundtrip.AfterIterations != 20 || roundtrip.CPU.Total != r.CPU.Total || roundtrip.Elapsed != r.Elapsed || len(roundtrip.Diagnostics) == 0 {
		t.Fatalf("incomplete JSON report: %s", out.String())
	}
	out.Reset()
	if err := writeComparison(&out, r, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Elapsed ns/iteration: 200.0 → 250.0", "Helpers", "Unknown/non-guest", "Samples: 10 → 16", "not proven zero execution cost"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, out.String())
		}
	}
}

func TestComparisonWithoutNativeSamplingRetainsWallTime(t *testing.T) {
	a, b := comparisonFixture()
	a.manifest.Backend, b.manifest.Backend = "none", "none"
	a.report, b.report = profile.Report{}, profile.Report{}
	r, err := compareCaptures(a, b)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeComparison(&out, r, true); err != nil {
		t.Fatal(err)
	}
	if r.CPU != nil || r.Elapsed != changedCost(200, 250) || len(r.Functions) != 0 || strings.Contains(out.String(), "sampled_cpu") || !strings.Contains(out.String(), "elapsed_ns_per_iteration") {
		t.Fatalf("invented CPU measurement or discarded elapsed time: %s", out.String())
	}
}

func TestComparisonRejectsIncompatibleMeasurement(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*capture)
	}{
		{"phase isolation", func(c *capture) { c.manifest.PhaseIsolation = "external-collector-uncontrolled" }},
		{"source collection", func(c *capture) { c.manifest.SourceMapsRequested = true }},
		{"unwind collection", func(c *capture) { c.manifest.UnwindMapsRequested = true }},
		{"native bytes", func(c *capture) { c.manifest.CodeIncluded = true }},
		{"collector version", func(c *capture) { c.manifest.CollectorVersion = "different" }},
		{"zero duration", func(c *capture) { c.manifest.ActualNS = 0 }},
		{"no completed work", func(c *capture) { c.manifest.Iterations = 0 }},
		{"no observations", func(c *capture) { c.report.Samples = 0 }},
		{"wrong units", func(c *capture) { c.report.Unit = "cycles" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, b := comparisonFixture()
			tt.change(&b)
			if _, err := compareCaptures(a, b); err == nil {
				t.Fatal("accepted incompatible measurement")
			}
		})
	}
	a, b := comparisonFixture()
	a.manifest.PhaseIsolation, b.manifest.PhaseIsolation = "external-collector-uncontrolled", "external-collector-uncontrolled"
	if _, err := compareCaptures(a, b); err == nil {
		t.Fatal("equal uncontrolled phases cannot be compared as isolated CPU")
	}
}

func TestComparisonDoesNotChooseArbitraryCompilerVariant(t *testing.T) {
	a, b := comparisonFixture()
	// An unobserved variant must not overwrite the observed variant's counters.
	a.events = append(a.events, wago.CodeProfileEvent{Image: &wago.CodeProfileImage{
		ModuleID: "module", ArtifactID: "other", Functions: []wago.CodeProfileFunction{{Index: 0, NativeBytes: 999}},
	}})
	r, err := compareCaptures(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Functions[0].BeforeCompiler.NativeBytes != 80 {
		t.Fatal("joined unobserved artifact")
	}
	a.report.Rows = append(a.report.Rows, profile.Row{ModuleID: "module", ArtifactID: "other", Function: 0, Samples: 1, Weight: 100})
	a.report.Samples++
	a.report.Weight += 100
	r, err = compareCaptures(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Functions[0].Before != 80 || r.Functions[0].BeforeCompiler != nil || r.Functions[0].AfterCompiler == nil || !strings.Contains(strings.Join(r.Diagnostics, "\n"), "multiple sampled artifacts") {
		t.Fatalf("ambiguous static join: %+v", r)
	}
}

type failedComparisonWriter struct{}

func (failedComparisonWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestComparisonPropagatesOutputFailures(t *testing.T) {
	a, b := comparisonFixture()
	r, err := compareCaptures(a, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, asJSON := range []bool{false, true} {
		if err := writeComparison(failedComparisonWriter{}, r, asJSON); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("lost writer failure: %v", err)
		}
	}
}
