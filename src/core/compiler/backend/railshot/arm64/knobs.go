// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/optimization"
)

// The binding inventory is checked against the arm64 Optimization Definitions
// during package initialization. Public sense is always "on = enabled".
var optimizationBindings = newOptimizationBindings()

func newOptimizationBindings() *optimization.Bindings {
	// Literals let the compiler store the unconditional bindings as data.
	specs := []optimization.BindingSpec{
		{Name: "layout-padding-skip", Value: &layoutPaddingSkipEnabled},
		{Name: "memory-min-bounds", Value: &memoryMinimumBoundsEnabled},
		{Name: "bounds-facts", Value: &boundsFactsEnabled},
		{Name: "byte-reduce-loop", Value: &byteReduceLoopEnabled},
		{Name: "straight-line-block", Value: &straightLineBlockEnabled},
		{Name: "loop-local-params", Value: &loopLocalParamsEnabled},
		{Name: "scalar-loop-unroll", Value: &scalarLoopUnrollEnabled},
		{Name: "tuple-if-select", Value: &tupleIfSelectEnabled},
		{Name: "pure-integer-if-select", Value: &pureIntegerIfSelectEnabled},
		{Name: "guarded-div-rem", Value: &guardedDivRemEnabled},
		{Name: "cold-tuple-loop-exit", Value: &coldTupleLoopExitEnabled},
		{Name: "interval-region-pins", Value: &intervalRegionPinsEnabled},
		{Name: "interval-next-use", Value: &intervalNextUseEnabled},
		{Name: "regional-memory-read", Value: &regionalMemoryReadEnabled},
		{Name: "interval-call-regions", Value: &intervalCallRegionsEnabled},
		{Name: "linear-sum-loop", Value: &linearSumLoopEnabled},
		{Name: "multi-bounds-cert", Value: &multiBoundsCertEnabled},
		{Name: "conditional-bounds-fallthrough", Value: &conditionalBoundsEnabled},
		{Name: "repeated-native-address", Value: &repeatedNativeAddressEnabled},
		{Name: "repeated-store-region", Value: &repeatedStoreRegionEnabled},
		{Name: "leaf-scratch-memsize", Value: &leafScratchMemSizeEnabled},
		{Name: "loop-trap-cell", Value: &loopTrapCellEnabled},
		{Name: "prepared-direct-entry", Value: &preparedDirectEntryEnabled},
		{Name: "prepared-light-entry", Value: &preparedLightEntryEnabled},
		{Name: "prepared-bounded-entry", Value: &preparedBoundedEntryEnabled},
		{Name: "native-leaf-host", Value: &nativeLeafHostEnabled},
		{Name: "direct-go-host-import", Value: &directGoImportEnabled},
		{Name: "loop-int-const", Value: &loopIntConstEnabled},
		{Name: "scoped-loop-int-const", Value: &scopedLoopConstsEnabled},
		{Name: "scoped-loop-float-const", Value: &scopedLoopFloatConstsEnabled},
		{Name: "loop-memory-base", Value: &loopMemoryBaseEnabled},
		{Name: "indexed-base-reuse", Value: &indexedBaseReuseEnabled},
		{Name: "linear-integer-tree", Value: &linearIntegerTreeEnabled},
		{Name: "dominated-indexed-base", Value: &dominatedIndexedBaseEnabled},
		{Name: "dominated-indexed-key", Value: &dominatedIndexedKeyEnabled},
		{Name: "ordinary-shifted-immediate", Value: &ordinaryShiftedImmediateEnabled},
		{Name: "common-exit-compare", Value: &commonExitCompareEnabled},
		{Name: "taken-zero-branch", Value: &takenZeroBranchEnabled},
		{Name: "cset-lineage", Value: &csetLineageEnabled},
		{Name: "guarded-boolean-xor", Value: &guardedBooleanXorEnabled},
		{Name: "integer-select-local-read", Value: &integerSelectLocalReadEnabled},
		{Name: "borrowed-div-rem", Value: &borrowedDivRemEnabled},
		{Name: "bounded-signed-quotient", Value: &boundedSignedQuotientEnabled},
		{Name: "br-table-branch-vector", Value: &brTableBranchVectorEnabled},
		{Name: "bounded-branch-index", Value: &boundedBranchIndexEnabled},
		{Name: "convert-read", Value: &convertReadEnabled},
		{Name: "cold-call-local-pins", Value: &coldCallLocalPinsEnabled},
		{Name: "magic-div", Value: &magicDivEnabled},
		{Name: "signed-quotient-direct", Value: &signedQuotientDirectEnabled},
		{Name: "shared-trap-body", Value: &sharedTrapBodyEnabled},
		{Name: "shared-adapters", Value: &sharedAdaptersEnabled},
		{Name: "st-flags", Value: &stFlagsEnabled},
		{Name: "reg-merge", Value: &regMergeEnabled},
		{Name: "register-call-borrowed-args", Value: &borrowedRegisterArgsEnabled},
		{Name: "tee-sink", Value: &teeLocalSinkEnabled},
		{Name: "unary-sink", Value: &unaryLocalSinkEnabled},
		{Name: "three-op-sink", Value: &threeOperandSinkEnabled},
		{Name: "olddest-rhs-sink", Value: &oldDestRHSSinkEnabled},
		{Name: "branch-fold", Value: &branchFoldEnabled},
		{Name: "store-load-fwd", Value: &storeLoadFwdEnabled},
		{Name: "byte-store-pair", Value: &byteStorePairEnabled},
		{Name: "float-memory-borrow", Value: &floatMemoryBorrowEnabled},
		{Name: "select-subtract", Value: &selectSubtractEnabled},
		{Name: "affine-bounds-loop", Value: &affineBoundsLoopEnabled},
		{Name: "native-loop-const", Value: &nativeLoopConstEnabled},
		{Name: "arithmetic-tee-branch", Value: &arithmeticTeeBranchEnabled},
		{Name: "uxtw-add", Value: &uxtwAddEnabled},
		{Name: "shifted-register-alu", Value: &shiftedRegisterALUEnabled},
		{Name: "inverted-logical", Value: &invertedLogicalEnabled},
		{Name: "select-group-guard", Value: &selectGroupEnabled},
		{Name: "dot-loop-vector", Value: &dotLoopEnabled},
		{Name: "regional-condition-fusion", Value: &regionalConditionFusionEnabled},
		{Name: "pure-reduce-vector", Value: &pureReduceEnabled},
		{Name: "wide-reuse-pins", Value: &wideReusePinsEnabled},
		{Name: "guarded-test-ccmp", Value: &guardedTestCCMPEnabled},
		{Name: "crowded-products", Value: &crowdedProductsEnabled},
		{Name: "streaming-i32-reduction", Value: &streamReduceEnabled},
		{Name: "single-negative-move32", Value: &singleNegativeMove32Enabled},
		{Name: "select-source-read", Value: &selectSourceReadEnabled},
		{Name: "select-sink-pure-guard", Value: &selectSinkPureGuardEnabled},
		{Name: "fp-immediate-const", Value: &fpImmediateConstEnabled},
		{Name: "fp-literal-pool", Value: &floatLiteralPoolEnabled},
		{Name: "value-facts", Value: &valueFactsEnabled},
		{Name: "load-pair", Value: &loadPairEnabled},
		{Name: "merge-next-use", Value: &mergeNextUseEnabled},
		{Name: "counted-loop-latch", Value: &countedLoopLatchEnabled},
		{Name: "counted-loop-body-align", Value: &countedLoopBodyAlignEnabled},
		{Name: "callfree-loop-cold-exit", Value: &callFreeLoopColdExitEnabled},
		{Name: "weighted-scalar-merge", Value: &weightedScalarMergeEnabled},
		{Name: "entry-param-pairs", Value: &entryParamPairsEnabled},
		{Name: "entry-zero-pairs", Value: &entryZeroPairsEnabled},
		{Name: "entry-arg-pins", Value: &entryArgPinsEnabled},
		{Name: "x8-pin", Value: &callFreeX8PinEnabled},
		{Name: "ext-fp-pins", Value: &extendedFPPinsEnabled},
		{Name: "leaf-scratch-pins", Value: &leafScratchPinsEnabled},
		{Name: "immutable-table", Value: &immutableLocalTableEnabled},
		{Name: "immutable-table-type", Value: &immutableTableTypeEnabled},
		{Name: "inline-callfree", Value: &inlineCallFreeHintsEnabled},
		{Name: "store-forward", Value: &linearStoreForwardEnabled},
		{Name: "frame-elide-reghomed", Value: &frameElideRegHomed},
		{Name: "small-frame", Value: &smallFrameAdjustEnabled},
		{Name: "zero-branch", Value: &zeroBranchEnabled},
		{Name: "mul-add-fuse", Value: &mulAddFuseEnabled},
		{Name: "entry-init-elision", Value: &entryInitElisionEnabled},
		{Name: "reg-abi", Value: &regABIEnabled},
		{Name: "inline", Value: &inlineEnabled},
		{Name: "inline-integer-tuples", Value: &inlineIntegerTuplesEnabled},
		{Name: "stack-fence", Value: &noStackFence, Inverted: true},
		{Name: "stack-reg", Value: &noStackReg, Inverted: true},
	}
	if buildfeatures.SIMD != 0 {
		v128ConstCacheEnabled = os.Getenv("WAGO_ARM64_NO_V128_CONST_CACHE") != "1"
		simdSuperoptEnabled = os.Getenv("WAGO_NO_SIMD_SUPEROPT") != "1"
		v128DirectResultEnabled = os.Getenv("WAGO_ARM64_NO_V128_DIRECT_RESULTS") != "1"
		v128LocalPinsEnabled = os.Getenv("WAGO_ARM64_NO_V128_PINS") != "1"
		specs = append(specs,
			optimization.BindOption("v128-const-cache", &v128ConstCacheEnabled, &optV128ConstCache),
			optimization.BindOption("simd-superopt", &simdSuperoptEnabled, &optSIMDSuperopt),
			optimization.BindOption("simd-load-extract", &simdLoadExtractEnabled, &optSIMDLoadExtract),
			optimization.BindOption("simd-store-load-address", &simdStoreLoadAddressEnabled, &optSIMDStoreLoadAddress),
			optimization.BindOption("v128-direct-results", &v128DirectResultEnabled, &optV128DirectResults),
			optimization.BindOption("v128-pins", &v128LocalPinsEnabled, &optV128Pins),
		)
	}
	if buildfeatures.Threads != 0 {
		specs = append(specs,
			optimization.BindOption("atomic-wait-zero-timeout", &atomicZeroWaitEnabled, &optAtomicZeroWait),
			optimization.BindOption("atomic-notify-empty", &atomicNotifyEmptyEnabled, &optAtomicNotifyEmpty),
			optimization.BindOption("atomic-static-bounds", &atomicStaticBoundsEnabled, &optAtomicStaticBounds),
			optimization.BindOption("shared-memory-base", &sharedMemoryBaseEnabled, &optSharedMemoryBase),
			optimization.BindOption("atomic-lse", &atomicLSEEnabled, &optAtomicLSE),
		)
	}
	if buildfeatures.BulkMemoryOperations != 0 {
		memcopyTail4Enabled = os.Getenv("WAGO_ARM64_NO_MEMCOPY_TAIL4") != "1"
		memcopyQPairsEnabled = os.Getenv("WAGO_ARM64_NO_MEMCOPY_QPAIRS") != "1"
		specs = append(specs,
			optimization.BindOption("bulk-fill-vector", &bulkFillVectorEnabled, &optBulkFillVector),
			optimization.BindOption("bulk-copy-vector", &bulkCopyVectorEnabled, &optBulkCopyVector),
			optimization.BindOption("bulk-memory-loops", &bulkMemoryLoopsEnabled, &optBulkMemoryLoops),
			optimization.BindOption("bulk-fill-copy-forward", &bulkFillCopyForwardEnabled, &optBulkFillCopyForward),
			optimization.BindOption("memcopy-tail4", &memcopyTail4Enabled, &optMemcopyTail4),
			optimization.BindOption("memcopy-qpairs", &memcopyQPairsEnabled, &optMemcopyQPairs),
		)
	}
	if buildfeatures.TailCall != 0 {
		specs = append(specs,
			optimization.BindOption("tail-self-loop", &tailSelfLoopEnabled, &optTailSelfLoop),
			optimization.BindOption("tail-loop-inline", &tailLoopInlineEnabled, &optTailLoopInline),
		)
	}
	return optimization.NewBindings("arm64", specs...)
}

