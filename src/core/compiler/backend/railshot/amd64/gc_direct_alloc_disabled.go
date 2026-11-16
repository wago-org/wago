//go:build amd64 && !wago_gc_direct_alloc && !wago_gc_direct_refstore

package amd64

const gcDirectAlloc = false
