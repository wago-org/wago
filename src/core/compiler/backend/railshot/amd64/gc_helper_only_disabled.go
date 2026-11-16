//go:build amd64 && !wago_gc_helper_only

package amd64

// gcHelperOnly is a correctness bridge, not a performance configuration.
const gcHelperOnly = false
