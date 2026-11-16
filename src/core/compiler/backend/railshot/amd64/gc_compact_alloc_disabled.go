//go:build amd64 && !(wago_gc_compact_alloc || wago_gc_compact_refstore)

package amd64

const gcCompactAlloc = false
