//go:build amd64 && !(wago_gc_direct_fill || wago_gc_direct_fill_sse2)

package amd64

const gcDirectFill = false
