package shared

// GCBarrierState records the lowering selected for a reference store. Only
// NoBarrier authorizes omission; the remaining values distinguish current and
// future remembered-set paths in compiler telemetry.
type GCBarrierState uint8

const (
	GCBarrierNoBarrier GCBarrierState = iota
	GCBarrierYoungParent
	GCBarrierKnownOldChild
	GCBarrierExistingCard
	GCBarrierCardMark
	GCBarrierSlowBarrier
)

func (s GCBarrierState) NeedsBarrier() bool { return s != GCBarrierNoBarrier }
