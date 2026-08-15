//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/optimization"
)

// The binding inventory is checked against the amd64 Optimization Definitions
// during package initialization. Public sense is always "on = enabled".
var optimizationBindings = optimization.NewBindings("amd64",
	optimization.Bind("bounds-facts", &boundsFactsEnabled),
	optimization.Bind("call-effect-bounds", &callEffectBoundsEnabled),
	optimization.Bind("st-flags", &stFlagsEnabled),
	optimization.Bind("store8-flags", &store8FlagsEnabled),
	optimization.Bind("reg-merge", &regMergeEnabled),
	optimization.Bind("tee-sink", &teeLocalSinkEnabled),
	optimization.Bind("unary-sink", &unaryLocalSinkEnabled),
	optimization.Bind("branch-fold", &branchFoldEnabled),
	optimization.Bind("entry-arg-pins", &entryArgPinsEnabled),
	optimization.Bind("ext-fp-pins", &extendedFPPinsEnabled),
	optimization.Bind("call-next-use", &callNextUseEnabled),
	optimization.Bind("affine-lea", &affineLeaEnabled),
	optimization.Bind("tree-order", &treeOrderEnabled),
	optimization.Bind("assoc-tree", &associativeTreeEnabled),
	optimization.Bind("bmi2-rorx", &bmi2RorxEnabled),
	optimization.Bind("vex-float-mem", &vexFloatMemEnabled),
	optimization.Bind("multi-bounds-cert", &multiBoundsCertEnabled),
	optimization.Bind("addr-zext-elim", &memory32AddrZExtElimEnabled),
	optimization.Bind("value-facts", &valueFactsEnabled),
	optimization.Bind("immutable-table", &immutableLocalTableEnabled),
	optimization.Bind("immutable-table-type", &immutableTableTypeEnabled),
	optimization.Bind("inline-callfree", &inlineCallFreeHintsEnabled),
	optimization.Bind("store-forward", &linearStoreForwardEnabled),
	optimization.Bind("frame-elide", &smallFrameElideEnabled),
	optimization.Bind("compact-i32-frame", &compactI32FrameEnabled),
	optimization.Bind("local-slot-order", &localSlotOrderEnabled),
	optimization.Bind("tee-spill-elide", &teeSpillElideEnabled),
	optimization.Bind("commute-self-update", &commuteSelfUpdateEnabled),
	optimization.Bind("i64-mask32", &i64Mask32Enabled),
	optimization.Bind("accumulator-immediate", &accumulatorImmediateEnabled),
	optimization.Bind("v128-const-cache", &v128ConstCacheEnabled),
	optimization.Bind("v128-pins", &v128LocalPinsEnabled),
	optimization.Bind("v128-sink", &v128LocalSinkEnabled),
	optimization.Bind("reg-abi", &regABIEnabled),
	optimization.Bind("prepared-fp-entry", &preparedFPEntryEnabled),
	optimization.Bind("entry-init-elide", &entryInitElisionEnabled),
	optimization.Bind("gc-dead-new", &deadGCNewEnabled),
	optimization.Bind("inline", &inlineEnabled),
	optimization.Bind("inline-loop-callees", &inlineLoopCallees),
	optimization.Bind("loop-precheck", &loopPrecheckEnabled),
	optimization.BindInverted("stack-fence", &noStackFence),
	optimization.BindInverted("stack-reg", &noStackReg),
)

var (
	optBoundsFacts          = optimizationBindings.Option("bounds-facts")
	optCallEffectBounds     = optimizationBindings.Option("call-effect-bounds")
	optSTFlags              = optimizationBindings.Option("st-flags")
	optStore8Flags          = optimizationBindings.Option("store8-flags")
	optRegMerge             = optimizationBindings.Option("reg-merge")
	optTeeSink              = optimizationBindings.Option("tee-sink")
	optUnarySink            = optimizationBindings.Option("unary-sink")
	optBranchFold           = optimizationBindings.Option("branch-fold")
	optEntryArgPins         = optimizationBindings.Option("entry-arg-pins")
	optExtendedFPPins       = optimizationBindings.Option("ext-fp-pins")
	optCallNextUse          = optimizationBindings.Option("call-next-use")
	optAffineLEA            = optimizationBindings.Option("affine-lea")
	optTreeOrder            = optimizationBindings.Option("tree-order")
	optAssocTree            = optimizationBindings.Option("assoc-tree")
	optBMI2Rorx             = optimizationBindings.Option("bmi2-rorx")
	optVEXFloatMem          = optimizationBindings.Option("vex-float-mem")
	optMultiBoundsCert      = optimizationBindings.Option("multi-bounds-cert")
	optAddrZExtElim         = optimizationBindings.Option("addr-zext-elim")
	optValueFacts           = optimizationBindings.Option("value-facts")
	optImmutableTable       = optimizationBindings.Option("immutable-table")
	optImmutableTableType   = optimizationBindings.Option("immutable-table-type")
	optInlineCallFree       = optimizationBindings.Option("inline-callfree")
	optStoreForward         = optimizationBindings.Option("store-forward")
	optFrameElide           = optimizationBindings.Option("frame-elide")
	optCompactI32Frame      = optimizationBindings.Option("compact-i32-frame")
	optLocalSlotOrder       = optimizationBindings.Option("local-slot-order")
	optTeeSpillElide        = optimizationBindings.Option("tee-spill-elide")
	optCommuteSelfUpdate    = optimizationBindings.Option("commute-self-update")
	optI64Mask32            = optimizationBindings.Option("i64-mask32")
	optAccumulatorImmediate = optimizationBindings.Option("accumulator-immediate")
	optV128ConstCache       = optimizationBindings.Option("v128-const-cache")
	optV128Pins             = optimizationBindings.Option("v128-pins")
	optV128Sink             = optimizationBindings.Option("v128-sink")
	optRegABI               = optimizationBindings.Option("reg-abi")
	optPreparedFPEntry      = optimizationBindings.Option("prepared-fp-entry")
	optEntryInitElide       = optimizationBindings.Option("entry-init-elide")
	optGCDeadNew            = optimizationBindings.Option("gc-dead-new")
	optInline               = optimizationBindings.Option("inline")
	optInlineLoopCallees    = optimizationBindings.Option("inline-loop-callees")
	optLoopPrecheck         = optimizationBindings.Option("loop-precheck")
	optStackFence           = optimizationBindings.Option("stack-fence")
	optStackReg             = optimizationBindings.Option("stack-reg")
)

type KnobInfo = optimization.Info
type OptimizationSnapshot = optimization.Snapshot
type OptimizationObjective = shared.OptimizationObjective
type CodegenPolicy = shared.CodegenPolicy

const (
	OptimizeSpeed    = shared.OptimizeSpeed
	OptimizeBalanced = shared.OptimizeBalanced
	OptimizeSize     = shared.OptimizeSize
	OptimizeEmbedded = shared.OptimizeEmbedded
)

func OptKnobs() []KnobInfo { return optimizationBindings.Infos() }

func OptKnobSnapshot() ([]KnobInfo, OptimizationSnapshot) { return optimizationBindings.Snapshot() }

func CurrentOptKnobSnapshot() OptimizationSnapshot { return optimizationBindings.CurrentSnapshot() }

func SetOptKnob(name string, on bool) bool { return optimizationBindings.Set(name, on) }

func currentCodegenPolicy() CodegenPolicy {
	selection, err := optimizationBindings.Resolve(nil)
	if err != nil {
		panic(err)
	}
	return shared.DefaultCodegenPolicy(selection)
}
