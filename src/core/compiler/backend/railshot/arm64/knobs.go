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
	specs := []optimization.BindingSpec{
		optimization.Bind("layout-padding-skip", &layoutPaddingSkipEnabled),
		optimization.Bind("memory-min-bounds", &memoryMinimumBoundsEnabled),
		optimization.Bind("bounds-facts", &boundsFactsEnabled),
		optimization.Bind("byte-reduce-loop", &byteReduceLoopEnabled),
		optimization.Bind("straight-line-block", &straightLineBlockEnabled),
		optimization.Bind("loop-local-params", &loopLocalParamsEnabled),
		optimization.Bind("scalar-loop-unroll", &scalarLoopUnrollEnabled),
		optimization.Bind("tuple-if-select", &tupleIfSelectEnabled),
		optimization.Bind("pure-integer-if-select", &pureIntegerIfSelectEnabled),
		optimization.Bind("guarded-div-rem", &guardedDivRemEnabled),
		optimization.Bind("cold-tuple-loop-exit", &coldTupleLoopExitEnabled),
		optimization.Bind("interval-region-pins", &intervalRegionPinsEnabled),
		optimization.Bind("interval-next-use", &intervalNextUseEnabled),
		optimization.Bind("regional-memory-read", &regionalMemoryReadEnabled),
		optimization.Bind("interval-call-regions", &intervalCallRegionsEnabled),
		optimization.Bind("linear-sum-loop", &linearSumLoopEnabled),
		optimization.Bind("multi-bounds-cert", &multiBoundsCertEnabled),
		optimization.Bind("conditional-bounds-fallthrough", &conditionalBoundsEnabled),
		optimization.Bind("repeated-native-address", &repeatedNativeAddressEnabled),
		optimization.Bind("repeated-store-region", &repeatedStoreRegionEnabled),
		optimization.Bind("leaf-scratch-memsize", &leafScratchMemSizeEnabled),
		optimization.Bind("loop-trap-cell", &loopTrapCellEnabled),
		optimization.Bind("prepared-direct-entry", &preparedDirectEntryEnabled),
		optimization.Bind("prepared-light-entry", &preparedLightEntryEnabled),
		optimization.Bind("prepared-bounded-entry", &preparedBoundedEntryEnabled),
		optimization.Bind("native-leaf-host", &nativeLeafHostEnabled),
		optimization.Bind("direct-go-host-import", &directGoImportEnabled),
		optimization.Bind("loop-int-const", &loopIntConstEnabled),
		optimization.Bind("scoped-loop-int-const", &scopedLoopConstsEnabled),
		optimization.Bind("scoped-loop-float-const", &scopedLoopFloatConstsEnabled),
		optimization.Bind("loop-memory-base", &loopMemoryBaseEnabled),
		optimization.Bind("indexed-base-reuse", &indexedBaseReuseEnabled),
		optimization.Bind("linear-integer-tree", &linearIntegerTreeEnabled),
		optimization.Bind("dominated-indexed-base", &dominatedIndexedBaseEnabled),
		optimization.Bind("dominated-indexed-key", &dominatedIndexedKeyEnabled),
		optimization.Bind("ordinary-shifted-immediate", &ordinaryShiftedImmediateEnabled),
		optimization.Bind("common-exit-compare", &commonExitCompareEnabled),
		optimization.Bind("taken-zero-branch", &takenZeroBranchEnabled),
		optimization.Bind("cset-lineage", &csetLineageEnabled),
		optimization.Bind("guarded-boolean-xor", &guardedBooleanXorEnabled),
		optimization.Bind("integer-select-local-read", &integerSelectLocalReadEnabled),
		optimization.Bind("borrowed-div-rem", &borrowedDivRemEnabled),
		optimization.Bind("bounded-signed-quotient", &boundedSignedQuotientEnabled),
		optimization.Bind("br-table-branch-vector", &brTableBranchVectorEnabled),
		optimization.Bind("bounded-branch-index", &boundedBranchIndexEnabled),
		optimization.Bind("convert-read", &convertReadEnabled),
		optimization.Bind("cold-call-local-pins", &coldCallLocalPinsEnabled),
		optimization.Bind("magic-div", &magicDivEnabled),
		optimization.Bind("signed-quotient-direct", &signedQuotientDirectEnabled),
		optimization.Bind("shared-trap-body", &sharedTrapBodyEnabled),
		optimization.Bind("shared-adapters", &sharedAdaptersEnabled),
		optimization.Bind("st-flags", &stFlagsEnabled),
		optimization.Bind("reg-merge", &regMergeEnabled),
		optimization.Bind("register-call-borrowed-args", &borrowedRegisterArgsEnabled),
		optimization.Bind("tee-sink", &teeLocalSinkEnabled),
		optimization.Bind("unary-sink", &unaryLocalSinkEnabled),
		optimization.Bind("three-op-sink", &threeOperandSinkEnabled),
		optimization.Bind("olddest-rhs-sink", &oldDestRHSSinkEnabled),
		optimization.Bind("branch-fold", &branchFoldEnabled),
		optimization.Bind("store-load-fwd", &storeLoadFwdEnabled),
		optimization.Bind("byte-store-pair", &byteStorePairEnabled),
		optimization.Bind("float-memory-borrow", &floatMemoryBorrowEnabled),
		optimization.Bind("select-subtract", &selectSubtractEnabled),
		optimization.Bind("affine-bounds-loop", &affineBoundsLoopEnabled),
		optimization.Bind("native-loop-const", &nativeLoopConstEnabled),
		optimization.Bind("arithmetic-tee-branch", &arithmeticTeeBranchEnabled),
		optimization.Bind("uxtw-add", &uxtwAddEnabled),
		optimization.Bind("shifted-register-alu", &shiftedRegisterALUEnabled),
		optimization.Bind("inverted-logical", &invertedLogicalEnabled),
		optimization.Bind("select-group-guard", &selectGroupEnabled),
		optimization.Bind("dot-loop-vector", &dotLoopEnabled),
		optimization.Bind("regional-condition-fusion", &regionalConditionFusionEnabled),
		optimization.Bind("pure-reduce-vector", &pureReduceEnabled),
		optimization.Bind("wide-reuse-pins", &wideReusePinsEnabled),
		optimization.Bind("guarded-test-ccmp", &guardedTestCCMPEnabled),
		optimization.Bind("crowded-products", &crowdedProductsEnabled),
		optimization.Bind("streaming-i32-reduction", &streamReduceEnabled),
		optimization.Bind("single-negative-move32", &singleNegativeMove32Enabled),
		optimization.Bind("select-source-read", &selectSourceReadEnabled),
		optimization.Bind("select-sink-pure-guard", &selectSinkPureGuardEnabled),
		optimization.Bind("fp-immediate-const", &fpImmediateConstEnabled),
		optimization.Bind("fp-literal-pool", &floatLiteralPoolEnabled),
		optimization.Bind("value-facts", &valueFactsEnabled),
		optimization.Bind("load-pair", &loadPairEnabled),
		optimization.Bind("merge-next-use", &mergeNextUseEnabled),
		optimization.Bind("counted-loop-latch", &countedLoopLatchEnabled),
		optimization.Bind("counted-loop-body-align", &countedLoopBodyAlignEnabled),
		optimization.Bind("callfree-loop-cold-exit", &callFreeLoopColdExitEnabled),
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
		optimization.Bind("reg-abi", &regABIEnabled),
		optimization.Bind("inline", &inlineEnabled),
		optimization.Bind("inline-integer-tuples", &inlineIntegerTuplesEnabled),
		optimization.BindInverted("stack-fence", &noStackFence),
		optimization.BindInverted("stack-reg", &noStackReg),
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
