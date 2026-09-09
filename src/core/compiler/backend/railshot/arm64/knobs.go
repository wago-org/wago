//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/optimization"
)

// The binding inventory is checked against the arm64 Optimization Definitions
// during package initialization. Public sense is always "on = enabled".
var optimizationBindings = optimization.NewBindings("arm64",
	optimization.Bind("bounds-facts", &boundsFactsEnabled),
	optimization.Bind("simd-superopt", &simdSuperoptEnabled),
	optimization.Bind("interval-region-pins", &intervalRegionPinsEnabled),
	optimization.Bind("multi-bounds-cert", &multiBoundsCertEnabled),
	optimization.Bind("leaf-scratch-memsize", &leafScratchMemSizeEnabled),
	optimization.Bind("loop-trap-cell", &loopTrapCellEnabled),
	optimization.Bind("prepared-direct-entry", &preparedDirectEntryEnabled),
	optimization.Bind("prepared-light-entry", &preparedLightEntryEnabled),
	optimization.Bind("prepared-bounded-entry", &preparedBoundedEntryEnabled),
	optimization.Bind("loop-int-const", &loopIntConstEnabled),
	optimization.Bind("indexed-base-reuse", &indexedBaseReuseEnabled),
	optimization.Bind("convert-read", &convertReadEnabled),
	optimization.Bind("cold-call-local-pins", &coldCallLocalPinsEnabled),
	optimization.Bind("magic-div", &magicDivEnabled),
	optimization.Bind("shared-trap-body", &sharedTrapBodyEnabled),
	optimization.Bind("shared-adapters", &sharedAdaptersEnabled),
	optimization.Bind("st-flags", &stFlagsEnabled),
	optimization.Bind("reg-merge", &regMergeEnabled),
	optimization.Bind("tee-sink", &teeLocalSinkEnabled),
	optimization.Bind("unary-sink", &unaryLocalSinkEnabled),
	optimization.Bind("three-op-sink", &threeOperandSinkEnabled),
	optimization.Bind("olddest-rhs-sink", &oldDestRHSSinkEnabled),
	optimization.Bind("branch-fold", &branchFoldEnabled),
	optimization.Bind("store-load-fwd", &storeLoadFwdEnabled),
	optimization.Bind("memcopy-tail4", &memcopyTail4Enabled),
	optimization.Bind("memcopy-qpairs", &memcopyQPairsEnabled),
	optimization.Bind("uxtw-add", &uxtwAddEnabled),
	optimization.Bind("shifted-register-alu", &shiftedRegisterALUEnabled),
	optimization.Bind("fp-immediate-const", &fpImmediateConstEnabled),
	optimization.Bind("value-facts", &valueFactsEnabled),
	optimization.Bind("load-pair", &loadPairEnabled),
	optimization.Bind("merge-next-use", &mergeNextUseEnabled),
	optimization.Bind("weighted-scalar-merge", &weightedScalarMergeEnabled),
	optimization.Bind("entry-param-pairs", &entryParamPairsEnabled),
	optimization.Bind("entry-zero-pairs", &entryZeroPairsEnabled),
	optimization.Bind("entry-arg-pins", &entryArgPinsEnabled),
	optimization.Bind("x8-pin", &callFreeX8PinEnabled),
	optimization.Bind("ext-fp-pins", &extendedFPPinsEnabled),
	optimization.Bind("leaf-scratch-pins", &leafScratchPinsEnabled),
	optimization.Bind("immutable-table", &immutableLocalTableEnabled),
	optimization.Bind("immutable-table-type", &immutableTableTypeEnabled),
	optimization.Bind("inline-callfree", &inlineCallFreeHintsEnabled),
	optimization.Bind("store-forward", &linearStoreForwardEnabled),
	optimization.Bind("frame-elide-reghomed", &frameElideRegHomed),
	optimization.Bind("small-frame", &smallFrameAdjustEnabled),
	optimization.Bind("zero-branch", &zeroBranchEnabled),
	optimization.Bind("mul-add-fuse", &mulAddFuseEnabled),
	optimization.Bind("entry-init-elision", &entryInitElisionEnabled),
	optimization.Bind("v128-direct-results", &v128DirectResultEnabled),
	optimization.Bind("v128-pins", &v128LocalPinsEnabled),
	optimization.Bind("reg-abi", &regABIEnabled),
	optimization.Bind("inline", &inlineEnabled),
	optimization.BindInverted("stack-fence", &noStackFence),
	optimization.BindInverted("stack-reg", &noStackReg),
)

