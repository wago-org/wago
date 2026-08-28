package profile

import (
	"fmt"
	"sync/atomic"
)

// MaxRailshotFunctionCounters bounds the opt-in counter slab at 8 MiB. Modules
// above this limit remain executable but must use targeted or sampled profiling
// rather than allocating an unbounded entry counter table.
const MaxRailshotFunctionCounters = 1 << 20

// RailshotCounters is the bounded, concurrency-safe first tier of profile
// collection. Function indexes are the module's original Wasm function index
// space, including imports. A runtime-owned native slab can drain observations
// into this aggregation layer with RecordFunctionN; Snapshot is the sole
// conversion into the immutable backend-neutral profile contract.
type RailshotCounters struct {
	moduleHash [32]byte
	generation atomic.Uint64
	functions  []atomic.Uint64
}

func NewRailshotCounters(moduleHash [32]byte, functionCount uint32) (*RailshotCounters, error) {
	if functionCount > MaxRailshotFunctionCounters {
		return nil, fmt.Errorf("profile: %d function counters exceed limit %d", functionCount, MaxRailshotFunctionCounters)
	}
	return &RailshotCounters{moduleHash: moduleHash, functions: make([]atomic.Uint64, int(functionCount))}, nil
}

func (c *RailshotCounters) FunctionCount() uint32 {
	if c == nil {
		return 0
	}
	return uint32(len(c.functions))
}

// RecordFunction adds one execution using saturating arithmetic. An invalid
// index is ignored so stale or corrupt instrumentation cannot write outside the
// bounded slab.
func (c *RailshotCounters) RecordFunction(function uint32) {
	c.RecordFunctionN(function, 1)
}

// RecordFunctionN is used by tests, hardware-sample importers, and future
// batched counter drains. It never wraps a hot counter back to zero.
func (c *RailshotCounters) RecordFunctionN(function uint32, count uint64) {
	if c == nil || count == 0 || function >= uint32(len(c.functions)) {
		return
	}
	counter := &c.functions[function]
	for {
		before := counter.Load()
		after := before + count
		if after < before {
			after = ^uint64(0)
		}
		if before == after || counter.CompareAndSwap(before, after) {
			return
		}
	}
}

// Snapshot returns one immutable Railshot profile generation. When reset is
// true, each counter is atomically drained into the snapshot; otherwise counts
// are observed without perturbing collection.
func (c *RailshotCounters) Snapshot(phase Phase, reset bool) (Module, error) {
	if c == nil {
		return Module{}, fmt.Errorf("profile: nil Railshot counters")
	}
	if phase != PhaseStartup && phase != PhaseSteady && phase != PhaseRare {
		return Module{}, fmt.Errorf("profile: unknown phase %q", phase)
	}
	counts := make([]uint64, len(c.functions))
	for index := range c.functions {
		if reset {
			counts[index] = c.functions[index].Swap(0)
		} else {
			counts[index] = c.functions[index].Load()
		}
	}
	generation := c.nextGeneration()
	p := Module{
		Version: Version, ModuleHash: c.moduleHash, Generation: generation,
		Source: SourceRailshot, Phase: phase, FunctionCounts: counts,
	}
	if err := p.Validate(); err != nil {
		return Module{}, err
	}
	return p, nil
}

func (c *RailshotCounters) nextGeneration() uint64 {
	for {
		before := c.generation.Load()
		if before == ^uint64(0) {
			return before
		}
		if c.generation.CompareAndSwap(before, before+1) {
			return before + 1
		}
	}
}
