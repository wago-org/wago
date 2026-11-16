//go:build amd64 && !(wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop))

package amd64

const gcCompactFill = false
