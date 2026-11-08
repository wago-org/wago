package shared

// LoopStore records one source-ordered effect in a bounded loop candidate.
// Address identifies a modular dynamic address expression; Offset is the
// separately widened memory immediate. Combining them before the dynamic wrap
// would change Wasm addressing. Value and Address are owned expression indices.
type LoopStore[Node ~uint8 | ~int] struct {
	Offset         uint64
	PC             uint32
	Address, Value Node
}
