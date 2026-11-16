//go:build !wago_gc_compact_cohort

package compactgc

const cohortDiagnostics = false

type cohortDiagnosticState struct{}
type cohortCycle struct{}

func (c *Collector) beginCohortCycle(bool) cohortCycle    { return cohortCycle{} }
func (c *Collector) captureCohortLive(*cohortCycle, int)  {}
func (c *Collector) finishCohortCycle(cohortCycle, error) {}
func (c *Collector) recordOversized(uint32)               {}
