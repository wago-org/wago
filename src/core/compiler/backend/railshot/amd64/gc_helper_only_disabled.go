//go:build amd64 && !(wago_gc_compact_helpers || (wago_gc_compact_reads || (wago_gc_compact_alloc || ((wago_gc_compact_refstore || wago_gc_compact_shared_checks) || (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || (wago_gc_compact_loop || wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || (wago_gc_compact_range_sse2 || (wago_gc_compact_weighted_sum || wago_gc_compact_weighted_sse2))) || wago_gc_compact_range_stride)))))))) || wago_gc_helper_only || wago_gc_direct_reads || wago_gc_direct_alloc || wago_gc_direct_refstore || wago_gc_direct_trusted || wago_gc_direct_scalar_arrays || wago_gc_direct_trusted_store || wago_gc_direct_trusted_arrays || ((wago_gc_direct_trusted_loop || wago_gc_direct_pair_loop || wago_gc_direct_range_loop) || wago_gc_direct_weighted_sum || wago_gc_direct_weighted_sse2) || wago_gc_direct_array_alloc || wago_gc_direct_trusted_alloc || wago_gc_direct_trusted_refstruct || wago_gc_direct_fill || wago_gc_direct_fill_sse2 || wago_gc_direct_copy || wago_gc_compact_refstruct)

package amd64

// gcHelperOnly is a correctness bridge, not a performance configuration.
const gcHelperOnly = false
