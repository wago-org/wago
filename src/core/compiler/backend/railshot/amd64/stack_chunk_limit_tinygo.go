//go:build amd64 && tinygo

package amd64

// Bound conservative GC object-head searches for interior operand pointers.
// This changes only geometric backing-array growth, not operand identities,
// lowering order, or the fixed total worker-retention budget.
const maxStackChunkCap = 256
