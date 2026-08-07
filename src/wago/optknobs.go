package wago

import "github.com/wago-org/wago/src/core/compiler/optimization"

// Optimization metadata and selection defaults. Public sense is always
// On == optimization enabled.

// OptKnobInfo describes one compiler optimization knob.
type OptKnobInfo = optimization.Info

// OptKnobs returns every optimization knob and its current state, in a stable
// order suitable for building a CLI flag surface.
func OptKnobs() []OptKnobInfo { return railshotOptKnobs() }

// OptimizationInfosForArch returns the registered optimization surface for a
// target architecture. Unlike OptKnobs, On reports the built-in default because
// a cross-target backend is not active in this process.
func OptimizationInfosForArch(arch string) []OptKnobInfo {
	return optimization.InfosForArch(arch)
}

// OptimizationInfos returns every registered optimization once, including
// target-specific entries used by standalone cross-compilation.
func OptimizationInfos() []OptKnobInfo {
	return optimization.Infos()
}

// SetOptKnob changes the active backend's process default.
// Deprecated: use RuntimeConfig.WithOptimization for runtime-local selection.
func SetOptKnob(name string, on bool) bool { return railshotSetOptKnob(name, on) }
