//go:build amd64 && (wago_gc_helper_only || wago_gc_direct_reads)

package amd64

// gcHelperOnly is a correctness bridge, not a performance configuration.
const gcHelperOnly = true
