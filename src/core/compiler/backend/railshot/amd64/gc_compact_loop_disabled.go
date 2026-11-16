//go:build amd64 && !(wago_gc_compact_loop || wago_gc_compact_pair_loop || wago_gc_compact_range_loop || wago_gc_compact_range_stride)

package amd64

const gcCompactLoop = false
