//go:build amd64 && !wago_gc_direct_trusted_arrays && !wago_gc_direct_trusted_alloc && !wago_gc_direct_trusted_refstruct && !wago_gc_direct_trusted_loop

package amd64

const gcTrustedDirectArrays = false
