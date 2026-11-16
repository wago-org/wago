//go:build amd64 && !(wago_gc_compact_refstore || wago_gc_compact_scalar_arrays)

package amd64

const gcCompactRefStore = false
