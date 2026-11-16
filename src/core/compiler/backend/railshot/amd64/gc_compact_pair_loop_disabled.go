//go:build amd64 && !(wago_gc_compact_pair_loop || wago_gc_compact_range_loop)

package amd64

const gcCompactPairLoop = false
