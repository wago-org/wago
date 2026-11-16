//go:build wago_gc_compact_cohort && (wago_gc_compact_helpers || wago_gc_compact_reads || wago_gc_compact_alloc || wago_gc_compact_refstore || wago_gc_compact_shared_checks || wago_gc_compact_scalar_arrays || wago_gc_compact_array_alloc || wago_gc_compact_fill || wago_gc_compact_copy || wago_gc_compact_loop || wago_gc_compact_pair_loop || wago_gc_compact_range_loop || wago_gc_compact_range_stride || wago_gc_compact_range_sse2 || wago_gc_compact_weighted_sum || wago_gc_compact_refstruct)

package gc

import compactgc "github.com/wago-org/wago/experiments/compactgc"

// CompactCohortDiagnostics returns completed collection totals by value.
// Diagnostic builds only; no heap pointers, callbacks, or timing contract.
func (c *Collector) CompactCohortDiagnostics() (compactgc.CohortSnapshot, bool) {
	if c == nil || c.closed || c.direct == nil {
		return compactgc.CohortSnapshot{}, false
	}
	return c.direct.CohortDiagnostics(), true
}
