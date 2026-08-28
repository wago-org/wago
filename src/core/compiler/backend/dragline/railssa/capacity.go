package railssa

// PipelineCapacityBreakdown attributes reusable RailSSA backing storage to its
// owning stage. It intentionally reports capacities rather than current
// lengths because retained headroom remains live during native finalization.
type PipelineCapacityBreakdown struct {
	CFG            uint64 `json:"cfg"`
	LocalSSA       uint64 `json:"local_ssa"`
	ValueFlow      uint64 `json:"value_flow"`
	Semantic       uint64 `json:"semantic"`
	Metadata       uint64 `json:"metadata"`
	Simplify       uint64 `json:"simplify"`
	Pressure       uint64 `json:"pressure"`
	Specialization uint64 `json:"specialization"`
	Emission       uint64 `json:"emission"`
}

func (b PipelineCapacityBreakdown) Total() uint64 {
	return b.CFG + b.LocalSSA + b.ValueFlow + b.Semantic + b.Metadata + b.Simplify + b.Pressure + b.Specialization + b.Emission
}

// PipelineCapacityBytes reports all reusable backing storage owned by the
// native RailSSA planning products. The aggregate lives in this package so
// private verifier scratch cannot silently disappear from compiler metrics.
func PipelineCapacityBytes(cfg *CFG, locals *LocalSSA, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, simplified *SimplifyResult, pressure *PressurePlan, specialize *SpecializationPlan, emission *EmissionPlan) uint64 {
	return MeasurePipelineCapacity(cfg, locals, flow, semantic, metadata, simplified, pressure, specialize, emission).Total()
}

func MeasurePipelineCapacity(cfg *CFG, locals *LocalSSA, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, simplified *SimplifyResult, pressure *PressurePlan, specialize *SpecializationPlan, emission *EmissionPlan) (bytes PipelineCapacityBreakdown) {
	if cfg != nil {
		bytes.CFG = capacityBytes(cfg.Blocks) + capacityBytes(cfg.Edges) + capacityBytes(cfg.EdgeStacks) + capacityBytes(cfg.Refinements) + capacityBytes(cfg.Preds) + capacityBytes(cfg.Succs) + capacityBytes(cfg.leaders) + capacityBytes(cfg.regionAtStart) + capacityBytes(cfg.regionAtElse) + capacityBytes(cfg.regionAtEnd) + capacityBytes(cfg.raw) + capacityBytes(cfg.active) + capacityBytes(cfg.starts) + capacityBytes(cfg.instrBlock) + capacityBytes(cfg.planned)
	}
	if locals != nil {
		bytes.LocalSSA = capacityBytes(locals.Definitions) + capacityBytes(locals.Params) + capacityBytes(locals.EdgeArgs) + capacityBytes(locals.EntryValues) + capacityBytes(locals.ExitValues) + capacityBytes(locals.InstructionValues) + capacityBytes(locals.Reachable) + capacityBytes(locals.LiveIn) + capacityBytes(locals.work) + capacityBytes(locals.ready) + capacityBytes(locals.before) + capacityBytes(locals.liveScratch)
	}
	if flow != nil {
		bytes.ValueFlow = capacityBytes(flow.Values) + capacityBytes(flow.Params) + capacityBytes(flow.EdgeArgs) + capacityBytes(flow.EntryStacks) + capacityBytes(flow.ExitStacks) + capacityBytes(flow.EntryDepths) + capacityBytes(flow.ExitDepths) + capacityBytes(flow.InstructionValues) + capacityBytes(flow.LocalDefinitionValues) + capacityBytes(flow.Reachable) + capacityBytes(flow.phi) + capacityBytes(flow.ready) + capacityBytes(flow.before) + capacityBytes(flow.merge) + capacityBytes(flow.stack)
	}
	if semantic != nil {
		bytes.Semantic = capacityBytes(semantic.Insts) + capacityBytes(semantic.Args) + capacityBytes(semantic.Blocks) + capacityBytes(semantic.InstructionMap) + capacityBytes(semantic.stack)
	}
	if metadata != nil {
		bytes.Metadata = capacityBytes(metadata.Instructions)
	}
	if simplified != nil {
		bytes.Simplify = capacityBytes(simplified.Aliases) + capacityBytes(simplified.Facts) + capacityBytes(simplified.factIndex) + capacityBytes(simplified.Reachable) + capacityBytes(simplified.LiveInsts) + capacityBytes(simplified.Branches) + capacityBytes(simplified.Remaining) + capacityBytes(simplified.UseOffsets) + capacityBytes(simplified.Uses) + capacityBytes(simplified.Bounds) + capacityBytes(simplified.liveValues) + capacityBytes(simplified.instructionBlock) + capacityBytes(simplified.liveWork) + capacityBytes(simplified.gvnKeys) + capacityBytes(simplified.gvnValues)
	}
	if pressure != nil {
		bytes.Pressure = capacityBytes(pressure.Blocks) + capacityBytes(pressure.Sinks) + capacityBytes(pressure.Remats) + capacityBytes(pressure.Inductions) + capacityBytes(pressure.LICM) + capacityBytes(pressure.ColdUses) + capacityBytes(pressure.definition) + capacityBytes(pressure.lastUse) + capacityBytes(pressure.useCount) + capacityBytes(pressure.directUseCount) + capacityBytes(pressure.directUseBlock) + capacityBytes(pressure.directUseInstruction) + capacityBytes(pressure.valueBlock) + capacityBytes(pressure.gprDelta) + capacityBytes(pressure.fprDelta) + capacityBytes(pressure.positionBlock) + capacityBytes(pressure.rematerializable) + capacityBytes(pressure.maxUseWeight)
	}
	if specialize != nil {
		bytes.Specialization = capacityBytes(specialize.Entries)
	}
	if emission != nil {
		bytes.Emission = capacityBytes(emission.boundsChecksElided)
	}
	return bytes
}
