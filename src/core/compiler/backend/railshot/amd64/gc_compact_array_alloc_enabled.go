//go:build amd64 && (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop || wago_gc_compact_pair_loop || wago_gc_compact_range_loop || wago_gc_compact_range_stride)))

package amd64

const gcCompactArrayAlloc = true
