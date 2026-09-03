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

	NodeScratchReserved  uint64
	NodeScratchPeak      uint64
	NodeScratchRetained  uint64
	NodeScratchDiscarded uint64
}
