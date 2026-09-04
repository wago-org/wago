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
	optimization.Bind("uxtw-add", &uxtwAddEnabled),
	optimization.Bind("value-facts", &valueFactsEnabled),
	optimization.Bind("load-pair", &loadPairEnabled),
	optimization.Bind("merge-next-use", &mergeNextUseEnabled),
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
	optBoundsFacts        = optimizationBindings.Option("bounds-facts")
	optSIMDSuperopt       = optimizationBindings.Option("simd-superopt")
	optIntervalRegionPins = optimizationBindings.Option("interval-region-pins")
	optMagicDiv           = optimizationBindings.Option("magic-div")
	optSharedTrapBody     = optimizationBindings.Option("shared-trap-body")
	optSharedAdapters     = optimizationBindings.Option("shared-adapters")
	optSTFlags            = optimizationBindings.Option("st-flags")
	optRegMerge           = optimizationBindings.Option("reg-merge")
	optTeeSink            = optimizationBindings.Option("tee-sink")
	optUnarySink          = optimizationBindings.Option("unary-sink")
	optThreeOpSink        = optimizationBindings.Option("three-op-sink")
	optOldDestRHSSink     = optimizationBindings.Option("olddest-rhs-sink")
	optBranchFold         = optimizationBindings.Option("branch-fold")
	optStoreLoadFwd       = optimizationBindings.Option("store-load-fwd")
	optUXTWAdd            = optimizationBindings.Option("uxtw-add")
	optValueFacts         = optimizationBindings.Option("value-facts")
	optLoadPair           = optimizationBindings.Option("load-pair")
	optMergeNextUse       = optimizationBindings.Option("merge-next-use")
	optEntryParamPairs    = optimizationBindings.Option("entry-param-pairs")
	optEntryZeroPairs     = optimizationBindings.Option("entry-zero-pairs")
	optEntryArgPins       = optimizationBindings.Option("entry-arg-pins")
	optX8Pin              = optimizationBindings.Option("x8-pin")
	optExtendedFPPins     = optimizationBindings.Option("ext-fp-pins")
	optLeafScratchPins    = optimizationBindings.Option("leaf-scratch-pins")
	optImmutableTable     = optimizationBindings.Option("immutable-table")
	optImmutableTableType = optimizationBindings.Option("immutable-table-type")
	optInlineCallFree     = optimizationBindings.Option("inline-callfree")
	optStoreForward       = optimizationBindings.Option("store-forward")
	optFrameElideRegHomed = optimizationBindings.Option("frame-elide-reghomed")
	optSmallFrame         = optimizationBindings.Option("small-frame")
	optZeroBranch         = optimizationBindings.Option("zero-branch")
	optMulAddFuse         = optimizationBindings.Option("mul-add-fuse")
	optEntryInitElision   = optimizationBindings.Option("entry-init-elision")
	optV128DirectResults  = optimizationBindings.Option("v128-direct-results")
	optV128Pins           = optimizationBindings.Option("v128-pins")
	optRegABI             = optimizationBindings.Option("reg-abi")
	optInline             = optimizationBindings.Option("inline")
	optStackFence         = optimizationBindings.Option("stack-fence")
	optStackReg           = optimizationBindings.Option("stack-reg")
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
