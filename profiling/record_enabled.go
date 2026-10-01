//go:build wago_profile

package profiling

import (
	"time"

	"github.com/wago-org/wago/internal/profcapture"
)

// Record profiles a real embedding workload using the same validated manifest,
// phase timing, metadata journal, and local bundle format as wagoprof. The
// callback runs in this process; supported backends are none, pprof, and
// perf-map with an externally started native collector.
func Record(o Options, h Harness) error {
	if o.Backend == "" {
		o.Backend = "none"
	}
	if o.Phase == "" {
		o.Phase = "execute"
	}
	if o.Bounds == "" {
		o.Bounds = "explicit"
	}
	if o.Iterations == 0 && o.Duration == 0 {
		o.Duration = 15 * time.Second
	}
	return profcapture.RunWithHarness(profcapture.Options{
		Out: o.Out, Backend: o.Backend, Phase: o.Phase, Mode: "application",
		Duration: o.Duration, Iterations: o.Iterations, Warmup: o.Warmup,
		IncludeCode: o.IncludeCode, SourceMaps: o.SourceMaps, UnwindMaps: o.UnwindMaps,
		ReloadArtifact: o.ReloadArtifact, Timeline: o.Timeline, MaxSpans: o.MaxSpans,
		Bounds: o.Bounds, Rate: 99, WagoRevision: o.WagoRevision,
	}, profcapture.Harness{
		ID: h.ID, Contract: h.Contract, WorkUnit: h.WorkUnit, Wasm: h.Wasm,
		RuntimeConfig: h.Config, InstantiateOptions: h.Instantiate,
		Initialize: h.Initialize, Execute: h.Execute,
	})
}
