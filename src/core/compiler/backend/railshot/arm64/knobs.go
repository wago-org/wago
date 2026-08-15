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
	optimization.Bind("call-effect-bounds", &callEffectBoundsEnabled),
	optimization.Bind("abi-classes", &abiClassesEnabled),
	optimization.Bind("abi-leaf-fp", &abiLeafFPEnabled),
	optimization.Bind("prepared-fp-entry", &preparedFPEntryEnabled),
	optimization.Bind("entry-init-elide", &entryInitElisionEnabled),
	optimization.Bind("gc-dead-new", &deadGCNewEnabled),
	optimization.Bind("gc-fixed-array-len", &fixedGCArrayLenEnabled),
	optimization.Bind("gc-const-struct-get", &constGCStructGetEnabled),
	optimization.Bind("gc-constructor-cast", &gcConstructorCastEnabled),
	optimization.Bind("gc-native-final-cast", &nativeGCFinalCastEnabled),
	optimization.Bind("gc-native-final-array-len", &nativeGCFinalArrayLenEnabled),
	optimization.Bind("gc-native-final-scalar-get", &nativeGCFinalScalarGetEnabled),
	optimization.Bind("gc-native-final-scalar-set", &nativeGCFinalScalarSetEnabled),
	optimization.Bind("gc-native-final-ref-get", &nativeGCFinalRefGetEnabled),
	optimization.Bind("gc-native-final-ref-set", &nativeGCFinalRefSetEnabled),
	optimization.Bind("gc-native-final-array-scalar-get", &nativeGCFinalArrayScalarGetEnabled),
	optimization.Bind("gc-native-final-array-scalar-set", &nativeGCFinalArrayScalarSetEnabled),
	optimization.Bind("gc-native-resolve-reuse", &nativeGCResolveReuseEnabled),
	optimization.Bind("simd-wide-bitmask-consumer", &simdWideBitmaskConsumerEnabled),
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
	optimization.Bind("shuffle-half-zip", &shuffleHalfZipEnabled),
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
	optCallEffectBounds      = optimizationBindings.Option("call-effect-bounds")
	optABIClasses            = optimizationBindings.Option("abi-classes")
	optABILeafFP             = optimizationBindings.Option("abi-leaf-fp")
	optPreparedFPEntry       = optimizationBindings.Option("prepared-fp-entry")
	optEntryInitElide        = optimizationBindings.Option("entry-init-elide")
	optGCDeadNew             = optimizationBindings.Option("gc-dead-new")
	optGCFixedArrayLen       = optimizationBindings.Option("gc-fixed-array-len")
	optGCConstStructGet      = optimizationBindings.Option("gc-const-struct-get")
	optGCConstructorCast     = optimizationBindings.Option("gc-constructor-cast")
	optGCNativeFinalCast     = optimizationBindings.Option("gc-native-final-cast")
	optGCNativeFinalArrayLen = optimizationBindings.Option("gc-native-final-array-len")
	optGCNativeScalarGet     = optimizationBindings.Option("gc-native-final-scalar-get")
	optGCNativeScalarSet     = optimizationBindings.Option("gc-native-final-scalar-set")
	optGCNativeRefGet        = optimizationBindings.Option("gc-native-final-ref-get")
	optGCNativeRefSet        = optimizationBindings.Option("gc-native-final-ref-set")
	optGCNativeArrayGet      = optimizationBindings.Option("gc-native-final-array-scalar-get")
	optGCNativeArraySet      = optimizationBindings.Option("gc-native-final-array-scalar-set")
	optGCNativeResolveReuse  = optimizationBindings.Option("gc-native-resolve-reuse")
	optSIMDWideBitmask       = optimizationBindings.Option("simd-wide-bitmask-consumer")
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
	optShuffleHalfZip        = optimizationBindings.Option("shuffle-half-zip")
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
	selection, err := optimizationBindings.Resolve(nil)
	if err != nil {
		panic(err)
	}
	return shared.DefaultCodegenPolicy(selection)
}
