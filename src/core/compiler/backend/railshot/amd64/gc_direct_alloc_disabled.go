//go:build amd64 && !wago_gc_direct_alloc && !wago_gc_direct_refstore && !wago_gc_direct_trusted && !wago_gc_direct_scalar_arrays && !wago_gc_direct_trusted_store && !wago_gc_direct_trusted_arrays && !wago_gc_direct_array_alloc && !wago_gc_direct_trusted_alloc && !wago_gc_direct_trusted_refstruct && !(wago_gc_direct_fill || wago_gc_direct_fill_sse2) && !(wago_gc_direct_trusted_loop || wago_gc_direct_weighted_sum || wago_gc_direct_weighted_sse2) && !wago_gc_direct_copy

package amd64

const gcDirectAlloc = false
