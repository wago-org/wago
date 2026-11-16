//go:build amd64 && !(wago_gc_compact_alloc || wago_gc_compact_refstore || wago_gc_compact_scalar_arrays || wago_gc_compact_array_alloc)

package amd64

const gcCompactAlloc = false
