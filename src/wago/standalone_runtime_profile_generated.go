// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone && wago_standalone_no_plugins && wago_standalone_generated

package wago

import "github.com/wago-org/wago/internal/buildfeatures"

const standaloneBareImage = true
const directNativeTailPlansForImage = buildfeatures.TailCall != 0 && buildfeatures.StandaloneDirectNativeTailPlans
const indirectNativeTailPlansForImage = buildfeatures.TailCall != 0 && buildfeatures.StandaloneIndirectNativeTailPlans
const referenceNativeTailPlansForImage = buildfeatures.TailCall != 0 && buildfeatures.StandaloneReferenceNativeTailPlans
const dynamicNativeTailForImage = indirectNativeTailPlansForImage || referenceNativeTailPlansForImage
const rv64NativeTailMetadataForImage = buildfeatures.TailCall != 0 && buildfeatures.StandaloneRV64NativeTailMetadata
const nativeTailPlansForImage = directNativeTailPlansForImage || dynamicNativeTailForImage

// The launcher owns a single module, so target directories are needed only
// when its own code contains a native tail plan.
const nativeTailForImage = nativeTailPlansForImage
const legacyNativeTailReferenceCarriersForImage = nativeTailPlansForImage && referenceSignaturesBuilt && buildfeatures.StandaloneLegacyNativeTailReferenceCarriers
const functionCodeForImage = buildfeatures.StandaloneFunctionCode
const retainedNativeContextForImage = buildfeatures.StandaloneRetainedNativeContext

const jobMemoryForImage = buildfeatures.StandaloneJobMemory

// Memory policy can add a directory even when the baked footprint is empty.
const arenaForImage = buildfeatures.StandaloneArenaBytes != 0 || memoriesForImage
const staticNativeTailForImage = directNativeTailPlansForImage && !dynamicNativeTailForImage

// The native image generator defines these constants from the linked module.
const hostFunctionImportsBuilt = buildfeatures.StandaloneFunctionImports
const guestGlobalStoreForImage = buildfeatures.StandaloneGuestGlobalStore
const nameSectionForImage = buildfeatures.StandaloneNameSection
const syncHostControlForImage = buildfeatures.StandaloneSyncHostControl
const rv64NativeOwnerScopeForImage = buildfeatures.StandaloneRV64NativeOwnerScope
const rv64CallableTableRowsForImage = buildfeatures.StandaloneRV64CallableTableRows
const functionReexportsForImage = buildfeatures.StandaloneFunctionReexports

