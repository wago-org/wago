package plugin

import "github.com/wago-org/wago"

// Report is built only from immutable provider metadata and the side-effect-free
// plan. It never constructs, validates config with, registers, or starts code.
type Report struct {
	Definition wago.PluginDefinition `json:"definition"`
	Plan       *wago.PluginPlanEntry `json:"plan,omitempty"`
}

func BuildReport(definition wago.PluginDefinition, plan *wago.PluginPlanEntry) Report {
	return Report{Definition: definition, Plan: plan}
}
