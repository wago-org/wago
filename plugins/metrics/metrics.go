// Package metrics provides counter and histogram host imports for wasm guests
// under the "wago_metrics" module.
package metrics

import (
	"sync"

	wago "github.com/wago-org/wago"
)

// CapWrite is the capability guarding the metrics imports.
const CapWrite = wago.CapMetricsWrite

// Sink receives metric updates. The default in-memory sink (see Ext) records
// them for inspection via the extension's Counter/Histogram accessors.
type Sink interface {
	CounterAdd(name string, delta int64)
	HistogramObserve(name string, value float64)
}

// Extension is the metrics extension. When constructed without WithSink it keeps
// metrics in memory, queryable through Counter and Histogram.
type Extension struct {
	sink Sink

	mu       sync.Mutex
	counters map[string]int64
	hists    map[string][]float64
}

// Option configures the metrics extension.
type Option func(*Extension)

// WithSink routes metric updates to a custom sink instead of the in-memory store.
func WithSink(s Sink) Option { return func(e *Extension) { e.sink = s } }

// Ext constructs the metrics extension.
func Ext(opts ...Option) *Extension {
	e := &Extension{counters: map[string]int64{}, hists: map[string][]float64{}}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Info identifies the extension.
func (e *Extension) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{
		ID:          "wago.metrics",
		Name:        "Metrics",
		Version:     "1.0.0",
		Description: "Counters and histograms for wasm guests.",
		Stability:   wago.Stable,
		Homepage:    "https://github.com/wago-org/wago",
		Repository:  "https://github.com/wago-org/wago",
		License:     "Apache-2.0",
		Authors:     []string{"The wago authors"},
		Tags:        []string{"metrics", "counters", "histograms", "observability"},
		Compat: wago.Compatibility{
			Engines:   map[string]string{"wago": ">=0.1.0", "tinygo": "*"},
			Platforms: []string{"linux/amd64"},
		},
	}
}

// Register wires the wago_metrics host imports.
func (e *Extension) Register(reg *wago.Registry) error {
	reg.Capability(CapWrite, wago.CapabilityDocs("record counters and histograms"))

	reg.ImportModule("wago_metrics").
		Func("counter_add", func(m wago.HostModule, p, res []uint64) {
			name, ok := readName(m, uint32(p[0]), uint32(p[1]))
			if !ok {
				res[0] = wago.I32(4)
				return
			}
			e.counterAdd(name, wago.AsI64(p[2]))
			res[0] = wago.I32(0)
		}).
		Params(wago.ValI32, wago.ValI32, wago.ValI64).Results(wago.ValI32).Capability(CapWrite).
		Docs("add to a counter: (name_ptr i32, name_len i32, delta i64) -> status i32")

	reg.ImportModule("wago_metrics").
		Func("histogram_observe", func(m wago.HostModule, p, res []uint64) {
			name, ok := readName(m, uint32(p[0]), uint32(p[1]))
			if !ok {
				res[0] = wago.I32(4)
				return
			}
			e.histObserve(name, wago.AsF64(p[2]))
			res[0] = wago.I32(0)
		}).
		Params(wago.ValI32, wago.ValI32, wago.ValF64).Results(wago.ValI32).Capability(CapWrite).
		Docs("observe a histogram value: (name_ptr i32, name_len i32, value f64) -> status i32")

	return nil
}

// Counter returns the current value of a counter recorded by the in-memory sink.
func (e *Extension) Counter(name string) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.counters[name]
}

// Histogram returns a copy of the observations recorded for a histogram by the
// in-memory sink.
func (e *Extension) Histogram(name string) []float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]float64(nil), e.hists[name]...)
}

func (e *Extension) counterAdd(name string, delta int64) {
	if e.sink != nil {
		e.sink.CounterAdd(name, delta)
		return
	}
	e.mu.Lock()
	e.counters[name] += delta
	e.mu.Unlock()
}

func (e *Extension) histObserve(name string, value float64) {
	if e.sink != nil {
		e.sink.HistogramObserve(name, value)
		return
	}
	e.mu.Lock()
	e.hists[name] = append(e.hists[name], value)
	e.mu.Unlock()
}

// readName reads a UTF-8 name from guest memory, returning false if the range is
// out of bounds.
func readName(m wago.HostModule, ptr, n uint32) (string, bool) {
	mem := m.Memory()
	if int64(ptr)+int64(n) > int64(len(mem)) {
		return "", false
	}
	return string(mem[ptr : ptr+n]), true
}