// The launcher validates both fixed and dynamically selected exports against
// the CLI scalar signature before invoking them.
const exportReferenceInvocationForImage = false
const invocationTypesForImage = uint16(buildfeatures.StandaloneInvocationTypes)
const maxFixedDirectArgsForImage = min(4, buildfeatures.StandaloneMaxFixedDirectArgs)
const fixedIntI32ArgsForImage = uint8(buildfeatures.StandaloneFixedIntI32Args)
const fixedIntI64ArgsForImage = uint8(buildfeatures.StandaloneFixedIntI64Args)
const fixedIntResultsForImage = uint8(buildfeatures.StandaloneFixedIntResults)
const pairIntNarrowArgsForImage = uint8(buildfeatures.StandalonePairIntNarrowArgs)
const pairIntWideArgsForImage = uint8(buildfeatures.StandalonePairIntWideArgs)
const pairIntNarrowResultsForImage = uint8(buildfeatures.StandalonePairIntNarrowResults)
const pairIntWideResultsForImage = uint8(buildfeatures.StandalonePairIntWideResults)
const wideIntNarrowArgsForImage = uint8(buildfeatures.StandaloneWideIntNarrowArgs)
const wideIntWideArgsForImage = uint8(buildfeatures.StandaloneWideIntWideArgs)
const wideIntNarrowResultsForImage = uint8(buildfeatures.StandaloneWideIntNarrowResults)
const wideIntWideResultsForImage = uint8(buildfeatures.StandaloneWideIntWideResults)
const floatFourNarrowArgsForImage = uint8(buildfeatures.StandaloneFloatFourNarrowArgs)
const floatFourWideArgsForImage = uint8(buildfeatures.StandaloneFloatFourWideArgs)
const floatFourNarrowResultsForImage = uint8(buildfeatures.StandaloneFloatFourNarrowResults)
const floatFourWideResultsForImage = uint8(buildfeatures.StandaloneFloatFourWideResults)
const floatOctNarrowArgsForImage = uint8(buildfeatures.StandaloneFloatOctNarrowArgs)
const floatOctWideArgsForImage = uint8(buildfeatures.StandaloneFloatOctWideArgs)
const floatOctNarrowResultsForImage = uint8(buildfeatures.StandaloneFloatOctNarrowResults)
const floatOctWideResultsForImage = uint8(buildfeatures.StandaloneFloatOctWideResults)
const mixedNarrowArgsForImage = uint8(buildfeatures.StandaloneMixedNarrowArgs)
const mixedWideArgsForImage = uint8(buildfeatures.StandaloneMixedWideArgs)
const mixedNarrowResultsForImage = uint8(buildfeatures.StandaloneMixedNarrowResults)
const mixedWideResultsForImage = uint8(buildfeatures.StandaloneMixedWideResults)
const mixedResultFormsForImage = uint8(buildfeatures.StandaloneMixedResultForms)
const mixedGPArgsForImage = uint8(buildfeatures.StandaloneMixedGPArgs)
const mixedFPArgsForImage = uint8(buildfeatures.StandaloneMixedFPArgs)
const mixedGPResultsForImage = uint8(buildfeatures.StandaloneMixedGPResults)
const mixedFPResultsForImage = uint8(buildfeatures.StandaloneMixedFPResults)
const directIntFixedForImage = preparedDirectIntSupported && buildfeatures.StandaloneDirectIntFixed
const directIntBoundedFullForImage = directIntFixedForImage && buildfeatures.StandaloneDirectIntBoundedFull
const directIntBoundedLightForImage = directIntFixedForImage && buildfeatures.StandaloneDirectIntBoundedLight
const directIntUnboundedFullForImage = directIntFixedForImage && buildfeatures.StandaloneDirectIntUnboundedFull
const directIntUnboundedLightForImage = directIntFixedForImage && buildfeatures.StandaloneDirectIntUnboundedLight
const preparedZeroArgNarrowResultForImage = buildfeatures.StandalonePreparedZeroArgNarrowResult
const preparedZeroArgWideResultForImage = buildfeatures.StandalonePreparedZeroArgWideResult
const preparedIntOneArgForImage = directFixedOrPairForImage && buildfeatures.StandalonePreparedIntOneArg
const cachedIntOneArgForImage = directIntFixedForImage && buildfeatures.StandaloneCachedIntOneArg
const cachedBoundedI32OneArgForImage = directIntBoundedForImage && buildfeatures.StandaloneCachedBoundedI32OneArg
const preparedUnboundedLightI32OneArgForImage = preparedEarlyI32LightSupported && directIntUnboundedLightForImage && buildfeatures.StandalonePreparedUnboundedLightI32OneArg
const directPairResultsForImage = buildfeatures.StandaloneDirectPairResults
const directWideFiveForImage = preparedDirectWideSupported && buildfeatures.StandaloneDirectIntWide
const directOctResultsForImage = preparedDirectWideSupported && buildfeatures.StandaloneDirectIntOct
const directFloatFourForImage = preparedDirectFloatSupported && buildfeatures.StandaloneDirectFloatFour
const directFloatOctForImage = preparedDirectFloatSupported && buildfeatures.StandaloneDirectFloatOct
const directMixedForImage = preparedDirectFloatSupported && buildfeatures.StandaloneDirectMixed
const structuralCallKeysForImage = buildfeatures.StandaloneStructuralCallKeys
const funcRefContextHeaderForImage = buildfeatures.StandaloneFuncRefContextHeader
const typedCallKeysForImage = buildfeatures.StandaloneTypedCallKeys
const referenceTypeDescriptorsForImage = buildfeatures.StandaloneReferenceTypeDescriptors
const typeLinksForImage = buildfeatures.StandaloneTypeLinks
const detailedGCTypeDescsForImage = buildfeatures.StandaloneDetailedGCTypeDescs
const aggregateTypesForImage = buildfeatures.StandaloneAggregateTypes

