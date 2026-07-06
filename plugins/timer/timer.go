// Package timer provides wall-clock and monotonic time host imports for wasm
// guests under the "wago_timer" module.
package timer

import (
	"time"

	wago "github.com/wago-org/wago"
)

// CapRead is the capability guarding the timer imports.
const CapRead = wago.CapTimerRead

// Clock is the time source the extension reads. Inject a fake with WithClock for
// deterministic tests.
type Clock interface {
	// UnixMilli returns the current wall-clock time in milliseconds since the
	// Unix epoch.
	UnixMilli() int64
	// MonotonicNanos returns a monotonic timer reading in nanoseconds. Only
	// differences between readings are meaningful.
	MonotonicNanos() int64
	// Sleep blocks for at least d.
	Sleep(d time.Duration)
}

type realClock struct{ start time.Time }

func (c realClock) UnixMilli() int64      { return time.Now().UnixMilli() }
func (c realClock) MonotonicNanos() int64 { return int64(time.Since(c.start)) }
func (c realClock) Sleep(d time.Duration) { time.Sleep(d) }

// Extension is the timer extension.
type Extension struct {
	clock Clock
}

// Option configures the timer extension.
type Option func(*Extension)

// WithClock overrides the time source (for tests or virtual clocks).
func WithClock(c Clock) Option { return func(e *Extension) { e.clock = c } }

// Ext constructs the timer extension.
func Ext(opts ...Option) *Extension {
	e := &Extension{clock: realClock{start: time.Now()}}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Info identifies the extension.
func (e *Extension) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{
		ID:          "wago.timer",
		Name:        "Timer",
		Version:     "1.0.0",
		Description: "Wall-clock and monotonic time for wasm guests.",
		Stability:   wago.Stable,
		Homepage:    "https://github.com/wago-org/wago",
		Repository:  "https://github.com/wago-org/wago",
		License:     "Apache-2.0",
		Authors:     []string{"The wago authors"},
		Keywords:    []string{"time", "clock", "monotonic"},
		Compat: wago.Compatibility{
			Engines:   map[string]string{"wago": ">=0.1.0", "tinygo": "*"},
			Platforms: []string{"linux/amd64"},
		},
	}
}

// Register wires the wago_timer host imports.
func (e *Extension) Register(reg *wago.Registry) error {
	reg.Capability(CapRead, wago.CapabilityDocs("read wall-clock and monotonic time"))

	reg.ImportModule("wago_timer").
		Func("now_unix_ms", func(_ wago.HostModule, _, res []uint64) {
			res[0] = wago.I64(e.clock.UnixMilli())
		}).
		Results(wago.ValI64).Capability(CapRead).
		Docs("current wall-clock time in milliseconds since the Unix epoch")

	reg.ImportModule("wago_timer").
		Func("now_monotonic_ns", func(_ wago.HostModule, _, res []uint64) {
			res[0] = wago.I64(e.clock.MonotonicNanos())
		}).
		Results(wago.ValI64).Capability(CapRead).
		Docs("monotonic timer reading in nanoseconds")

	reg.ImportModule("wago_timer").
		Func("sleep_ms", func(_ wago.HostModule, p, res []uint64) {
			ms := wago.AsI64(p[0])
			if ms > 0 {
				e.clock.Sleep(time.Duration(ms) * time.Millisecond)
			}
			res[0] = wago.I32(0)
		}).
		Params(wago.ValI64).Results(wago.ValI32).Capability(CapRead).
		Docs("block the calling guest for the given milliseconds")

	return nil
}
