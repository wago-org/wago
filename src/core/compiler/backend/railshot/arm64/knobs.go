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
	optimization.Bind("entry-arg-pins", &entryArgPinsEnabled),
	optimization.Bind("x8-pin", &callFreeX8PinEnabled),
	optimization.Bind("deep-fp-pins", &deepFPPinsEnabled),
	optimization.Bind("ext-fp-pins", &extendedFPPinsEnabled),
	optimization.Bind("leaf-scratch-pins", &leafScratchPinsEnabled),
	optimization.Bind("immutable-table", &immutableLocalTableEnabled),
	optimization.Bind("immutable-table-type", &immutableTableTypeEnabled),
	optimization.Bind("inline-callfree", &inlineCallFreeHintsEnabled),
	optimization.Bind("store-forward", &linearStoreForwardEnabled),
	optimization.Bind("frame-elide-reghomed", &frameElideRegHomed),
	optimization.Bind("small-frame", &smallFrameAdjustEnabled),
	optimization.Bind("v128-const-cache", &v128ConstCacheEnabled),
	optimization.Bind("v128-pins", &v128LocalPinsEnabled),
	optimization.Bind("v128-sink", &v128LocalSinkEnabled),
	optimization.Bind("reg-abi", &regABIEnabled),
	optimization.Bind("inline", &inlineEnabled),
	optimization.Bind("inline-loop-callees", &inlineLoopCallees),
	optimization.Bind("loop-precheck", &loopPrecheckEnabled),
	optimization.Bind("loop-region-pins", &loopRegionPinsEnabled),
	optimization.Bind("immutable-poly-fastpath", &immutableLocalPolyFastPath),
	optimization.Bind("legacy-fp-pins", &legacyFPPinsEnabled),
	optimization.Bind("legacy-gp-pins", &legacyGPPinsEnabled),
	optimization.BindInverted("stack-fence", &noStackFence),
	optimization.BindInverted("stack-reg", &noStackReg),
)

var (
	optBoundsFacts           = optimizationBindings.Option("bounds-facts")
	optSTFlags               = optimizationBindings.Option("st-flags")
	optRegMerge              = optimizationBindings.Option("reg-merge")
	optTeeSink               = optimizationBindings.Option("tee-sink")
	optUnarySink             = optimizationBindings.Option("unary-sink")
	optThreeOpSink           = optimizationBindings.Option("three-op-sink")
	optOldDestRHSSink        = optimizationBindings.Option("olddest-rhs-sink")
	optBranchFold            = optimizationBindings.Option("branch-fold")
	optStoreLoadFwd          = optimizationBindings.Option("store-load-fwd")
	optUXTWAdd               = optimizationBindings.Option("uxtw-add")
	optValueFacts            = optimizationBindings.Option("value-facts")
	optLoadPair              = optimizationBindings.Option("load-pair")
	optEntryArgPins          = optimizationBindings.Option("entry-arg-pins")
	optX8Pin                 = optimizationBindings.Option("x8-pin")
	optDeepFPPins            = optimizationBindings.Option("deep-fp-pins")
	optExtendedFPPins        = optimizationBindings.Option("ext-fp-pins")
	optLeafScratchPins       = optimizationBindings.Option("leaf-scratch-pins")
	optImmutableTable        = optimizationBindings.Option("immutable-table")
	optImmutableTableType    = optimizationBindings.Option("immutable-table-type")
	optInlineCallFree        = optimizationBindings.Option("inline-callfree")
	optStoreForward          = optimizationBindings.Option("store-forward")
	optFrameElideRegHomed    = optimizationBindings.Option("frame-elide-reghomed")
	optSmallFrame            = optimizationBindings.Option("small-frame")
	optV128ConstCache        = optimizationBindings.Option("v128-const-cache")
	optV128Pins              = optimizationBindings.Option("v128-pins")
	optV128Sink              = optimizationBindings.Option("v128-sink")
	optRegABI                = optimizationBindings.Option("reg-abi")
	optInline                = optimizationBindings.Option("inline")
	optInlineLoopCallees     = optimizationBindings.Option("inline-loop-callees")
	optLoopPrecheck          = optimizationBindings.Option("loop-precheck")
	optLoopRegionPins        = optimizationBindings.Option("loop-region-pins")
	optImmutablePolyFastPath = optimizationBindings.Option("immutable-poly-fastpath")
	optLegacyFPPins          = optimizationBindings.Option("legacy-fp-pins")
	optLegacyGPPins          = optimizationBindings.Option("legacy-gp-pins")
	optStackFence            = optimizationBindings.Option("stack-fence")
	optStackReg              = optimizationBindings.Option("stack-reg")
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
	selection, err := optimizationBindings.ResolveSnapshot(nil, OptimizationSnapshot{}, nil)
	if err != nil {
		panic(err)
	}
	return shared.DefaultCodegenPolicy(selection)
}
