//go:build amd64 && !(wago_gc_compact_array_alloc || wago_gc_compact_fill)

package amd64

const gcCompactArrayAlloc = false
