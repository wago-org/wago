package shared

// CompileStage identifies a module-compilation stage in CompileResourceStats.
// Keep this list architecture-neutral so both Railshot targets expose the same
// resource ledger.
type CompileStage uint8

const (
	CompileStageHints CompileStage = iota
	CompileStageFunctions
	CompileStageFinalize
	CompileStageCount
)

// CompileResourceStats attributes retained compiler metadata, scratch and
// repeated work. It is populated only when the caller enables ModuleStats.
// Payload and Go runtime/GC metrics remain separate: callers can pair this
// ledger with runtime/metrics without conflating the two.
type CompileResourceStats struct {
	StageNanos [CompileStageCount]uint64

	HintHeaderBytes  uint64
	HintSidecarBytes uint64

	FunctionAttempts uint64
	RetryFunctions   uint64
	RetryInputBytes  uint64
	RetryNodeBytes   uint64
	RetryCodeBytes   uint64
	RetryNanos       uint64

	// NodeScratchReserved is the sum of workers' initial operand-node backing.
	// Peak is the sum of each worker's individual high-water (an envelope, not a
	// claim that every worker peaked simultaneously). Retained is the backing left
	// when compilation finishes; Discarded is cumulative backing released after a
	// smaller successor no longer used a worker's overflow chunks.
	NodeScratchReserved  uint64
	NodeScratchPeak      uint64
	NodeScratchRetained  uint64
	NodeScratchDiscarded uint64

	// ControlScratch fields use the same summed-worker envelope convention for
	// pointer-rich control-frame backing. Unlike node chunks, control backing is
	// currently retained until the module compile finishes.
	ControlScratchReserved  uint64
	ControlScratchPeak      uint64
	ControlScratchRetained  uint64
	ControlScratchDiscarded uint64
}

// WorkerScratchStats is the scalar handoff retained after one parallel compiler
// worker releases its scratch object. It deliberately contains no slice, map, or
// pointer fields, so final module assembly cannot keep worker scratch reachable.
type WorkerScratchStats struct {
	NodeReserved  uint64
	NodePeak      uint64
	NodeRetained  uint64
	NodeDiscarded uint64

	ControlReserved  uint64
	ControlPeak      uint64
	ControlRetained  uint64
	ControlDiscarded uint64
}

// AddWorkerScratch merges one worker's pointer-free scratch snapshot into the
// module resource ledger.
func (s *CompileResourceStats) AddWorkerScratch(w WorkerScratchStats) {
	s.NodeScratchReserved += w.NodeReserved
	s.NodeScratchPeak += w.NodePeak
	s.NodeScratchRetained += w.NodeRetained
	s.NodeScratchDiscarded += w.NodeDiscarded
	s.ControlScratchReserved += w.ControlReserved
	s.ControlScratchPeak += w.ControlPeak
	s.ControlScratchRetained += w.ControlRetained
	s.ControlScratchDiscarded += w.ControlDiscarded
}
