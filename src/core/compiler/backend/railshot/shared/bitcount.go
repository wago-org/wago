package shared

// Bit-count CPU features describe optional scalar instructions emitted by AMD64.
const (
	BitCountLZCNT uint8 = 1 << iota
	BitCountTZCNT
	BitCountPOPCNT
)
