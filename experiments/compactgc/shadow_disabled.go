//go:build !wago_gc_compact_shadow

package compactgc

const shadowDiagnostics = false

type shadowDiagnosticState struct{}
type shadowCycle struct{}

func (c *Collector) beginShadowCycle([]*Ref, bool) shadowCycle { return shadowCycle{} }
func (c *Collector) finishShadowCycle(shadowCycle, error)      {}
