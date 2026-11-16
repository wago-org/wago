//go:build wago_gc_compact_eden && !(wago_gc_compact_cohort || wago_gc_compact_shadow || wago_gc_compact_age2_metrics || wago_gc_compact_age2_queue_reuse || wago_gc_compact8)

package compactgc

const edenCompatible = true
