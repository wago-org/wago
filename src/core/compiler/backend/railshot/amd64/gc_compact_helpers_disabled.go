//go:build amd64 && !(wago_gc_compact_helpers || wago_gc_compact_reads || wago_gc_compact_alloc)

package amd64

// gcCompactHelpers admits only the separate compact collector helper ABI.
const gcCompactHelpers = false
