package wago

import "github.com/wago-org/wago/src/core/compiler/optimization"

// Optimization-knob API, re-exported from the active railshot backend
// (railshot_{amd64,arm64}.go). Knobs default from WAGO_* env vars at init; this
// API lets an embedder or the CLI override them programmatically before
// compiling. Public sense: On == optimization enabled.

// OptKnobInfo describes one compiler optimization knob.
type OptKnobInfo = railshotKnobInfo

// OptKnobs returns every optimization knob and its current state, in a stable
// order suitable for building a CLI flag surface.
func OptKnobs() []OptKnobInfo { return railshotOptKnobs() }

// OptimizationInfosForArch returns the registered optimization surface for a
// target architecture. Unlike OptKnobs, On reports the built-in default because
// a cross-target backend is not active in this process.
func OptimizationInfosForArch(arch string) []OptKnobInfo {
	definitions := optimization.ForArch(arch)
	result := make([]OptKnobInfo, len(definitions))
	for index, definition := range definitions {
		result[index] = OptKnobInfo{
			Name: definition.Name, Label: definition.Label, Desc: definition.Description,
			On: definition.Default, Default: definition.Default, Experimental: definition.Experimental,
		}
	}
	return result
}

// OptimizationInfos returns every registered optimization once, including
// target-specific entries used by standalone cross-compilation.
func OptimizationInfos() []OptKnobInfo {
	definitions := optimization.All()
	result := make([]OptKnobInfo, len(definitions))
	for index, definition := range definitions {
		result[index] = OptKnobInfo{
			Name: definition.Name, Label: definition.Label, Desc: definition.Description,
			On: definition.Default, Default: definition.Default, Experimental: definition.Experimental,
		}
	}
	return result
}

// SetOptKnob forces the named knob on or off. Returns false if the name is not a
// known knob. Call before compiling a module for the setting to take effect.
func SetOptKnob(name string, on bool) bool { return railshotSetOptKnob(name, on) }
