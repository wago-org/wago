//go:build amd64 && !wago_gc_direct_array_alloc && !wago_gc_direct_trusted_alloc

package amd64

const gcDirectArrayAlloc = false
