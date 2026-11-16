//go:build amd64 && !wago_gc_direct_reads && !wago_gc_direct_alloc && !wago_gc_direct_refstore && !wago_gc_direct_trusted && !wago_gc_direct_scalar_arrays && !wago_gc_direct_trusted_store

package amd64

const gcDirectReads = false