const localStartForImage = buildfeatures.StandaloneLocalStart
const importedStartForImage = buildfeatures.StandaloneImportedStart
const startFunctionForImage = localStartForImage || importedStartForImage
const guardedGrowthForImage = buildfeatures.StandaloneGuardedGrowth
const guardedMemoryForImage = buildfeatures.StandaloneGuardedMemory
const sharedMemoryRuntimeBuilt = buildfeatures.Threads != 0 && buildfeatures.StandaloneSharedMemory
const moduleImportsBuilt = buildfeatures.StandaloneModuleImports
const memoriesForImage = buildfeatures.StandaloneMemories

// An import-free launcher has one instance. Managed function ownership can
// also use the dependency graph to account for native collector roots.
// Fixed memory bindings alone can only retain owners that already exist.
// Keep their ordinary importer counts and physical roots without graph receipts.
const crossInstanceDependenciesForImage = hostFunctionImportsBuilt || tableImportsForImage || globalImportsForImage ||
	tagImportsForImage || managedFunctionConstructionForImage || memoryImportsForImage && referenceStoreForImage

const memoryImportsForImage = buildfeatures.StandaloneMemoryImports
const exceptionHandlingForImage = buildfeatures.ExceptionHandling != 0 && buildfeatures.StandaloneExceptionHandling
const exceptionTagsForImage = buildfeatures.ExceptionHandling != 0 && buildfeatures.StandaloneExceptionTags
const ownedExceptionReferencesForImage = buildfeatures.ExceptionHandling != 0 && buildfeatures.StandaloneOwnedExceptionReferences
const tagImportsForImage = exceptionTagsForImage && buildfeatures.StandaloneTagImports
const funcrefStorageImportsBuilt = buildfeatures.StandaloneFuncrefStorageImports
const atomicNotifyRuntimeBuilt = buildfeatures.Threads != 0 && buildfeatures.StandaloneAtomicNotify
const atomicWait32RuntimeBuilt = buildfeatures.Threads != 0 && buildfeatures.StandaloneAtomicWait32
const atomicWait64RuntimeBuilt = buildfeatures.Threads != 0 && buildfeatures.StandaloneAtomicWait64
const atomicBlockingWaitRuntimeBuilt = atomicWait32RuntimeBuilt || atomicWait64RuntimeBuilt
const indexedAtomicHelperForImage = buildfeatures.StandaloneAtomicIndexedMemory
const atomicMemory64ForImage = buildfeatures.StandaloneAtomicMemory64
const atomicNotifyPendingForImage = atomicBlockingWaitRuntimeBuilt && buildfeatures.StandaloneAtomicNotifyPending
const atomicGrowthRuntimeBuilt = buildfeatures.Threads != 0 && buildfeatures.StandaloneSharedGrowthHelpers
const atomicWaitRuntimeBuilt = atomicNotifyRuntimeBuilt || atomicBlockingWaitRuntimeBuilt || atomicGrowthRuntimeBuilt
const memoryWaitQueuesForImage = atomicBlockingWaitRuntimeBuilt
const collectorRuntimeForImage = buildfeatures.StandaloneNativeCollector
const gcStructHelpersForImage = wasmGCBuilt && buildfeatures.StandaloneGCStructHelpers
const gcArrayHelpersForImage = wasmGCBuilt && buildfeatures.StandaloneGCArrayHelpers
const gcHelperMaskForImage = buildfeatures.StandaloneGCHelperMask
const gcExternConversionForImage = wasmGCBuilt && buildfeatures.StandaloneStagedGCExternConversion
const gcCompactExternDispatchForImage = wasmGCBuilt && rv64CollectorBuilt && buildfeatures.StandaloneCompactExternDispatch
const compactExternTablesForImage = rv64CollectorBuilt && buildfeatures.StandaloneCompactExternTables
const compactExternElementsForImage = rv64CollectorBuilt && buildfeatures.StandaloneCompactExternElements
const compactExternConstructionForImage = rv64CollectorBuilt && nativeCollectorFrameRootsBuilt && buildfeatures.StandaloneCompactExternConstruction
const managedFunctionConstructionForImage = rv64CollectorBuilt && nativeCollectorFrameRootsBuilt && buildfeatures.StandaloneManagedFunctionConstruction
const managedFunctionGraphForImage = buildfeatures.StandaloneManagedFunctionGraph
const rv64RootedReceiptsForImage = rv64RootedReceiptsBuilt && collectorRuntimeForImage && buildfeatures.StandaloneRootedReceipts
const referenceStoreForImage = buildfeatures.StandaloneReferenceStore
const multiMemoryRuntimeBuilt = buildfeatures.MultiMemory != 0 && buildfeatures.StandaloneMultiMemory
const activeDataForImage = buildfeatures.StandaloneActiveData
const core1DataPreflightForImage = buildfeatures.StandaloneCore1DataPreflight
const core1ElementPreflightForImage = buildfeatures.StandaloneCore1ElementPreflight
const tablesForImage = buildfeatures.StandaloneTables
const rv64TableCapacitiesForImage = tablesForImage && buildfeatures.StandaloneRV64TableCapacities
const multiTableForImage = buildfeatures.StandaloneMultiTable
const legacyTable32InstructionsForImage = multiTableForImage && buildfeatures.StandaloneLegacyTable32Instructions
const stateOnlyTable32ForImage = multiTableForImage && buildfeatures.StandaloneStateOnlyTable32
const table64ForImage = buildfeatures.Table64 != 0 && buildfeatures.StandaloneTable64
const legacyTable64ForImage = table64ForImage && buildfeatures.StandaloneLegacyTable64
const tableImportsForImage = buildfeatures.StandaloneTableImports
const tableInitializersForImage = tablesForImage && buildfeatures.StandaloneTableInitializers
const globalsForImage = buildfeatures.StandaloneGlobals
const rv64MutableFuncrefGlobalsForImage = buildfeatures.StandaloneRV64MutableFuncrefGlobals
const rv64LegacyGCProductsForImage = wasmGCBuilt && buildfeatures.StandaloneRV64LegacyGCProducts
const i31ExecutionForImage = buildfeatures.GC != 0 && buildfeatures.StandaloneI31Execution
const rv64LegacyI31ProductMaskForImage = buildfeatures.StandaloneRV64LegacyI31ProductMask
const rv64LegacyI31ProductsForImage = i31ExecutionForImage && rv64LegacyI31ProductMaskForImage != 0
const managedAggregateHelpersForImage = buildfeatures.StandaloneManagedAggregateHelpers
const rv64ReferenceFunctionsForImage = buildfeatures.StandaloneRV64ReferenceFunctions
const rv64ReferenceCallsForImage = buildfeatures.StandaloneRV64ReferenceCalls
const globalImportsForImage = buildfeatures.StandaloneGlobalImports
const activeElementsForImage = buildfeatures.StandaloneActiveElements
const passiveElementsForImage = buildfeatures.StandalonePassiveElements
const elementGlobalsForImage = buildfeatures.StandaloneElementGlobals
const elementExpressionsForImage = buildfeatures.StandaloneElementExpressions
const elementRepeatsForImage = buildfeatures.StandaloneElementRepeats
const elementFuncRefForImage = buildfeatures.StandaloneElementTypes&(1<<ValFuncRef) != 0
const elementExternRefForImage = buildfeatures.StandaloneElementTypes&(1<<ValExternRef) != 0
const elementExnRefForImage = buildfeatures.StandaloneElementTypes&(1<<ValExnRef) != 0
const elementAnyRefForImage = buildfeatures.StandaloneElementTypes&(1<<ValAnyRef) != 0
const elementI31RefForImage = buildfeatures.StandaloneElementTypes&(1<<ValI31Ref) != 0
const passiveDataForImage = buildfeatures.StandalonePassiveData
const globalInitExpressionsForImage = buildfeatures.StandaloneGlobalInitExpressions
const segmentGlobalOffsetsForImage = buildfeatures.StandaloneSegmentGlobalOffsets
const segmentOffsetExpressionsForImage = buildfeatures.StandaloneSegmentOffsetExpressions