var (
	optMemoryMinimumBounds     = optimizationBindings.Option("memory-min-bounds")
	optAtomicNotifyEmpty       optimization.Option
	optAtomicStaticBounds      optimization.Option
	optAtomicZeroWait          optimization.Option
	optSharedMemoryBase        optimization.Option
	optAtomicLSE               optimization.Option
	optBoundsFacts             = optimizationBindings.Option("bounds-facts")
	optStraightLineBlock       = optimizationBindings.Option("straight-line-block")
	optLoopLocalParams         = optimizationBindings.Option("loop-local-params")
	optScalarLoopUnroll        = optimizationBindings.Option("scalar-loop-unroll")
	optTupleIfSelect           = optimizationBindings.Option("tuple-if-select")
	optPureIntegerIfSelect     = optimizationBindings.Option("pure-integer-if-select")
	optGuardedDivRem           = optimizationBindings.Option("guarded-div-rem")
	optColdTupleLoopExit       = optimizationBindings.Option("cold-tuple-loop-exit")
	optBulkFillVector          optimization.Option
	optSIMDStoreLoadAddress    optimization.Option
	optSIMDLoadExtract         optimization.Option
	optSIMDSuperopt            optimization.Option
	optIntervalRegionPins      = optimizationBindings.Option("interval-region-pins")
	optIntervalNextUse         = optimizationBindings.Option("interval-next-use")
	optRegionalMemoryRead      = optimizationBindings.Option("regional-memory-read")
	optIntervalCallRegions     = optimizationBindings.Option("interval-call-regions")
	optLinearSumLoop           = optimizationBindings.Option("linear-sum-loop")
	optMultiBoundsCert         = optimizationBindings.Option("multi-bounds-cert")
	optConditionalBounds       = optimizationBindings.Option("conditional-bounds-fallthrough")
	optRepeatedNativeAddress   = optimizationBindings.Option("repeated-native-address")
	optRepeatedStoreRegion     = optimizationBindings.Option("repeated-store-region")
	optLeafScratchMemSize      = optimizationBindings.Option("leaf-scratch-memsize")
	optLoopTrapCell            = optimizationBindings.Option("loop-trap-cell")
	optPreparedDirectEntry     = optimizationBindings.Option("prepared-direct-entry")
	optPreparedLightEntry      = optimizationBindings.Option("prepared-light-entry")
	optPreparedBoundedEntry    = optimizationBindings.Option("prepared-bounded-entry")
	optNativeLeafHost          = optimizationBindings.Option("native-leaf-host")
	optDirectGoHostImport      = optimizationBindings.Option("direct-go-host-import")
	optLoopIntConst            = optimizationBindings.Option("loop-int-const")
	optScopedLoopIntConst      = optimizationBindings.Option("scoped-loop-int-const")
	optScopedLoopFloatConst    = optimizationBindings.Option("scoped-loop-float-const")
	optLoopMemoryBase          = optimizationBindings.Option("loop-memory-base")
	optCommonExitCompare       = optimizationBindings.Option("common-exit-compare")
	optTakenZeroBranch         = optimizationBindings.Option("taken-zero-branch")
	optCsetLineage             = optimizationBindings.Option("cset-lineage")
	optGuardedBooleanXor       = optimizationBindings.Option("guarded-boolean-xor")
	optSelectLocalRead         = optimizationBindings.Option("integer-select-local-read")
	optBorrowedDivRem          = optimizationBindings.Option("borrowed-div-rem")
	optBoundedQuotient         = optimizationBindings.Option("bounded-signed-quotient")
	optBrTableBranchVector     = optimizationBindings.Option("br-table-branch-vector")
	optBoundedBranchIndex      = optimizationBindings.Option("bounded-branch-index")
	optDominatedIndexedBase    = optimizationBindings.Option("dominated-indexed-base")
	optDominatedIndexedKey     = optimizationBindings.Option("dominated-indexed-key")
	optOrdinaryShiftedImm      = optimizationBindings.Option("ordinary-shifted-immediate")
	optIndexedBaseReuse        = optimizationBindings.Option("indexed-base-reuse")
	optLinearIntegerTree       = optimizationBindings.Option("linear-integer-tree")
	optConvertRead             = optimizationBindings.Option("convert-read")
	optColdCallLocalPins       = optimizationBindings.Option("cold-call-local-pins")
	optMagicDiv                = optimizationBindings.Option("magic-div")
	optSignedQuotientDirect    = optimizationBindings.Option("signed-quotient-direct")
	optSharedTrapBody          = optimizationBindings.Option("shared-trap-body")
	optSharedAdapters          = optimizationBindings.Option("shared-adapters")
	optSTFlags                 = optimizationBindings.Option("st-flags")
	optRegMerge                = optimizationBindings.Option("reg-merge")
	optBorrowedRegisterArgs    = optimizationBindings.Option("register-call-borrowed-args")
	optTeeSink                 = optimizationBindings.Option("tee-sink")
	optUnarySink               = optimizationBindings.Option("unary-sink")
	optThreeOpSink             = optimizationBindings.Option("three-op-sink")
	optOldDestRHSSink          = optimizationBindings.Option("olddest-rhs-sink")
	optBranchFold              = optimizationBindings.Option("branch-fold")
	optStoreLoadFwd            = optimizationBindings.Option("store-load-fwd")
	optSelectSubtract          = optimizationBindings.Option("select-subtract")
	optAffineBoundsLoop        = optimizationBindings.Option("affine-bounds-loop")
	optNativeLoopConst         = optimizationBindings.Option("native-loop-const")
	optArithmeticTeeBranch     = optimizationBindings.Option("arithmetic-tee-branch")
	optFloatMemoryBorrow       = optimizationBindings.Option("float-memory-borrow")
	optByteStorePair           = optimizationBindings.Option("byte-store-pair")
	optMemcopyTail4            optimization.Option
	optMemcopyQPairs           optimization.Option
	optUXTWAdd                 = optimizationBindings.Option("uxtw-add")
	optSelectGroupGuard        = optimizationBindings.Option("select-group-guard")
	optDotLoopVector           = optimizationBindings.Option("dot-loop-vector")
	optRegionalConditionFusion = optimizationBindings.Option("regional-condition-fusion")
	optPureReduceVector        = optimizationBindings.Option("pure-reduce-vector")
	optWideReusePins           = optimizationBindings.Option("wide-reuse-pins")
	optGuardedTestCCMP         = optimizationBindings.Option("guarded-test-ccmp")
	optCrowdedProducts         = optimizationBindings.Option("crowded-products")
	optStreamReduce            = optimizationBindings.Option("streaming-i32-reduction")
	optSingleNegativeMove32    = optimizationBindings.Option("single-negative-move32")
	optSelectSourceRead        = optimizationBindings.Option("select-source-read")
	optSelectSinkPureGuard     = optimizationBindings.Option("select-sink-pure-guard")
	optInvertedLogical         = optimizationBindings.Option("inverted-logical")
	optShiftedRegisterALU      = optimizationBindings.Option("shifted-register-alu")
	optFPImmediateConst        = optimizationBindings.Option("fp-immediate-const")
	optFPLiteralPool           = optimizationBindings.Option("fp-literal-pool")
	optValueFacts              = optimizationBindings.Option("value-facts")
	optLoadPair                = optimizationBindings.Option("load-pair")
	optMergeNextUse            = optimizationBindings.Option("merge-next-use")
	optCountedLoopLatch        = optimizationBindings.Option("counted-loop-latch")
	optCountedLoopBodyAlign    = optimizationBindings.Option("counted-loop-body-align")
	optCallFreeLoopColdExit    = optimizationBindings.Option("callfree-loop-cold-exit")
	optWeightedScalarMerge     = optimizationBindings.Option("weighted-scalar-merge")
	optEntryParamPairs         = optimizationBindings.Option("entry-param-pairs")
	optEntryZeroPairs          = optimizationBindings.Option("entry-zero-pairs")
	optEntryArgPins            = optimizationBindings.Option("entry-arg-pins")
	optX8Pin                   = optimizationBindings.Option("x8-pin")
	optExtendedFPPins          = optimizationBindings.Option("ext-fp-pins")
	optLeafScratchPins         = optimizationBindings.Option("leaf-scratch-pins")
	optImmutableTable          = optimizationBindings.Option("immutable-table")
	optImmutableTableType      = optimizationBindings.Option("immutable-table-type")
	optInlineCallFree          = optimizationBindings.Option("inline-callfree")
	optStoreForward            = optimizationBindings.Option("store-forward")
	optFrameElideRegHomed      = optimizationBindings.Option("frame-elide-reghomed")
	optSmallFrame              = optimizationBindings.Option("small-frame")
	optZeroBranch              = optimizationBindings.Option("zero-branch")
	optMulAddFuse              = optimizationBindings.Option("mul-add-fuse")
	optEntryInitElision        = optimizationBindings.Option("entry-init-elision")
	optV128DirectResults       optimization.Option
	optV128ConstCache          optimization.Option
	optV128Pins                optimization.Option
	optTailSelfLoop            optimization.Option
	optTailLoopInline          optimization.Option
	optLayoutPaddingSkip       = optimizationBindings.Option("layout-padding-skip")
	optRegABI                  = optimizationBindings.Option("reg-abi")
	optInline                  = optimizationBindings.Option("inline")
	optInlineIntegerTuples     = optimizationBindings.Option("inline-integer-tuples")
	optStackFence              = optimizationBindings.Option("stack-fence")
	optStackReg                = optimizationBindings.Option("stack-reg")
	optBulkCopyVector          optimization.Option
	optBulkMemoryLoops         optimization.Option
	optBulkFillCopyForward     optimization.Option
	optByteReduceLoop          = optimizationBindings.Option("byte-reduce-loop")
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
