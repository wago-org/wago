//go:build amd64 && !wago_gc_direct_trusted && !wago_gc_direct_trusted_store && !wago_gc_direct_trusted_arrays && !wago_gc_direct_trusted_alloc && !wago_gc_direct_trusted_refstruct && !(wago_gc_direct_trusted_loop || wago_gc_direct_weighted_sum || wago_gc_direct_weighted_sse2)

package amd64

const gcTrustedDirectReads = false
