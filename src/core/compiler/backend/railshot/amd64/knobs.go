// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"os"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/optimization"
)

// The binding inventory is checked against the amd64 Optimization Definitions
// during package initialization. Public sense is always "on = enabled".
var optimizationBindings = newOptimizationBindings()

func newOptimizationBindings() *optimization.Bindings {
	// Literals let the compiler store the unconditional bindings as data.
	specs := []optimization.BindingSpec{
		{Name: "memory-min-bounds", Value: &memoryMinimumBoundsEnabled},
		{Name: "bounds-facts", Value: &boundsFactsEnabled},
		{Name: "byte-reduce-loop", Value: &byteReduceLoopEnabled},
		{Name: "straight-line-block", Value: &straightLineBlockEnabled},
		{Name: "prepared-direct-entry", Value: &preparedDirectEntryEnabled},
		{Name: "prepared-bounded-entry", Value: &preparedBoundedEntryEnabled},
		{Name: "direct-go-host-import", Value: &directGoImportEnabled},
		{Name: "native-leaf-host", Value: &nativeLeafHostEnabled},
		{Name: "wide-loop-int-const", Value: &wideLoopIntConstEnabled},
		{Name: "compact-loop-align32", Value: &compactLoopAlign32Enabled},
		{Name: "counted-loop-latch", Value: &countedLoopLatchEnabled},
		{Name: "scalar-loop-unroll", Value: &scalarLoopUnrollEnabled},
		{Name: "loop-local-params", Value: &loopLocalParamsEnabled},
		{Name: "tuple-if-select", Value: &tupleIfSelectEnabled},
		{Name: "pure-integer-if-select", Value: &pureIntegerIfSelectEnabled},
		{Name: "linear-integer-tree", Value: &linearIntegerTreeEnabled},
		{Name: "linear-sum-loop", Value: &linearSumLoopEnabled},
		{Name: "callfree-loop-cold-exit", Value: &callFreeLoopColdExitEnabled},
		{Name: "callfree-loop-reg-state", Value: &callFreeLoopRegStateEnabled},
		{Name: "interval-region-pins", Value: &intervalRegionPinsEnabled},
		{Name: "interval-control", Value: &intervalControlEnabled},
		{Name: "interval-next-use", Value: &intervalNextUseEnabled},
		{Name: "interval-scratch-lease", Value: &intervalScratchLeaseEnabled},
		{Name: "interval-r8-lease", Value: &intervalR8LeaseEnabled},
		{Name: "interval-i64-weight", Value: &intervalI64WeightEnabled},
		{Name: "memsize-regional-lease", Value: &memSizeRegionalLeaseEnabled},
		{Name: "module-global-regional-lease", Value: &moduleGlobalRegionalLeaseEnabled},
		{Name: "magic-div", Value: &magicDivEnabled},
		{Name: "borrowed-div-rem", Value: &borrowedDivRemEnabled},
		{Name: "shared-trap-body", Value: &sharedTrapBodyEnabled},
		{Name: "shared-adapters", Value: &sharedAdaptersEnabled},
		{Name: "st-flags", Value: &stFlagsEnabled},
		{Name: "store8-flags", Value: &store8FlagsEnabled},
		{Name: "reg-merge", Value: &regMergeEnabled},
		{Name: "tee-sink", Value: &teeLocalSinkEnabled},
		{Name: "unary-sink", Value: &unaryLocalSinkEnabled},
		{Name: "branch-fold", Value: &branchFoldEnabled},
		{Name: "entry-arg-pins", Value: &entryArgPinsEnabled},
		{Name: "ext-fp-pins", Value: &extendedFPPinsEnabled},
		{Name: "tree-order", Value: &treeOrderEnabled},
		{Name: "assoc-tree", Value: &associativeTreeEnabled},
		{Name: "bmi2-rorx", Value: &bmi2RorxEnabled},
		{Name: "vex-float-mem", Value: &vexFloatMemEnabled},
		{Name: "float-frame-mem", Value: &floatFrameMemEnabled},
		{Name: "float-store-borrow", Value: &floatStoreBorrowEnabled},
		{Name: "memory-compare-immediate", Value: &memoryCompareEnabled},
		{Name: "wide-local-pins", Value: &wideLocalPinsEnabled},
		{Name: "direct-int-branch-merge", Value: &directIntBranchMergeEnabled},
		{Name: "multi-bounds-cert", Value: &multiBoundsCertEnabled},
		{Name: "addr-zext-elim", Value: &memory32AddrZExtElimEnabled},
		{Name: "canonical-i32", Value: &canonicalI32CarriersEnabled},
		{Name: "value-facts", Value: &valueFactsEnabled},
		{Name: "immutable-table", Value: &immutableLocalTableEnabled},
		{Name: "immutable-table-type", Value: &immutableTableTypeEnabled},
		{Name: "inline-callfree", Value: &inlineCallFreeHintsEnabled},
		{Name: "store-forward", Value: &linearStoreForwardEnabled},
		{Name: "frame-elide", Value: &smallFrameElideEnabled},
		{Name: "compact-i32-frame", Value: &compactI32FrameEnabled},
		{Name: "local-slot-order", Value: &localSlotOrderEnabled},
		{Name: "commute-self-update", Value: &commuteSelfUpdateEnabled},
		{Name: "commute-fixed-self-update", Value: &commuteFixedSelfUpdateEnabled},
		{Name: "i64-mask32", Value: &i64Mask32Enabled},
		{Name: "accumulator-immediate", Value: &accumulatorImmediateEnabled},
		{Name: "v128-const-cache", Value: &v128ConstCacheEnabled},
		{Name: "reg-abi", Value: &regABIEnabled},
		{Name: "inline", Value: &inlineEnabled},
		{Name: "stack-fence", Value: &noStackFence, Inverted: true},
		{Name: "stack-reg", Value: &noStackReg, Inverted: true},
	}
	if buildfeatures.SIMD != 0 {
		simdSuperoptEnabled = os.Getenv("WAGO_NO_SIMD_SUPEROPT") != "1"
		avx512TernaryEnabled = os.Getenv("WAGO_AMD64_NO_AVX512_TERNARY") != "1"
		avx512VRotateEnabled = os.Getenv("WAGO_AMD64_NO_AVX512_VROTATE") != "1"
		v128LocalPinsEnabled = os.Getenv("WAGO_AMD64_NO_V128_PINS") != "1"
		specs = append(specs,
			optimization.BindOption("simd-superopt", &simdSuperoptEnabled, &optSIMDSuperopt),
			optimization.BindOption("simd-load-extract", &simdLoadExtractEnabled, &optSIMDLoadExtract),
			optimization.BindOption("simd-store-load-address", &simdStoreLoadAddressEnabled, &optSIMDStoreLoadAddress),
			optimization.BindOption("simd-reduction-borrow", &simdReductionBorrowEnabled, &optSIMDReductionBorrow),
			optimization.BindOption("avx512-ternary", &avx512TernaryEnabled, &optAVX512Ternary),
			optimization.BindOption("avx512-vrotate", &avx512VRotateEnabled, &optAVX512VRotate),
			optimization.BindOption("v128-pins", &v128LocalPinsEnabled, &optV128Pins),
		)
	}
	if buildfeatures.Threads != 0 {
		specs = append(specs,
			optimization.BindOption("atomic-wait-zero-timeout", &atomicZeroWaitEnabled, &optAtomicZeroWait),
			optimization.BindOption("shared-memory-base", &sharedMemoryBaseEnabled, &optSharedMemoryBase),
			optimization.BindOption("atomic-notify-empty", &atomicNotifyEmptyEnabled, &optAtomicNotifyEmpty),
			optimization.BindOption("atomic-static-bounds", &atomicStaticBoundsEnabled, &optAtomicStaticBounds),
		)
	}
	if buildfeatures.BulkMemoryOperations != 0 {
		specs = append(specs,
			optimization.BindOption("bulk-fill-vector", &bulkFillVectorEnabled, &optBulkFillVector),
			optimization.BindOption("bulk-copy-vector", &bulkCopyVectorEnabled, &optBulkCopyVector),
			optimization.BindOption("bulk-memory-loops", &bulkMemoryLoopsEnabled, &optBulkMemoryLoops),
			optimization.BindOption("bulk-fill-copy-forward", &bulkFillCopyForwardEnabled, &optBulkFillCopyForward),
		)
	}
	if buildfeatures.TailCall != 0 {
		specs = append(specs,
			optimization.BindOption("tail-self-loop", &tailSelfLoopEnabled, &optTailSelfLoop),
			optimization.BindOption("tail-loop-inline", &tailLoopInlineEnabled, &optTailLoopInline),
		)
	}
	if buildfeatures.GC != 0 {
		deadGCNewEnabled = os.Getenv("WAGO_AMD64_NO_DEAD_GC_NEW") != "1"
		nativeGCStructAllocEnabled = os.Getenv("WAGO_AMD64_NO_GC_NATIVE_ALLOC") != "1"
		nativeGCArrayScratchEnabled = os.Getenv("WAGO_AMD64_NO_GC_ARRAY_SCRATCH") != "1"
		nativeGCStructInlineEnabled = os.Getenv("WAGO_AMD64_NO_GC_STRUCT_INLINE") != "1"
		nativeGCStructDirectSlotsEnabled = os.Getenv("WAGO_AMD64_NO_GC_STRUCT_DIRECT_SLOTS") != "1"
		gcSharedStubsEnabled = os.Getenv("WAGO_AMD64_NO_GC_SHARED_STUBS") != "1"
		gcResolveReuseEnabled = os.Getenv("WAGO_AMD64_NO_GC_RESOLVE_REUSE") != "1"
		gcInlineCastsEnabled = os.Getenv("WAGO_AMD64_NO_GC_CAST_INLINE") != "1"
		gcPrivateSinkDropEnabled = os.Getenv("WAGO_AMD64_NO_GC_SINK_STORE_ELISION") != "1"
		specs = append(specs,
			optimization.BindOption("dead-gc-new", &deadGCNewEnabled, &optDeadGCNew),
			optimization.BindOption("gc-native-alloc", &nativeGCStructAllocEnabled, &optGCNativeAlloc),
		)
	}
	if buildfeatures.ReferenceTypes != 0 {
		specs = append(specs,
			optimization.BindOption("direct-reference-abi", &directReferenceABIEnabled, &optDirectReferenceABI),
		)
	}
	return optimization.NewBindings("amd64", specs...)
}

