package profcapture

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/wago-org/wago"
)

// Harness adapts an embedding application's real configuration, imports, and
// validated work operation to the same phase/capture engine as CLI presets.
// Execute must return an error if its result is invalid. Hooks run in-process;
// external perf/samply supervision cannot serialize them into a child process.
type Harness struct {
	ID                 string
	Contract           string
	WorkUnit           string
	Wasm               []byte
	RuntimeConfig      *wago.RuntimeConfig
	InstantiateOptions wago.InstantiateOptions
	Initialize         func(*wago.Instance) error
	Execute            func(*wago.Instance) error
}

func RunWithHarness(o Options, h Harness) error {
	if h.ID == "" || h.Contract == "" || h.WorkUnit == "" || len(h.Wasm) == 0 || h.Execute == nil {
		return fmt.Errorf("application capture needs an ID, contract, work unit, Wasm module, and validating Execute callback")
	}
	if o.Backend != "none" && o.Backend != "pprof" && o.Backend != "perf-map" {
		return fmt.Errorf("application capture supports none, pprof, or externally collected perf-map backends")
	}
	o.Mode = "application"
	sum := sha256.Sum256(h.Wasm)
	w := Workload{ID: h.ID, Contract: h.Contract + ":" + h.WorkUnit, Hash: hex.EncodeToString(sum[:])}
	return run(o, w, h.Wasm, &h)
}
