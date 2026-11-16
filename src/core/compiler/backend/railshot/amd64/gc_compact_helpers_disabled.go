//go:build amd64 && !(wago_gc_compact_helpers || wago_gc_compact_reads || wago_gc_compact_alloc || wago_gc_compact_refstore || wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || wago_gc_compact_fill))

package amd64

// gcCompactHelpers admits only the separate compact collector helper ABI.
const gcCompactHelpers = false
