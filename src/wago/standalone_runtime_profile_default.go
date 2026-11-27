// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone || !wago_standalone_no_plugins || !wago_standalone_generated

package wago

import "github.com/wago-org/wago/internal/buildfeatures"

const standaloneBareImage = false
const nativeTailForImage = buildfeatures.TailCall != 0
const dynamicNativeTailForImage = nativeTailForImage
const functionCodeForImage = true
const retainedNativeContextForImage = true
const jobMemoryForImage = true
const arenaForImage = true
const hostFunctionImportsBuilt = true
const guestGlobalStoreForImage = true
const nameSectionForImage = true
const syncHostControlForImage = true
const functionReexportsForImage = true
const exportReferenceInvocationForImage = referenceSignaturesBuilt
const invocationTypesForImage = ^uint16(0)
const maxFixedDirectArgsForImage = 4
const fixedIntI32ArgsForImage = uint8(0xf)
const fixedIntI64ArgsForImage = uint8(0xf)
const fixedIntResultsForImage = fixedIntVoidResult | fixedIntI32Result | fixedIntI64Result
const pairIntNarrowArgsForImage = uint8(0xff)
const pairIntWideArgsForImage = uint8(0xff)
const pairIntNarrowResultsForImage = uint8(0xff)
const pairIntWideResultsForImage = uint8(0xff)
const wideIntNarrowArgsForImage = uint8(0xff)
const wideIntWideArgsForImage = uint8(0xff)
const wideIntNarrowResultsForImage = uint8(0xff)
const wideIntWideResultsForImage = uint8(0xff)
const floatFourNarrowArgsForImage = uint8(0xff)
const floatFourWideArgsForImage = uint8(0xff)
const floatFourNarrowResultsForImage = uint8(0xff)
const floatFourWideResultsForImage = uint8(0xff)
const floatOctNarrowArgsForImage = uint8(0xff)
const floatOctWideArgsForImage = uint8(0xff)
const floatOctNarrowResultsForImage = uint8(0xff)
const floatOctWideResultsForImage = uint8(0xff)
const mixedNarrowArgsForImage = uint8(0xff)
const mixedWideArgsForImage = uint8(0xff)
const mixedNarrowResultsForImage = uint8(0xff)
const mixedWideResultsForImage = uint8(0xff)
const mixedResultFormsForImage = mixedVoidResult | mixedSingleResult | mixedMultipleResults
const mixedGPArgsForImage = uint8(0xf)
const mixedFPArgsForImage = uint8(0xf)
const mixedGPResultsForImage = uint8(0xf)
const mixedFPResultsForImage = uint8(0xf)
const directIntFixedForImage = true
const directIntBoundedFullForImage = true
const directIntBoundedLightForImage = true
const directIntUnboundedFullForImage = true
const directIntUnboundedLightForImage = true
const preparedZeroArgNarrowResultForImage = true
const preparedZeroArgWideResultForImage = true
const preparedIntOneArgForImage = true
const cachedIntOneArgForImage = true
const cachedBoundedI32OneArgForImage = true
const preparedUnboundedLightI32OneArgForImage = preparedEarlyI32LightSupported
const directPairResultsForImage = true
const directWideFiveForImage = preparedDirectWideSupported
const directOctResultsForImage = preparedDirectWideSupported
const directFloatFourForImage = preparedDirectFloatSupported
const directFloatOctForImage = preparedDirectFloatSupported
const directMixedForImage = preparedDirectFloatSupported
const structuralCallKeysForImage = true
const funcRefContextHeaderForImage = true
const referenceTypeDescriptorsForImage = true
const typeLinksForImage = true
const detailedGCTypeDescsForImage = true
const aggregateTypesForImage = true
const localStartForImage = true
const importedStartForImage = true
const startFunctionForImage = localStartForImage || importedStartForImage
const guardedGrowthForImage = true
const guardedMemoryForImage = true
const sharedMemoryRuntimeBuilt = buildfeatures.Threads != 0
const moduleImportsBuilt = true
const memoriesForImage = true
const crossInstanceDependenciesForImage = true
const memoryImportsForImage = true
const exceptionHandlingForImage = buildfeatures.ExceptionHandling != 0
const exceptionTagsForImage = buildfeatures.ExceptionHandling != 0
const tagImportsForImage = buildfeatures.ExceptionHandling != 0
const funcrefStorageImportsBuilt = true
const atomicNotifyRuntimeBuilt = buildfeatures.Threads != 0
const atomicWait32RuntimeBuilt = buildfeatures.Threads != 0
const atomicWait64RuntimeBuilt = buildfeatures.Threads != 0
const atomicBlockingWaitRuntimeBuilt = atomicWait32RuntimeBuilt || atomicWait64RuntimeBuilt
const indexedAtomicHelperForImage = true
const atomicMemory64ForImage = true
const atomicNotifyPendingForImage = true
const atomicGrowthRuntimeBuilt = buildfeatures.Threads != 0
const atomicWaitRuntimeBuilt = atomicNotifyRuntimeBuilt || atomicBlockingWaitRuntimeBuilt || atomicGrowthRuntimeBuilt
const memoryWaitQueuesForImage = true
const collectorRuntimeForImage = buildfeatures.StandaloneNativeCollector
const ownedExceptionReferencesForImage = buildfeatures.ExceptionHandling != 0
const gcStructHelpersForImage = wasmGCBuilt
const gcArrayHelpersForImage = wasmGCBuilt
const gcHelperMaskForImage = ^uint64(0)
const gcExternConversionForImage = wasmGCBuilt
const gcCompactExternDispatchForImage = wasmGCBuilt && rv64CollectorBuilt
const compactExternTablesForImage = rv64CollectorBuilt
const compactExternElementsForImage = rv64CollectorBuilt
const compactExternConstructionForImage = rv64CollectorBuilt && nativeCollectorFrameRootsBuilt
const managedFunctionConstructionForImage = rv64CollectorBuilt && nativeCollectorFrameRootsBuilt
const managedFunctionGraphForImage = true
const rv64LegacyGCProductsForImage = wasmGCBuilt
const i31ExecutionForImage = wasmGCBuilt
const managedAggregateHelpersForImage = managedStructAggregateHelpers | managedArrayAggregateHelpers
const referenceStoreForImage = true
const multiMemoryRuntimeBuilt = buildfeatures.MultiMemory != 0
const activeDataForImage = true
const core1DataPreflightForImage = true
const core1ElementPreflightForImage = true
const tablesForImage = true
const rv64TableCapacitiesForImage = true
const multiTableForImage = true
const legacyTable32InstructionsForImage = true
const table64ForImage = buildfeatures.Table64 != 0
const legacyTable64ForImage = table64ForImage
const tableImportsForImage = true
const tableInitializersForImage = true
const globalsForImage = true
const globalImportsForImage = true
const activeElementsForImage = true
const passiveElementsForImage = true
const elementGlobalsForImage = true
const elementExpressionsForImage = true
const elementRepeatsForImage = true
const elementFuncRefForImage = true
const elementExternRefForImage = true
const elementExnRefForImage = true
const elementAnyRefForImage = true
const elementI31RefForImage = true
const passiveDataForImage = true
const globalInitExpressionsForImage = true
const segmentGlobalOffsetsForImage = true
const segmentOffsetExpressionsForImage = true
