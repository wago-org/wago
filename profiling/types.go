// Package profiling records application workloads in profiling-enabled builds.
// It uses Wago's phase/capture engine without requiring applications to adopt
// the built-in CLI's JSON-AS imports or invocation loop.
package profiling

import (
	"time"

	"github.com/wago-org/wago"
)

// Options selects a bounded local capture. Native sampling can be started by
// an external collector while Backend is perf-map; pprof captures Go CPU only.
// Out must name a new directory. A duration or iteration count is required.
type Options struct {
	Out            string
	Backend        string
	Phase          string
	Duration       time.Duration
	Iterations     uint64
	Warmup         uint64
	IncludeCode    bool
	SourceMaps     bool
	UnwindMaps     bool
	ReloadArtifact bool
	Timeline       bool
	MaxSpans       int
	Bounds         string
	// WagoRevision is the Wago source revision used by the embedding binary.
	// Executable VCS information names the application, so it is not inferred.
	WagoRevision string
}

// Harness names an application contract and one validated unit of work.
// Execute returns an error on invalid results; only successful calls count as
// completed work. Imports and other instance settings come from Instantiate.
// Contract must change whenever work semantics or validation change.
type Harness struct {
	ID          string
	Contract    string
	WorkUnit    string
	Wasm        []byte
	Config      *wago.RuntimeConfig
	Instantiate wago.InstantiateOptions
	Initialize  func(*wago.Instance) error
	Execute     func(*wago.Instance) error
}