var (
	optSharedMemoryBase        optimization.Option
	optMemoryMinimumBounds     = optimizationBindings.Option("memory-min-bounds")
	optAtomicNotifyEmpty       optimization.Option
	optAtomicStaticBounds      optimization.Option
	optAtomicZeroWait          optimization.Option
	optBoundsFacts             = optimizationBindings.Option("bounds-facts")
	optStraightLineBlock       = optimizationBindings.Option("straight-line-block")
	optByteReduceLoop          = optimizationBindings.Option("byte-reduce-loop")
	optBulkCopyVector          optimization.Option
	optBulkFillVector          optimization.Option
	optBulkMemoryLoops         optimization.Option
	optBulkFillCopyForward     optimization.Option
	optSIMDStoreLoadAddress    optimization.Option
	optSIMDLoadExtract         optimization.Option
	optSIMDSuperopt            optimization.Option
	optSIMDReductionBorrow     optimization.Option
	optPreparedDirectEntry     = optimizationBindings.Option("prepared-direct-entry")
	optPreparedBoundedEntry    = optimizationBindings.Option("prepared-bounded-entry")
	optDirectGoHostImport      = optimizationBindings.Option("direct-go-host-import")
	optNativeLeafHost          = optimizationBindings.Option("native-leaf-host")
	optWideLoopIntConst        = optimizationBindings.Option("wide-loop-int-const")
	optCompactLoopAlign32      = optimizationBindings.Option("compact-loop-align32")
	optCountedLoopLatch        = optimizationBindings.Option("counted-loop-latch")
	optScalarLoopUnroll        = optimizationBindings.Option("scalar-loop-unroll")
	optLoopLocalParams         = optimizationBindings.Option("loop-local-params")
	optLinearSumLoop           = optimizationBindings.Option("linear-sum-loop")
	optCallFreeLoopColdExit    = optimizationBindings.Option("callfree-loop-cold-exit")
	optLoopRegState            = optimizationBindings.Option("callfree-loop-reg-state")
	optIntervalRegionPins      = optimizationBindings.Option("interval-region-pins")
	optIntervalControl         = optimizationBindings.Option("interval-control")
	optIntervalNextUse         = optimizationBindings.Option("interval-next-use")
	optIntervalScratchLease    = optimizationBindings.Option("interval-scratch-lease")
	optIntervalR8Lease         = optimizationBindings.Option("interval-r8-lease")
	optIntervalI64Weight       = optimizationBindings.Option("interval-i64-weight")
	optMemSizeRegionalLease    = optimizationBindings.Option("memsize-regional-lease")
	optModuleGlobalRegionLease = optimizationBindings.Option("module-global-regional-lease")
	optMagicDiv                = optimizationBindings.Option("magic-div")
	optBorrowedDivRem          = optimizationBindings.Option("borrowed-div-rem")
	optSharedTrapBody          = optimizationBindings.Option("shared-trap-body")
	optSharedAdapters          = optimizationBindings.Option("shared-adapters")
	optSTFlags                 = optimizationBindings.Option("st-flags")
	optStore8Flags             = optimizationBindings.Option("store8-flags")
	optRegMerge                = optimizationBindings.Option("reg-merge")
	optTeeSink                 = optimizationBindings.Option("tee-sink")
	optUnarySink               = optimizationBindings.Option("unary-sink")
	optBranchFold              = optimizationBindings.Option("branch-fold")
	optEntryArgPins            = optimizationBindings.Option("entry-arg-pins")
	optExtendedFPPins          = optimizationBindings.Option("ext-fp-pins")
	optTreeOrder               = optimizationBindings.Option("tree-order")
	optAssocTree               = optimizationBindings.Option("assoc-tree")
	optBMI2Rorx                = optimizationBindings.Option("bmi2-rorx")
	optAVX512Ternary           optimization.Option
	optAVX512VRotate           optimization.Option
	optWideLocalPins           = optimizationBindings.Option("wide-local-pins")
	optDirectIntBranchMerge    = optimizationBindings.Option("direct-int-branch-merge")
	optFloatFrameMem           = optimizationBindings.Option("float-frame-mem")
	optFloatStoreBorrow        = optimizationBindings.Option("float-store-borrow")
	optMemoryCompareImmediate  = optimizationBindings.Option("memory-compare-immediate")
	optVEXFloatMem             = optimizationBindings.Option("vex-float-mem")
	optMultiBoundsCert         = optimizationBindings.Option("multi-bounds-cert")
	optAddrZExtElim            = optimizationBindings.Option("addr-zext-elim")
	optCanonicalI32            = optimizationBindings.Option("canonical-i32")
	optValueFacts              = optimizationBindings.Option("value-facts")
	optImmutableTable          = optimizationBindings.Option("immutable-table")
	optImmutableTableType      = optimizationBindings.Option("immutable-table-type")
	optInlineCallFree          = optimizationBindings.Option("inline-callfree")
	optStoreForward            = optimizationBindings.Option("store-forward")
	optFrameElide              = optimizationBindings.Option("frame-elide")
	optCompactI32Frame         = optimizationBindings.Option("compact-i32-frame")
	optLocalSlotOrder          = optimizationBindings.Option("local-slot-order")
	optCommuteSelfUpdate       = optimizationBindings.Option("commute-self-update")
	optCommuteFixedSelfUpdate  = optimizationBindings.Option("commute-fixed-self-update")
	optI64Mask32               = optimizationBindings.Option("i64-mask32")
	optAccumulatorImmediate    = optimizationBindings.Option("accumulator-immediate")
	optDeadGCNew               optimization.Option
	optGCNativeAlloc           optimization.Option
	optV128ConstCache          = optimizationBindings.Option("v128-const-cache")
	optV128Pins                optimization.Option
	optRegABI                  = optimizationBindings.Option("reg-abi")
	optTailSelfLoop            optimization.Option
	optTailLoopInline          optimization.Option
	optDirectReferenceABI      optimization.Option
	optInline                  = optimizationBindings.Option("inline")
	optTupleIfSelect           = optimizationBindings.Option("tuple-if-select")
	optPureIntegerIfSelect     = optimizationBindings.Option("pure-integer-if-select")
	optLinearIntegerTree       = optimizationBindings.Option("linear-integer-tree")
	optStackFence              = optimizationBindings.Option("stack-fence")
	optStackReg                = optimizationBindings.Option("stack-reg")
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
