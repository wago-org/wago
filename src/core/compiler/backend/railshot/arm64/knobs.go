//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/optimization"

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

type KnobInfo = optimization.Info
type OptimizationSnapshot = optimization.Snapshot

func OptKnobs() []KnobInfo { return optimizationBindings.Infos() }

func OptKnobSnapshot() ([]KnobInfo, OptimizationSnapshot) { return optimizationBindings.Snapshot() }

func CurrentOptKnobSnapshot() OptimizationSnapshot { return optimizationBindings.CurrentSnapshot() }

func SetOptKnob(name string, on bool) bool { return optimizationBindings.Set(name, on) }
