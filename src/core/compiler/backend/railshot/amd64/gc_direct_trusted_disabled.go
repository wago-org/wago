//go:build amd64 && !wago_gc_direct_trusted && !wago_gc_direct_trusted_store && !wago_gc_direct_trusted_arrays

package amd64

const gcTrustedDirectReads = false
