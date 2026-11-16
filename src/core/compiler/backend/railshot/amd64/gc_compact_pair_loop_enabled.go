//go:build amd64 && (wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || (wago_gc_compact_range_sse2 || (wago_gc_compact_weighted_sum || wago_gc_compact_weighted_sse2))) || wago_gc_compact_range_stride)

package amd64

const gcCompactPairLoop = true