var (
	optBoundsFacts          = optimizationBindings.Option("bounds-facts")
	optSIMDSuperopt         = optimizationBindings.Option("simd-superopt")
	optIntervalRegionPins   = optimizationBindings.Option("interval-region-pins")
	optMultiBoundsCert      = optimizationBindings.Option("multi-bounds-cert")
	optLeafScratchMemSize   = optimizationBindings.Option("leaf-scratch-memsize")
	optLoopTrapCell         = optimizationBindings.Option("loop-trap-cell")
	optPreparedDirectEntry  = optimizationBindings.Option("prepared-direct-entry")
	optPreparedLightEntry   = optimizationBindings.Option("prepared-light-entry")
	optPreparedBoundedEntry = optimizationBindings.Option("prepared-bounded-entry")
	optLoopIntConst         = optimizationBindings.Option("loop-int-const")
	optIndexedBaseReuse     = optimizationBindings.Option("indexed-base-reuse")
	optConvertRead          = optimizationBindings.Option("convert-read")
	optColdCallLocalPins    = optimizationBindings.Option("cold-call-local-pins")
	optMagicDiv             = optimizationBindings.Option("magic-div")
	optSharedTrapBody       = optimizationBindings.Option("shared-trap-body")
	optSharedAdapters       = optimizationBindings.Option("shared-adapters")
	optSTFlags              = optimizationBindings.Option("st-flags")
	optRegMerge             = optimizationBindings.Option("reg-merge")
	optTeeSink              = optimizationBindings.Option("tee-sink")
	optUnarySink            = optimizationBindings.Option("unary-sink")
	optThreeOpSink          = optimizationBindings.Option("three-op-sink")
	optOldDestRHSSink       = optimizationBindings.Option("olddest-rhs-sink")
	optBranchFold           = optimizationBindings.Option("branch-fold")
	optStoreLoadFwd         = optimizationBindings.Option("store-load-fwd")
	optMemcopyTail4         = optimizationBindings.Option("memcopy-tail4")
	optMemcopyQPairs        = optimizationBindings.Option("memcopy-qpairs")
	optUXTWAdd              = optimizationBindings.Option("uxtw-add")
	optShiftedRegisterALU   = optimizationBindings.Option("shifted-register-alu")
	optFPImmediateConst     = optimizationBindings.Option("fp-immediate-const")
	optValueFacts           = optimizationBindings.Option("value-facts")
	optLoadPair             = optimizationBindings.Option("load-pair")
	optMergeNextUse         = optimizationBindings.Option("merge-next-use")
	optWeightedScalarMerge  = optimizationBindings.Option("weighted-scalar-merge")
	optEntryParamPairs      = optimizationBindings.Option("entry-param-pairs")
	optEntryZeroPairs       = optimizationBindings.Option("entry-zero-pairs")
	optEntryArgPins         = optimizationBindings.Option("entry-arg-pins")
	optX8Pin                = optimizationBindings.Option("x8-pin")
	optExtendedFPPins       = optimizationBindings.Option("ext-fp-pins")
	optLeafScratchPins      = optimizationBindings.Option("leaf-scratch-pins")
	optImmutableTable       = optimizationBindings.Option("immutable-table")
	optImmutableTableType   = optimizationBindings.Option("immutable-table-type")
	optInlineCallFree       = optimizationBindings.Option("inline-callfree")
	optStoreForward         = optimizationBindings.Option("store-forward")
	optFrameElideRegHomed   = optimizationBindings.Option("frame-elide-reghomed")
	optSmallFrame           = optimizationBindings.Option("small-frame")
	optZeroBranch           = optimizationBindings.Option("zero-branch")
	optMulAddFuse           = optimizationBindings.Option("mul-add-fuse")
	optEntryInitElision     = optimizationBindings.Option("entry-init-elision")
	optV128DirectResults    = optimizationBindings.Option("v128-direct-results")
	optV128Pins             = optimizationBindings.Option("v128-pins")
	optRegABI               = optimizationBindings.Option("reg-abi")
	optInline               = optimizationBindings.Option("inline")
	optStackFence           = optimizationBindings.Option("stack-fence")
	optStackReg             = optimizationBindings.Option("stack-reg")
)

type KnobInfo = optimization.Info
type OptimizationSnapshot = optimization.Snapshot
type CodegenPolicy = shared.CodegenPolicy

func OptKnobs() []KnobInfo { return optimizationBindings.Infos() }

func OptKnobSnapshot() ([]KnobInfo, OptimizationSnapshot) { return optimizationBindings.Snapshot() }

func CurrentOptKnobSnapshot() OptimizationSnapshot { return optimizationBindings.CurrentSnapshot() }

func SetOptKnob(name string, on bool) bool { return optimizationBindings.Set(name, on) }

func currentCodegenPolicy() CodegenPolicy {
	selection, err := optimizationBindings.ResolveSnapshot(nil, OptimizationSnapshot{}, nil)
	if err != nil {
		panic(err)
	}
	return shared.DefaultCodegenPolicy(selection)
}
