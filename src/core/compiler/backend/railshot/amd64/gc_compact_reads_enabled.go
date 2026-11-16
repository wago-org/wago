//go:build amd64 && (wago_gc_compact_reads || wago_gc_compact_alloc || wago_gc_compact_refstore || wago_gc_compact_scalar_arrays)

package amd64

const gcCompactReads = true
