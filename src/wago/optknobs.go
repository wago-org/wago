package wago

// Optimization-knob API, re-exported from the active railshot backend
// (railshot_{amd64,arm64}.go). Knobs default from WAGO_* env vars at init; this
// API lets an embedder or the CLI override them programmatically before
// compiling. Public sense: On == optimization enabled.

// OptKnobInfo describes one compiler optimization knob.
type OptKnobInfo = railshotKnobInfo

// OptKnobs returns every optimization knob and its current state, in a stable
// order suitable for building a CLI flag surface.
func OptKnobs() []OptKnobInfo { return railshotOptKnobs() }

// SetOptKnob forces the named knob on or off. Returns false if the name is not a
// known knob. Call before compiling a module for the setting to take effect.
func SetOptKnob(name string, on bool) bool { return railshotSetOptKnob(name, on) }
