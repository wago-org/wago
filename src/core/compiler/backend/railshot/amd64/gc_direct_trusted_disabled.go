//go:build amd64 && !wago_gc_direct_trusted && !wago_gc_direct_trusted_store && !wago_gc_direct_trusted_arrays && !wago_gc_direct_trusted_alloc && !wago_gc_direct_trusted_refstruct

package amd64

const gcTrustedDirectReads = false
