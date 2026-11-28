// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone

package wago

import (
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreplugins "github.com/wago-org/wago/src/core/plugins"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func compileWithFrontendFeaturesAndInstructions(cfg *RuntimeConfig, wasmBytes []byte, features frontend.Features, instructions pluginInstructions) (*Compiled, error) {
	railshotConfigureFrontendTable64(&features)
	if cfg.maxModuleBytes != 0 && uint64(len(wasmBytes)) > cfg.maxModuleBytes {
		return nil, &wruntime.ResourceLimitError{
			Resource:  "module bytes",
			Scope:     "compile",
			Requested: uint64(len(wasmBytes)),
			Limit:     cfg.maxModuleBytes,
		}
	}
	decodeFeatures := wasm.ValidationFeatures{MultiMemory: features.MultiMemory}
	source := wasmBytes
	var staged *wruntime.Arena
	if compilerInputStaging && len(wasmBytes) >= 128<<10 && len(wasmBytes) <= 1<<20 &&
		cfg.codeProfile == nil && !codegen.SourceChecks && !hasPluginInstructions(instructions) {
		staged, _ = wruntime.NewArena(len(wasmBytes))
		if staged != nil {
			defer staged.Close()
			source = staged.AllocNoZero(len(wasmBytes))
			source = source[:len(source):len(source)]
			copy(source, wasmBytes)
		}
	}
	var decodeScratch *wasm.CompilerDecodeScratch
	if retainCompilerDecodeScratch && goruntime.GOARCH == "arm64" &&
		cfg.codeProfile == nil && !codegen.SourceChecks && !hasPluginInstructions(instructions) {
		decodeScratch = borrowCompilerDecodeScratch()
		defer returnCompilerDecodeScratch(decodeScratch)
	}
	m, deferredSyntax, err := decodeCompilerSource(source, decodeFeatures, decodeScratch)
	if err != nil {
		return nil, wrapContextError("decode", err)
	}
	workers := functionWorkersForModule(m, cfg.functionWorkers)
	validationFeatures := wasm.ValidationFeatures{
		CompactImports:       features.CompactImports || features.MultiMemory,
		MultiMemory:          features.MultiMemory,
		ExtendedConstGlobals: features.ExtendedConstGlobals,
		GCConstExpr:          features.GCStructProducts || features.GCArrayProducts || features.GCI31Products,
	}
	var validationAnalysis wasm.ValidatedModuleAnalysis
	validationLimits := wasm.ValidationLimits{MaxFunctionLocals: cfg.maxFunctionLocals, MaxMemoriesPerModule: cfg.maxMemoriesPerModule}
	usePrivateGCView := wasmGCBuilt && goruntime.GOARCH == "amd64" && goruntime.GOOS == "linux" &&
		cfg.codeProfile == nil && !hasPluginInstructions(instructions) && len(m.BranchHints) == 0 &&
		os.Getenv("WAGO_AMD64_NO_GC_CARRIER_ELISION") != "1" && gcOptionMayContainCarrier(m)
	if usePrivateGCView {
		for _, custom := range m.Customs {
			if custom.Name == "metadata.code.branch_hint" {
				usePrivateGCView = false
				break
			}
		}
	}
	initialValidationErr := wasm.ValidateModuleWithAnalysis(m, validationFeatures, workers, validationLimits, &validationAnalysis)
	if err := initialValidationErr; err != nil {
		if deferredSyntax {
			if _, syntaxErr := wasm.DecodeModuleWithFeatures(wasmBytes, decodeFeatures); syntaxErr != nil {
				return nil, wrapContextError("decode", syntaxErr)
			}
		}
		// Proposal-aware decoding can expose a structurally unsupported module to
		// validation before the validator has complete proposal subtyping rules.
		// Compile must still fail clearly as unsupported rather than mislabeling a
		// valid proposal module as an ordinary core type error.
		requirements := analyzeModuleRequirements(m)
		narrowFrontendFeatures(&features, requirements.features)
		if unsupported := frontend.RejectUnsupportedWithFeatures(m, features); unsupported != nil && isUnsupportedProposalError(unsupported) {
			return nil, wrapContextError("compile", unsupported)
		}
		return nil, wrapContextError("validate", err)
	}
	requirements := analyzeModuleRequirementsWithValidation(m, &validationAnalysis)
	if buildfeatures.Custom {
		required := requirements.features
		if m.UsesCompactImports {
			required |= CoreFeatureCompactImports
		}
		if err := checkCompiledFeatures(required); err != nil {
			return nil, err
		}
	}
	if err := railshotAdmitSource(wasmBytes, m); err != nil {
		return nil, wrapContextError("compile", err)
	}
	exactI31CoreSource := buildfeatures.GC != 0 && railshotI31CoreSource(wasmBytes)
	exactGCRefEqSource := buildfeatures.GC != 0 && railshotGCRefEqSource(wasmBytes)
	exactGCRefTestTableSource := buildfeatures.GC != 0 && railshotGCRefTestTableSource(wasmBytes)
	exactGCRefTestConcreteSource := buildfeatures.GC != 0 && railshotGCRefTestConcreteSource(wasmBytes)
	exactGCRefTestAbstractSource := buildfeatures.GC != 0 && railshotGCRefTestAbstractSource(wasmBytes)
	exactGCExternSource := buildfeatures.GC != 0 && railshotGCExternSource(wasmBytes)
	exactGCRefCastAbstractSource := buildfeatures.GC != 0 && railshotGCRefCastAbstractSource(wasmBytes)
	exactGCRefCastConcreteSource := buildfeatures.GC != 0 && railshotGCRefCastConcreteSource(wasmBytes)
	exactGCBrOnCastAbstractSource := buildfeatures.GC != 0 && railshotGCBrOnCastAbstractSource(wasmBytes)
	exactGCBrOnCastFailAbstractSource := buildfeatures.GC != 0 && railshotGCBrOnCastFailAbstractSource(wasmBytes)
	exactGCBrOnCastConcreteSource := buildfeatures.GC != 0 && railshotGCBrOnCastConcreteSource(wasmBytes)
	exactGCBrOnCastFailConcreteSource := buildfeatures.GC != 0 && railshotGCBrOnCastFailConcreteSource(wasmBytes)
	exactGCBrOnCastNullabilitySource := buildfeatures.GC != 0 && railshotGCBrOnCastNullabilitySource(wasmBytes)
	exactGCBrOnCastFailNullabilitySource := buildfeatures.GC != 0 && railshotGCBrOnCastFailNullabilitySource(wasmBytes)
	gcTypeSubtypingDeclarationsSourceVariant := railshotGCTypeSubtypingDeclarationsSource(wasmBytes)
	gcTypeSubtypingRecursiveFunctionsSourceVariant := railshotGCTypeSubtypingRecursiveFunctionsSource(wasmBytes)
	gcTypeSubtypingRefFuncGlobalsSourceVariant := railshotGCTypeSubtypingRefFuncGlobalsSource(wasmBytes)
	exactGCTypeSubtypingRuntimeCallCastSource := buildfeatures.GC != 0 && railshotGCTypeSubtypingRuntimeCallCastSource(wasmBytes)
	exactGCTypeSubtypingRuntimeFinalityCallCastSource := buildfeatures.GC != 0 && railshotGCTypeSubtypingRuntimeFinalityCallCastSource(wasmBytes)
	exactGCTypeSubtypingRuntimeTypedTableCallSource := buildfeatures.GC != 0 && railshotGCTypeSubtypingRuntimeTypedTableCallSource(wasmBytes)
	gcTypeSubtypingRefTestSingleSourceVariant := railshotGCTypeSubtypingRefTestSingleSource(wasmBytes)
	gcTypeSubtypingLinkSourceVariant := railshotGCTypeSubtypingLinkSource(wasmBytes)
	gcTypeSubtypingFinalityLinkSourceVariant := railshotGCTypeSubtypingFinalityLinkSource(wasmBytes)
	gcTypeSubtypingStructLinkSourceVariant := railshotGCTypeSubtypingStructLinkSource(wasmBytes)
	gcTypeSubtypingStructProjectionLinkSourceVariant := railshotGCTypeSubtypingStructProjectionLinkSource(wasmBytes)
	gcTypeSubtypingStructMismatchLinkSourceVariant := railshotGCTypeSubtypingStructMismatchLinkSource(wasmBytes)
	gcTypeSubtypingIndependentStructLinkSourceVariant := railshotGCTypeSubtypingIndependentStructLinkSource(wasmBytes)
	gcTypeSubtypingExtendedProjectionLinkSourceVariant := railshotGCTypeSubtypingExtendedProjectionLinkSource(wasmBytes)
	gcTypeSubtypingDuplicateRecursiveLinkSourceVariant := railshotGCTypeSubtypingDuplicateRecursiveLinkSource(wasmBytes)
	gcTypeSubtypingExtendedRecursiveLinkSourceVariant := railshotGCTypeSubtypingExtendedRecursiveLinkSource(wasmBytes)
	gcTypeSubtypingCrossGroupMismatchLinkSourceVariant := railshotGCTypeSubtypingCrossGroupMismatchLinkSource(wasmBytes)
	gcTypeSubtypingTransitiveMismatchLinkSourceVariant := railshotGCTypeSubtypingTransitiveMismatchLinkSource(wasmBytes)
	exactGCTypeSubtypingNonFlatExportSource := buildfeatures.GC != 0 && railshotGCTypeSubtypingNonFlatExportSource(wasmBytes)
	exactGCRootedScalarSource := buildfeatures.GC != 0 && railshotGCRootedScalarSource(wasmBytes)
	exactGCStructGetSource := buildfeatures.GC != 0 && railshotGCStructGetSource(wasmBytes)
	exactGCStructMutationSource := buildfeatures.GC != 0 && railshotGCStructMutationSource(wasmBytes)
	exactGCStructNullSource := buildfeatures.GC != 0 && railshotGCStructNullSource(wasmBytes)
	exactGCStructPackedSource := buildfeatures.GC != 0 && railshotGCStructPackedSource(wasmBytes)
	exactGCStructBasicSource := buildfeatures.GC != 0 && railshotGCStructBasicSource(wasmBytes)
	exactGCStructNumericGlobalsSource := buildfeatures.GC != 0 && railshotGCStructNumericGlobalsSource(wasmBytes)
	exactGCArrayLocalSource := compiledArchitecture != 4 && railshotGCArrayLocalSource(wasmBytes)
	exactGCArrayUniformSource := compiledArchitecture != 4 && railshotGCArrayUniformSource(wasmBytes)
	exactGCArrayFixedLocalSource := compiledArchitecture != 4 && railshotGCArrayFixedLocalSource(wasmBytes)
	exactGCArrayFixedOfficialSource := compiledArchitecture != 4 && railshotGCArrayFixedOfficialSource(wasmBytes)
	exactGCArrayDefaultOfficialSource := compiledArchitecture != 4 && railshotGCArrayDefaultOfficialSource(wasmBytes)
	exactGCArrayPackedDataSource := compiledArchitecture != 4 && railshotGCArrayPackedDataSource(wasmBytes)
	exactGCArrayReferenceSource := compiledArchitecture != 4 && railshotGCArrayReferenceSource(wasmBytes)
	exactGCArrayInitElemSource := compiledArchitecture != 4 && railshotGCArrayInitElemSource(wasmBytes)
	exactGCArrayInitDataSource := compiledArchitecture != 4 && railshotGCArrayInitDataSource(wasmBytes)
	exactGCArrayFillSource := compiledArchitecture != 4 && railshotGCArrayFillSource(wasmBytes)
	exactGCArrayCopySource := compiledArchitecture != 4 && railshotGCArrayCopySource(wasmBytes)
	requiredByModule := requirements.features
	narrowFrontendFeatures(&features, requiredByModule)
	if buildfeatures.Threads != 0 && requiredByModule.IsEnabled(CoreFeatureThreads) {
		if err := validateThreadedExecutionBoundary(m, cfg.boundsChecks, &validationAnalysis); err != nil {
			return nil, wrapContextError("compile", err)
		}
	}
	var customInstructions map[uint32]coreplugins.Instruction
	if pluginRuntimeBuilt {
		customInstructions, err = resolveInstructionLowerings(m, instructions)
		if err != nil {
			return nil, err
		}
	}
	if buildfeatures.GC != 0 && !exactGCRootedScalarSource && len(customInstructions) != 0 {
		exactGCRootedScalarSource = railshotGCRootedScalarCustomModule(m, customInstructions)
	}
	atomicWaitHelpers := buildfeatures.Threads != 0 && (requirements.atomicWaitHelpers || threadedGrowthHelpers(m))
	structuralProduct := stagedStructuralTypeProduct(0)
	gcTypeSubtypingProduct := stagedGCTypeSubtypingProduct(0)
	gcStructProduct := stagedGCStructProduct(0)
	gcArrayProduct := stagedGCArrayProduct(0)
	gcI31Product := stagedGCI31Product(0)
	nullReferenceProduct := false
	metadataOnlyTypes := railshotTypeDeclarationsSource(m) || railshotPrimitiveFunctionGraphSource(m)
	// Validation already proves whether any GC feature is present. Ordinary
	// modules cannot contain i31 operations, so avoid the collector-free i31
	// classifier's repeated body walks for those modules.
	generalI31Source := buildfeatures.GC != 0 && (compiledArchitecture == 4 && railshotGeneralI31Module(m) || supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && compilerGCProductAnalysisNeeded(requiredByModule) && generalCollectorFreeI31Module(m))
	if buildfeatures.GC != 0 && features.StructuralTypeProducts && hasStructuralHeapTypes(m) && !metadataOnlyTypes && !exactGCRootedScalarSource {
		// Structural execution is shape-proven; historical suite hashes no longer
		// define the product boundary now that Core 3 support is complete.
		if product, shapeErr := stagedStructuralTypeProductShape(m); shapeErr == nil {
			if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) {
				return nil, fmt.Errorf("compile: unsupported collector-free structural product staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
			}
			structuralProduct = product
		}
	}
	if buildfeatures.GC != 0 && features.GCTypeSubtypingProducts && requiredByModule.IsEnabled(CoreFeatureGC) && !metadataOnlyTypes && !exactGCRootedScalarSource {
		// Core 3 type-subtyping execution is no longer tied to hashes of the
		// historical spec fixtures. The structural classifier remains deliberately
		// strict because it selects the collector-free function-identity lowering,
		// but any module with that proven shape receives the same semantics.
		if product, shapeErr := stagedGCTypeSubtypingProductShape(m); shapeErr == nil {
			if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !(product == stagedGCTypeSubtypingDeclarations && gcTypeSubtypingDeclarationsSourceVariant != 0 || product == stagedGCTypeSubtypingRecursiveFunctions && gcTypeSubtypingRecursiveFunctionsSourceVariant != 0 || product == stagedGCTypeSubtypingRefFuncGlobals && gcTypeSubtypingRefFuncGlobalsSourceVariant != 0 || product == stagedGCTypeSubtypingRuntimeCallCast && exactGCTypeSubtypingRuntimeCallCastSource || product == stagedGCTypeSubtypingRuntimeFinalityCallCast && exactGCTypeSubtypingRuntimeFinalityCallCastSource || product == stagedGCTypeSubtypingRuntimeTypedTableCall && exactGCTypeSubtypingRuntimeTypedTableCallSource || (product == stagedGCTypeSubtypingRefTestSingle || product == stagedGCTypeSubtypingRefTestMulti || product == stagedGCTypeSubtypingRefTestDirectionFalse) && gcTypeSubtypingRefTestSingleSourceVariant != 0 || (product == stagedGCTypeSubtypingLinkProvider || product == stagedGCTypeSubtypingLinkConsumer) && gcTypeSubtypingLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingFinalityLinkProvider || product == stagedGCTypeSubtypingFinalityLinkConsumer) && gcTypeSubtypingFinalityLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingStructLinkProvider || product == stagedGCTypeSubtypingStructLinkConsumer) && gcTypeSubtypingStructLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingStructProjectionLinkProvider || product == stagedGCTypeSubtypingStructProjectionLinkConsumer) && gcTypeSubtypingStructProjectionLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingStructMismatchLinkProvider || product == stagedGCTypeSubtypingStructMismatchLinkConsumer) && gcTypeSubtypingStructMismatchLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingIndependentStructLinkProvider || product == stagedGCTypeSubtypingIndependentStructLinkConsumer) && gcTypeSubtypingIndependentStructLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingExtendedProjectionLinkProvider || product == stagedGCTypeSubtypingExtendedProjectionLinkConsumer) && gcTypeSubtypingExtendedProjectionLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingDuplicateRecursiveLinkProvider || product == stagedGCTypeSubtypingDuplicateRecursiveLinkConsumer) && gcTypeSubtypingDuplicateRecursiveLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingExtendedRecursiveLinkProvider || product == stagedGCTypeSubtypingExtendedRecursiveLinkConsumer) && gcTypeSubtypingExtendedRecursiveLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingCrossGroupMismatchLinkProvider || product == stagedGCTypeSubtypingCrossGroupMismatchLinkConsumer) && gcTypeSubtypingCrossGroupMismatchLinkSourceVariant != 0 || (product == stagedGCTypeSubtypingTransitiveMismatchLinkProvider || product == stagedGCTypeSubtypingTransitiveMismatchLinkConsumer) && gcTypeSubtypingTransitiveMismatchLinkSourceVariant != 0 || product == stagedGCTypeSubtypingNonFlatExport && exactGCTypeSubtypingNonFlatExportSource) {
				return nil, fmt.Errorf("compile: unsupported gc/type-subtyping product staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
			}
			gcTypeSubtypingProduct = product
		}
	}
	if buildfeatures.GC != 0 && features.GCStructProducts && !metadataOnlyTypes && gcProductAnalysisNeeded(requiredByModule) && gcTypeSubtypingProduct == 0 {
		product, ok := stagedGCStructExecutionProduct(wasmBytes)
		if ok && exactGCRootedScalarSource && (product == stagedGCStructDeclarations || product == stagedGCStructBindings) {
			// A declaration-only source is carried by the complete rooted graph;
			// it has no legacy helper image to select or authenticate.
			product, ok = 0, false
		}
		exactStructSource := exactGCRefEqSource || exactGCRefTestTableSource || exactGCRefTestConcreteSource || exactGCRefTestAbstractSource || exactGCExternSource || exactGCRefCastAbstractSource || exactGCRefCastConcreteSource || exactGCBrOnCastAbstractSource || exactGCBrOnCastFailAbstractSource || exactGCBrOnCastConcreteSource || exactGCBrOnCastFailConcreteSource || exactGCBrOnCastNullabilitySource || exactGCBrOnCastFailNullabilitySource || exactGCStructGetSource || exactGCStructMutationSource || exactGCStructNullSource || exactGCStructPackedSource || exactGCStructBasicSource || exactGCStructNumericGlobalsSource
		if ok && exactGCRootedScalarSource && !exactStructSource && moduleUsesGenericGCStructHelpers(m) {
			// General lowering and retained metadata must use the same helper ABI.
			// A legacy product without its source witness cannot select old state.
			product = stagedGCStructGeneric
		}
		if ok && exactGCRootedScalarSource && product == stagedGCStructNamedGets && moduleUsesGenericGCStructHelpers(m) {
			product = stagedGCStructGeneric
		}
		// Validation and initializer requirements already prove whether a GC
		// instruction can occur; avoid rescanning ordinary function bodies.
		if !ok && requiredByModule.IsEnabled(CoreFeatureGC) && moduleUsesGenericGCStructHelpers(m) {
			product, ok = stagedGCStructGeneric, true
		}
		if ok {
			if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !(product == stagedGCStructGeneric && exactGCRootedScalarSource) && !(product == stagedGCStructRefEq && exactGCRefEqSource) && !(product == stagedGCStructRefTestTable && exactGCRefTestTableSource) && !(product == stagedGCStructRefTestConcrete && exactGCRefTestConcreteSource) && !(product == stagedGCStructRefTestAbstract && exactGCRefTestAbstractSource) && !(product == stagedGCStructExtern && exactGCExternSource) && !(product == stagedGCStructRefCastAbstract && exactGCRefCastAbstractSource) && !(product == stagedGCStructRefCastConcrete && exactGCRefCastConcreteSource) && !(product == stagedGCStructBrOnCastAbstract && exactGCBrOnCastAbstractSource) && !(product == stagedGCStructBrOnCastFailAbstract && exactGCBrOnCastFailAbstractSource) && !(product == stagedGCStructBrOnCastConcrete && exactGCBrOnCastConcreteSource) && !(product == stagedGCStructBrOnCastFailConcrete && exactGCBrOnCastFailConcreteSource) && !(product == stagedGCStructBrOnCastNullability && exactGCBrOnCastNullabilitySource) && !(product == stagedGCStructBrOnCastFailNullability && exactGCBrOnCastFailNullabilitySource) && !(product == stagedGCStructNumericLocal && (exactGCStructGetSource || exactGCStructMutationSource)) && !(product == stagedGCStructNullDereference && exactGCStructNullSource) && !(product == stagedGCStructPacked && exactGCStructPackedSource) && !(product == stagedGCStructBasic && exactGCStructBasicSource) && !(product == stagedGCStructNumericGlobals && exactGCStructNumericGlobalsSource) {
				return nil, fmt.Errorf("compile: unsupported collector-backed struct product staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
			}
			gcStructProduct = product
		}
	}
	// The exact RV64 basic checkpoint is a backend admission product even when
	// the caller enabled GC through the narrower i31 configuration surface.
	// Its wire and decoded witnesses are independently rechecked by railshot.
	if exactGCStructBasicSource {
		gcStructProduct = stagedGCStructBasic
	}
	if exactGCStructNumericGlobalsSource {
		gcStructProduct = stagedGCStructNumericGlobals
	}
	if buildfeatures.GC != 0 && features.GCArrayProducts && !metadataOnlyTypes && gcProductAnalysisNeeded(requiredByModule) && gcStructProduct != stagedGCStructRefTestAbstract && gcStructProduct != stagedGCStructExtern && gcStructProduct != stagedGCStructRefEq && gcStructProduct != stagedGCStructRefCastAbstract && gcStructProduct != stagedGCStructBrOnCastAbstract && gcStructProduct != stagedGCStructBrOnCastFailAbstract {
		product, ok := stagedGCArrayExecutionProduct(wasmBytes)
		if !ok {
			product, ok = stagedGCArrayOpcodeProduct(m)
		}
		// Opcode classification can select an older exact array profile even
		// for a different module. Fresh rooted admission proves the generic
		// helper ABI and frame maps for that module; retain exact profiles only
		// when their independent source proof is present.
		exactArraySource := exactGCArrayLocalSource || exactGCArrayUniformSource || exactGCArrayFixedLocalSource || exactGCArrayFixedOfficialSource || exactGCArrayDefaultOfficialSource || exactGCArrayPackedDataSource || exactGCArrayReferenceSource || exactGCArrayInitElemSource || exactGCArrayInitDataSource || exactGCArrayFillSource || exactGCArrayCopySource
		if ok && !exactArraySource && exactGCRootedScalarSource {
			// Array declarations alone need metadata, not an execution helper.
			// Include allocating initializers in the opcode proof.
			if _, usesArrayHelpers := stagedGCArrayOpcodeProduct(m); usesArrayHelpers {
				product = stagedGCArrayProductGeneric
			} else {
				product, ok = 0, false
			}
		}
		if ok {
			if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !(product == stagedGCArrayProductGeneric && exactGCRootedScalarSource) && !(product == stagedGCArrayProductNumericLocal && exactGCArrayLocalSource) && !(product == stagedGCArrayProductNumericUniform && exactGCArrayUniformSource) && !(product == stagedGCArrayProductNumericFixedLocal && exactGCArrayFixedLocalSource) && !(product == stagedGCArrayProductNumericFixed && exactGCArrayFixedOfficialSource) && !(product == stagedGCArrayProductNumericDefault && exactGCArrayDefaultOfficialSource) && !(product == stagedGCArrayProductPackedData && exactGCArrayPackedDataSource) && !(product == stagedGCArrayProductReferenceElements && exactGCArrayReferenceSource) && !(product == stagedGCArrayProductInitElem && exactGCArrayInitElemSource) && !(product == stagedGCArrayProductInitData && exactGCArrayInitDataSource) && !(product == stagedGCArrayProductBulkFill && exactGCArrayFillSource) && !(product == stagedGCArrayProductBulkCopy && exactGCArrayCopySource) {
				return nil, fmt.Errorf("compile: unsupported collector-backed array product staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
			}
			gcArrayProduct = product
		}
	}
	if buildfeatures.GC != 0 && features.GCI31Products && gcProductAnalysisNeeded(requiredByModule) && gcStructProduct != stagedGCStructRefTestAbstract && gcStructProduct != stagedGCStructExtern && gcStructProduct != stagedGCStructRefEq && gcStructProduct != stagedGCStructRefCastAbstract && gcStructProduct != stagedGCStructRefCastConcrete && gcStructProduct != stagedGCStructBrOnCastAbstract && gcStructProduct != stagedGCStructBrOnCastFailAbstract {
		if product, ok := stagedGCI31ExecutionProduct(wasmBytes); ok {
			if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !exactI31CoreSource {
				return nil, fmt.Errorf("compile: unsupported i31 product staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
			}
			gcI31Product = product
		}
	}
	if buildfeatures.GC != 0 && features.GCI31Products && requiredByModule.IsEnabled(CoreFeatureGC) && gcI31Product == 0 && gcTypeSubtypingProduct == 0 && (!moduleHasGCHeapType(m) || generalI31Source) && !metadataOnlyTypes && !railshotPrimitiveFunctionGraphSource(m) && moduleUsesI31Operations(m) {
		// Immediate i31 execution is complete and does not require a collector.
		// Prefer the table/global initializer product when its structural proof
		// succeeds; otherwise use the general non-allocating i31 lowering.
		if initErr := validateStagedI31TableInitializer(m); initErr == nil {
			gcI31Product = stagedGCI31ProductTableGlobalInitializer
		} else {
			gcI31Product = stagedGCI31ProductCore
		}
	}
	if buildfeatures.GC != 0 && features.GCI31Products && requiredByModule.IsEnabled(CoreFeatureGC) && exactI31CoreSource && gcI31Product == 0 {
		gcI31Product = stagedGCI31ProductCore
	}
	if buildfeatures.GC != 0 && features.GCStructProducts && gcProductAnalysisNeeded(requiredByModule) && !gcStructProduct.requiresExternConversion() && moduleUsesGCExternConversion(m) && !exactI31CoreSource && !exactGCRootedScalarSource {
		// Conversion identity is an orthogonal runtime obligation. The extern
		// product uses the same complete struct/array helpers while additionally
		// provisioning the bounded anyref/externref identity bridge.
		gcStructProduct = stagedGCStructExtern
	}
	if buildfeatures.GC != 0 && features.GCStructProducts && !metadataOnlyTypes && requiredByModule.IsEnabled(CoreFeatureGC) && moduleHasGCHeapType(m) && gcStructProduct == 0 && structuralProduct == 0 && gcTypeSubtypingProduct == 0 && (gcI31Product == 0 || moduleUsesGenericGCStructHelpers(m)) && !exactGCRefTestTableSource && !exactGCRefTestConcreteSource && !exactGCRefTestAbstractSource && !exactGCExternSource && !exactGCRefCastAbstractSource && !exactGCRefCastConcreteSource && !exactGCBrOnCastAbstractSource && !exactGCBrOnCastFailAbstractSource && !exactGCBrOnCastConcreteSource && !exactGCBrOnCastFailConcreteSource && !exactGCBrOnCastNullabilitySource && !exactGCBrOnCastFailNullabilitySource && !exactGCStructMutationSource && !exactGCStructNullSource && !exactGCStructPackedSource && !exactGCStructBasicSource && !exactGCStructNumericGlobalsSource && !exactGCArrayLocalSource && !exactGCArrayUniformSource && !exactGCArrayFixedLocalSource && !exactGCArrayFixedOfficialSource && !exactGCArrayDefaultOfficialSource && !exactGCArrayPackedDataSource && !exactGCArrayReferenceSource && !exactGCArrayInitElemSource && !exactGCArrayInitDataSource && !exactGCArrayFillSource && !exactGCArrayCopySource {
		// Generic GC lowering is the complete Core 3 fallback for modules that do
		// not match a narrower collector-free or specialized product. Product
		// recognizers select optimizations, not semantic admission boundaries.
		// The bounded RV64 source witness keeps struct and array helper families
		// independent. An array-only module must not gain the generic struct
		// product merely because the complete backends use it as an umbrella.
		if compiledArchitecture != 4 || !exactGCRootedScalarSource || moduleUsesGenericGCStructHelpers(m) {
			gcStructProduct = stagedGCStructGeneric
		}
	}
	if wasmGCBuilt && goruntime.GOARCH == "arm64" && !gcStructProduct.requiresHelpers() && requirements.arm64GCRefTestHelper {
		// ARM64 lowers collector-reference ref.test through the generic checked
		// helper. Provision the collector and helper dispatch even when a narrower
		// metadata-only product recognized the module.
		gcStructProduct = stagedGCStructGeneric
	}
	if referenceSignaturesBuilt && features.NullReferenceProducts && !railshotExceptionHandlingSource(m) {
		if _, shapeErr := stagedNullReferenceProductShape(m); shapeErr == nil {
			nullReferenceProduct = true
			if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) {
				return nil, fmt.Errorf("compile: unsupported null-reference product staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
			}
		}
	}
	if buildfeatures.MultiMemory != 0 && features.MultiMemory && m.MemCount() > 1 {
		if !supportsMultiMemoryBackend(goruntime.GOOS, goruntime.GOARCH) {
			return nil, fmt.Errorf("compile: unsupported memory multi-memory staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
		}
	}
	if buildfeatures.TailCall != 0 && features.TailCalls && requiredByModule.IsEnabled(CoreFeatureTailCall) && !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !railshotNativeTailSource() && railshotStagedTailFeatures(cfg) == 0 {
		return nil, fmt.Errorf("compile: unsupported instruction tail-call staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	if buildfeatures.ExceptionHandling != 0 && features.ExceptionHandling {
		if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !railshotExceptionHandlingSource(m) {
			return nil, fmt.Errorf("compile: unsupported exception handling staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
		}
	}
	usesMemory64 := false
	for i := uint32(0); i < uint32(m.MemCount()); i++ {
		if mt, ok := m.MemoryType(i); ok && mt.Limits.Addr64 {
			usesMemory64 = true
		}
	}
	usesTable64 := false
	for i := uint32(0); i < uint32(m.TableCount()); i++ {
		if tt, ok := m.TableType(i); ok && tt.Limits.Addr64 {
			usesTable64 = true
		}
	}
	if buildfeatures.Table64 != 0 && features.Table64 && usesTable64 {
		if err := validateCompileTable64(m); err != nil {
			return nil, err
		}
	}
	if buildfeatures.Memory64 != 0 && features.Memory64 && usesMemory64 {
		if !supportsMemory64Backend(goruntime.GOOS, goruntime.GOARCH) {
			return nil, fmt.Errorf("compile: unsupported memory memory64 staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
		}
		for i := uint32(0); i < uint32(m.MemCount()); i++ {
			_, ok := m.MemoryType(i)
			if !ok {
				return nil, fmt.Errorf("compile: staged memory64 memory %d type is unavailable", i)
			}
		}
	}
	moduleFacts := requirements.moduleFacts
	if err := frontend.RejectUnsupportedWithFeaturesFactsAndValidation(m, features, moduleFacts, &validationAnalysis); err != nil {
		return nil, wrapContextError("compile", err)
	}
	var gcBodyViews map[uint32]codegen.GCBodyView
	if usePrivateGCView {
		optionView, viewErr := buildGCOptionBodyViewWithLocalLimit(m, cfg.maxFunctionLocals)
		if viewErr != nil {
			return nil, wrapContextError("private GC view", viewErr)
		}
		if optionView != nil {
			m, validationAnalysis, err = optionView.applyEdits(m, &validationAnalysis, validationFeatures, workers, validationLimits)
			if err != nil {
				return nil, wrapContextError("private GC validation", err)
			}
			requirements = analyzeModuleRequirementsWithValidation(m, &validationAnalysis)
			gcBodyViews = optionView.metadata()
		}
	}
	ownedExceptionReferences := buildfeatures.ExceptionHandling != 0 && railshotManagedExceptionReferences(m)
	gcMetadata, err := frontend.BuildCompileTypeMetadata(m, frontend.GCTypeMetadataOptions{OwnedExceptionReferences: ownedExceptionReferences})
	if err != nil {
		return nil, wrapContextError("gc descriptors", err)
	}
	gcDescs := gcMetadata.Descs
	var unsafeDirectTailImports []uint64
	if buildfeatures.TailCall != 0 && features.TailCalls {
		unsafeDirectTailImports, err = unsafeDirectTailImportBitset(m)
		if err != nil {
			return nil, wrapContextError("compile: direct imported tail metadata", err)
		}
	}
	// Architectures that always use the sync-host dispatcher can compile host
	// defaults up front; others defer returning imports until link time.
	boundsMode := effectiveCompileBoundsMode(cfg.boundsChecks, m)
	elide := boundsMode == BoundsChecksSignalsBased
	importedFuncs := m.ImportedFuncCount()
	dynamicBindings := make([]railshotImportBinding, importedFuncs)
	for i := range dynamicBindings {
		dynamicBindings[i] = railshotImportBinding{Dynamic: true, ImportIndex: uint32(i), EHTransfer: features.ExceptionHandling}
	}
	pressureAt, pressure := compileMemoryPressure(len(wasmBytes))
	genericGCExecution := gcStructProduct == stagedGCStructGeneric || gcArrayProduct == stagedGCArrayProductNewData || gcArrayProduct == stagedGCArrayProductNewElem || gcArrayProduct == stagedGCArrayProductGeneric
	collectorReferenceCallBoundary := nativeCollectorFrameRootsBuilt && moduleHasCollectorReferenceCallBoundary(m)
	gcAllocationSites := nativeCollectorFrameRootsBuilt && moduleHasGCAllocationSitesWithValidation(m, &validationAnalysis)
	// These exact source products retain their bounded persistent-root proof and
	// fixed artifact image. Generic collector calls use frame-root maps.
	boundedPersistentGCRoots := exactGCRefTestAbstractSource || exactGCExternSource || exactGCRefCastAbstractSource || exactGCBrOnCastAbstractSource || exactGCBrOnCastFailAbstractSource ||
		exactGCStructBasicSource || exactGCArrayFixedOfficialSource || exactGCArrayDefaultOfficialSource || exactGCArrayPackedDataSource
	opaqueExternFrames := nativeCollectorBuilt && compiledArchitecture == 4 && exactGCRootedScalarSource && len(m.Code) > 0 &&
		(requiredByModule.IsEnabled(CoreFeatureGC) || moduleHasOpaqueExternTable(m)) && moduleHasOpaqueExternSignatures(m) &&
		!genericGCExecution && !boundedPersistentGCRoots && !ownedExceptionReferences && !collectorReferenceCallBoundary && !moduleHasInternalCollectorReferenceFunctions(m)
	rootedReferenceDeclarations := opaqueExternFrames || compiledArchitecture == 4 && exactGCRootedScalarSource && (!metadataOnlyTypes || opaqueExternFrames) && gcStructProduct == 0 && gcArrayProduct == 0 && !gcAllocationSites && (opaqueExternFrames || moduleHasInternalCollectorReferenceFunctions(m) || collectorReferenceCallBoundary)
	exactNativeGCRoots := nativeCollectorFrameRootsBuilt && (genericGCExecution || collectorReferenceCallBoundary && !boundedPersistentGCRoots || ownedExceptionReferences || rootedReferenceDeclarations)
	functionEnvironmentFrames := nativeCollectorBuilt && compiledArchitecture == 4 && exactNativeGCRoots && !ownedExceptionReferences && !wasm.ModuleFunctionReferenceStorage(m) && wasm.ModuleFunctionReferenceCarriers(m)
	var gcFrameRootDiagnostic string
	var gcFrameRoots *shared.GCModuleFrameRootPlan
	if nativeCollectorFrameRootsBuilt {
		gcFrameRoots = newGCFrameRootPlanWithInstructions(m, exactNativeGCRoots, genericGCExecution, &gcFrameRootDiagnostic, &validationAnalysis, customInstructions)
	}
	indexedFunctionRefTest, indexedFunctionRefCast := requirements.indexedFuncRefTest, requirements.indexedFuncRefCast
	indexedFunctionRefOps := indexedFunctionRefTest || indexedFunctionRefCast
	foreignFunctionSubtypeHelpers := railshotFunctionSubtypeHelpers(m)
	dynamicFuncRefTest := foreignFunctionSubtypeHelpers || indexedFunctionRefTest && !gcTypeSubtypingProduct.usesRefTest() && !gcTypeSubtypingProduct.usesRuntimeFunctionIdentity()
	if compiledArchitecture == 4 && metadataOnlyTypes && !foreignFunctionSubtypeHelpers {
		// Collector-free RV64 function tests/casts compare canonical descriptor
		// keys directly. They need no collector type-ID array per instance.
		dynamicFuncRefTest = false
	}
	gcFunctionRefTest := gcTypeSubtypingProduct.usesRefTest() || gcTypeSubtypingProduct.usesRuntimeFunctionIdentity() || indexedFunctionRefOps || foreignFunctionSubtypeHelpers
	var gcCodeStats *railshotModuleStats
	if (compilerTelemetryEnabled && cfg.gcCodeTelemetry) || (codeProfileEnabled && cfg.codeProfile != nil) {
		gcCodeStats = new(railshotModuleStats)
	}
	syncHostSlots, err := moduleSyncHostSlotCapacity(m)
	if err != nil {
		return nil, wrapContextError("compile", err)
	}
	gcStructHelpers := gcStructProduct.requiresHelpers()
	gcArrayHelpers := gcArrayHelpersRequired(gcArrayProduct, gcStructProduct)
	usesGCHelpers := ownedExceptionReferences || rootedReferenceDeclarations || gcStructHelpers || gcArrayHelpers
	if wasmGCBuilt && usesGCHelpers {
		gcSlots, err := gcSyncHostSlotCapacity(gcMetadata.Descs)
		if err != nil {
			return nil, wrapContextError("compile", err)
		}
		if gcSlots > syncHostSlots {
			syncHostSlots = gcSlots
		}
		if goruntime.GOARCH == "riscv64" && gcArrayProduct == stagedGCArrayProductGeneric {
			syncHostSlots = max(syncHostSlots, rv64GenericArrayFixedSlots)
		}
	}
	compileOptions := railshotCompileOptions{BitCountFeatures: bitCountHostFeaturesSupported(), Workers: workers, DeferCodeMapping: true, Optimizations: cfg.optimizations, OptimizationSnapshot: cfg.optimizationSnapshot, OptimizationDeltas: cfg.optimizationDeltas, ElideBoundsChecks: elide, NoBoundsFacts: cfg.noDeferBounds, ImportBindings: dynamicBindings, SyncHostCalls: atomicWaitHelpers || ownedExceptionReferences || foreignFunctionSubtypeHelpers, SyncHostSlots: syncHostSlots, GCTypeSubtypingRefTest: gcFunctionRefTest, GCStructHelpers: gcStructHelpers, GCArrayHelpers: gcArrayHelpers, GCFrameRoots: gcFrameRoots, Interruptible: !wruntime.HostInterruptSupported(), MemoryPressureAt: pressureAt, MemoryPressure: pressure, CustomInstructions: customInstructions, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: gcMetadata.Descs, GCTypeLayouts: gcMetadata.Layouts}}, Stats: gcCodeStats, SourceMaps: codeProfileEnabled && cfg.codeProfile.IncludeSources(), UnwindMaps: codeProfileEnabled && cfg.codeProfile.IncludeUnwind(), Profile: codeProfileEnabled && cfg.codeProfile != nil}
	setCapturedOptimizationSelection(&compileOptions, cfg)
	compileOptions.Codegen.Module.GCBodyViews = gcBodyViews
	railshotConfigureResourceLimits(cfg, &compileOptions)
	railshotConfigureI31Core(exactI31CoreSource, &compileOptions)
	if wasmGCBuilt && compiledArchitecture == 4 {
		// Import compaction can change the structural witness. Match the final
		// module that the RV64 backend independently validates.
		generalI31Source = railshotGeneralI31Module(m)
	}
	railshotConfigureGeneralI31(generalI31Source, &compileOptions)
	railshotConfigureGCRefEq(exactGCRefEqSource, &compileOptions)
	railshotConfigureGCRefTestTable(exactGCRefTestTableSource, &compileOptions)
	railshotConfigureGCRefTestConcrete(exactGCRefTestConcreteSource, &compileOptions)
	railshotConfigureGCRefTestAbstract(exactGCRefTestAbstractSource, &compileOptions)
	railshotConfigureGCExtern(exactGCExternSource, &compileOptions)
	railshotConfigureGCRefCastAbstract(exactGCRefCastAbstractSource, &compileOptions)
	railshotConfigureGCRefCastConcrete(exactGCRefCastConcreteSource, &compileOptions)
	railshotConfigureGCBrOnCastAbstract(exactGCBrOnCastAbstractSource, &compileOptions)
	railshotConfigureGCBrOnCastFailAbstract(exactGCBrOnCastFailAbstractSource, &compileOptions)
	railshotConfigureGCBrOnCastConcrete(exactGCBrOnCastConcreteSource, &compileOptions)
	railshotConfigureGCBrOnCastFailConcrete(exactGCBrOnCastFailConcreteSource, &compileOptions)
	railshotConfigureGCBrOnCastNullability(exactGCBrOnCastNullabilitySource, &compileOptions)
	railshotConfigureGCBrOnCastFailNullability(exactGCBrOnCastFailNullabilitySource, &compileOptions)
	railshotConfigureGCTypeSubtypingDeclarations(gcTypeSubtypingDeclarationsSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingRecursiveFunctions(gcTypeSubtypingRecursiveFunctionsSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingRuntimeCallCast(exactGCTypeSubtypingRuntimeCallCastSource, &compileOptions)
	railshotConfigureGCStructGet(exactGCStructGetSource, &compileOptions)
	railshotConfigureGCStructMutation(exactGCStructMutationSource, &compileOptions)
	railshotConfigureGCStructNull(exactGCStructNullSource, &compileOptions)
	railshotConfigureGCStructPacked(exactGCStructPackedSource, &compileOptions)
	railshotConfigureGCStructBasic(exactGCStructBasicSource, &compileOptions)
	railshotConfigureGCStructNumericGlobals(exactGCStructNumericGlobalsSource, &compileOptions)
	railshotConfigureGCArrayLocal(exactGCArrayLocalSource, &compileOptions)
	railshotConfigureGCArrayUniform(exactGCArrayUniformSource, &compileOptions)
	railshotConfigureGCArrayFixedLocal(exactGCArrayFixedLocalSource, &compileOptions)
	railshotConfigureGCArrayFixedOfficial(exactGCArrayFixedOfficialSource, &compileOptions)
	railshotConfigureGCArrayDefaultOfficial(exactGCArrayDefaultOfficialSource, &compileOptions)
	railshotConfigureGCArrayPackedData(exactGCArrayPackedDataSource, &compileOptions)
	railshotConfigureGCArrayReference(exactGCArrayReferenceSource, &compileOptions)
	railshotConfigureGCArrayInitElem(exactGCArrayInitElemSource, &compileOptions)
	railshotConfigureGCArrayInitData(exactGCArrayInitDataSource, &compileOptions)
	railshotConfigureGCArrayFill(exactGCArrayFillSource, &compileOptions)
	railshotConfigureGCArrayCopy(exactGCArrayCopySource, &compileOptions)
	railshotConfigureNativeTail(requiredByModule, cfg, &compileOptions)
	railshotConfigureGCTypeSubtypingRuntimeFinalityCallCast(exactGCTypeSubtypingRuntimeFinalityCallCastSource, &compileOptions)
	railshotConfigureGCTypeSubtypingRuntimeTypedTableCall(exactGCTypeSubtypingRuntimeTypedTableCallSource, &compileOptions)
	railshotConfigureGCTypeSubtypingRefTestSingle(gcTypeSubtypingRefTestSingleSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingLink(gcTypeSubtypingLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingFinalityLink(gcTypeSubtypingFinalityLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingStructLink(gcTypeSubtypingStructLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingStructProjectionLink(gcTypeSubtypingStructProjectionLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingStructMismatchLink(gcTypeSubtypingStructMismatchLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingIndependentStructLink(gcTypeSubtypingIndependentStructLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingExtendedProjectionLink(gcTypeSubtypingExtendedProjectionLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingDuplicateRecursiveLink(gcTypeSubtypingDuplicateRecursiveLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingExtendedRecursiveLink(gcTypeSubtypingExtendedRecursiveLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingCrossGroupMismatchLink(gcTypeSubtypingCrossGroupMismatchLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingTransitiveMismatchLink(gcTypeSubtypingTransitiveMismatchLinkSourceVariant, &compileOptions)
	railshotConfigureGCTypeSubtypingNonFlatExport(exactGCTypeSubtypingNonFlatExportSource, &compileOptions)
	railshotConfigureGCTypeSubtypingRefFuncGlobals(gcTypeSubtypingRefFuncGlobalsSourceVariant, &compileOptions)
	var cm *railshotCompiledModule
	if codegen.SourceChecks {
		cm, err = railshotCompileValidatedModuleWith(m, compileOptions, &validationAnalysis, validationFeatures)
	} else {
		cm, err = railshotCompileModuleWith(m, compileOptions)
	}
	if err != nil {
		return nil, wrapContextError("compile", err)
	}
	keepCodeImage := false
	defer func() {
		if cm.CodeImage != nil && !keepCodeImage {
			_ = cm.CodeImage.Close()
		}
	}()
	if cfg.maxNativeCodeBytes != 0 && uint64(len(cm.Code)) > cfg.maxNativeCodeBytes {
		return nil, &wruntime.ResourceLimitError{
			Resource:  "native code bytes",
			Scope:     "compile",
			Requested: uint64(len(cm.Code)),
			Limit:     cfg.maxNativeCodeBytes,
		}
	}
	code, entry, internalEntry := cm.Code, cm.Entry, cm.InternalEntry
	if (goruntime.GOARCH == "arm64" || goruntime.GOARCH == "amd64" && cfg.optimizations["direct-go-host-import"]) && importedFuncs > 0 && len(customInstructions) == 0 && len(internalEntry) != 0 {
		internalEntry[0] = int(uint(internalEntry[0]) | goHostDispatchTagMask)
	}
	// Native Go leaf lowering needs no guest segment proof. Potentially
	// unbounded execution retains syscall scheduling; bounded entry separately
	// requires the whole-module proof below and the per-instance native binding.
	// This compile-only selection is a rollback policy.
	if goruntime.GOARCH != "riscv64" && wruntime.NativeScalarLeafSupported && cfg.optimizations["native-leaf-host"] && importedFuncs > 0 && len(customInstructions) == 0 && len(internalEntry) != 0 {
		internalEntry[0] = int(uint(internalEntry[0]) | nativeScalarLeafMask)
	}
	if goruntime.GOARCH != "riscv64" && cfg.optimizations["prepared-bounded-entry"] && len(customInstructions) == 0 && boundedModuleHostSegments(m) && len(internalEntry) != 0 {
		internalEntry[0] = int(uint(internalEntry[0]) | directHostSegmentsMask)
	}
	if goruntime.GOARCH != "riscv64" && wruntime.NativeScalarLeafSupported && cfg.optimizations["prepared-bounded-entry"] && len(customInstructions) == 0 && boundedModuleNativeHostBody(m) && len(internalEntry) != 0 {
		internalEntry[0] = int(uint(internalEntry[0]) | nativeScalarBoundedMask)
	}
	if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && cfg.optimizations["prepared-bounded-entry"] && len(customInstructions) == 0 && boundedIntegerModuleHostSegmentsFor(m, goruntime.GOARCH == "arm64") && len(internalEntry) != 0 {
		internalEntry[0] = int(uint(internalEntry[0]) | integerHostContextMask)
	}
	for i := range internalEntry {
		if i>>6 < len(cm.DirectPrepared) && cm.DirectPrepared[i>>6]&(uint64(1)<<uint(i&63)) != 0 {
			internalEntry[i] = markDirectPreparedEntry(internalEntry[i])
			// Imported native leaves need a binding proof in addition to the whole
			// module bound. Isolated entry admission supplies that proof; arbitrary
			// Go bindings cannot select any direct register entry for this module.
			if uint(internalEntry[0])&nativeScalarBoundedMask != 0 {
				internalEntry[i] = markDirectPreparedBoundedEntry(internalEntry[i])
			}
		}
		if i>>6 < len(cm.DirectPreparedLight) && cm.DirectPreparedLight[i>>6]&(uint64(1)<<uint(i&63)) != 0 {
			internalEntry[i] = markDirectPreparedLightEntry(internalEntry[i])
		}
		if i>>6 < len(cm.DirectPreparedBounded) && cm.DirectPreparedBounded[i>>6]&(uint64(1)<<uint(i&63)) != 0 {
			internalEntry[i] = markDirectPreparedBoundedEntry(internalEntry[i])
		}
	}
	if goruntime.GOOS == "darwin" && goruntime.GOARCH == "arm64" && boundsMode == BoundsChecksSignalsBased && validationAnalysis.ValidFor(m) {
		if _, multipleMemories := m.MemoryType(1); !multipleMemories {
			for i, growsMemory := range guardedMemoryGrowFunctions(m, &validationAnalysis) {
				if growsMemory {
					internalEntry[i] = markGuardedMemoryGrowEntry(internalEntry[i])
				}
			}
		}
	}

	typeConverter := newWasmTypeDescriptorConverter(m)
	types, err := typeConverter.typeDescriptors()
	if err != nil {
		return nil, wrapContextError("type metadata", err)
	}
	nativeGCABIVersion := uint32(0)
	if exactNativeGCRoots || gcStructHelpers || gcArrayHelpers {
		nativeGCABIVersion = gc.NativeABIVersion
	}
	c := newCompilerCompiled()
	c.codeCache.recordGCHelperUses(cm.GCHelperUses)
	c.codeCache.recordAtomicHelperUses(cm.AtomicHelperUses)
	c.code = code
	c.Entry = entry
	c.InternalEntry = internalEntry
	c.registerABIDisabled = goruntime.GOARCH == "riscv64" || !cfg.optimizations["reg-abi"]
	c.NumImports = importedFuncs
	c.Types = types
	c.Exports = map[string]int{}
	c.Names = m.NameSec
	c.GlobalExports = map[string]int{}
	c.hasTableExportMetadata = true
	c.boundsMode = boundsMode
	c.stagedTable64 = features.Table64 && usesTable64
	c.nativeTable64ABI = stagedNativeTable64ABI(m)
	c.independentInstances = cfg.independentInstances
	c.preparedIsolatedTables = cm.PreparedIsolatedTables
	if buildfeatures.BulkMemoryOperations != 0 && goruntime.GOOS == "linux" && goruntime.GOARCH == "amd64" && preparedBoundedWrapperSchedulerSupported && requiredByModule.IsEnabled(CoreFeatureBulkMemoryOperations) && cfg.optimizations["prepared-bounded-entry"] &&
		len(customInstructions) == 0 && !compileOptions.Interruptible && !compileOptions.Profile && !compileOptions.SourceMaps && !usesGCHelpers {
		c.boundedBulkPlans = boundedBulkArgumentPlans(m)
	}
	c.GCTypeDescs = gcDescs
	c.requiredFeatures = requiredByModule
	c.dynamicImports = importedFuncs > 0
	c.dynamicFuncrefEscape = referenceSignaturesBuilt && moduleDynamicFuncrefEscapeWithValidation(m, &validationAnalysis)
	c.customInstructions = customInstructions
	c.requiresNotifyPending = cm.AtomicHelperUses&shared.AtomicHelperUsesNotifyPending != 0
	c.requiresARM64LSE = cm.RequiresARM64LSE
	c.requiredAMD64Features = shared.AMD64Features(cm.RequiredAMD64Features)
	c.syncHostSlots = uint16(syncHostSlots)
	c.hasGCCodeTelemetry = compilerTelemetryEnabled && cfg.gcCodeTelemetry
	c.core1SegmentPreflight = !features.BulkMemory
	if cfg.optimizations["prepared-bounded-entry"] && len(customInstructions) == 0 && boundsMode == BoundsChecksExplicit {
		c.fixedSharedHostSegments = boundedFixedSharedHostSegments(m)
		c.fixedSharedHostInteger = (goruntime.GOARCH == "arm64" || goruntime.GOARCH == "amd64") && c.fixedSharedHostSegments && integerHostModuleRegistersFor(m, true)
	}
	c.codeCache.setNativeStackBytes(cfg.nativeStackBytes)
	if c.validateMemo != nil {
		c.validateMemo.memoryLimitPages = cfg.maxMemoryPages
		c.validateMemo.maxInstanceMetadataBytes = cfg.maxInstanceMetadataBytes
		c.validateMemo.snapshotLimit = cfg.maxCompiledMetadataBytes
	}
	if err := railshotStoreCompileMetadata(c, cm, cfg); err != nil {
		return nil, err
	}
	if cfg.maxNativeCodeBytes != 0 && railshotNativeCodeBytes(c) > cfg.maxNativeCodeBytes {
		return nil, &wruntime.ResourceLimitError{Resource: "native code bytes", Scope: "compile", Requested: railshotNativeCodeBytes(c), Limit: cfg.maxNativeCodeBytes}
	}
	c.memoryDir.exactExports = true
	c.memoryDir.staged = features.MultiMemory && (m.MemCount() > 1 || m.ImportedMemCount() > 0)
	c.memoryDir.stagedMemory64 = features.Memory64 && usesMemory64
	if compilerTelemetryEnabled && cfg.gcCodeTelemetry {
		c.gcCodeTelemetry = railshotGCNativeCodeTelemetry(gcCodeStats)
		c.gcCodeTelemetry.TotalBytes = uint64(len(code))
	}
	constExprCtx := &constExprCompileContext{module: m, types: c.Types, converter: typeConverter}
	if wasmGCBuilt && (gcStructProduct != 0 || gcArrayProduct != 0) {
		if err := c.compileGCInitializers(m, gcStructProduct, gcArrayProduct); err != nil {
			return nil, err
		}
	}

	if err := compileModuleMetadata(c, constExprCtx, features, &requirements, unsafeDirectTailImports, gcArrayProduct, gcTypeSubtypingProduct, indexedFunctionRefOps); err != nil {
		return nil, err
	}
	compiled := c
	if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && c.NumImports == 0 && cm.CodeImage == nil {
		// There is no non-Go resource to reclaim until the code is mapped.
		c.ensureCompileIndexes().pendingFinalizer = c
	} else {
		installCompilerCompiledFinalizer(c)
	}
	compiled.setPreparedGCInductions(cm.GCI31Inductions)
	compiled.codeCache.setNativeGCABIVersion(nativeGCABIVersion)
	if functionEnvironmentFrames {
		compiled.codeCache.gcMetadataFlags |= compiledGCMetadataFunctionFrames
	}
	if opaqueExternFrames {
		compiled.codeCache.gcMetadataFlags |= compiledGCMetadataOpaqueExternFrames
	}
	if cm.CodeImage != nil {
		mapping, base, err := cm.CodeImage.Take()
		if err != nil {
			return nil, wrapContextError("compile: transfer code image", err)
		}
		if image, ok := cm.CodeImage.(*wruntime.CodeBuffer); compilerCodeStaging && ok && image.IsStaging() {
			compiled.retainCodeStaging(mapping)
		} else {
			compiled.codeCache.mem = mapping
			compiled.codeCache.base = base
			railshotRecordMappedCodeBytes(compiled, len(compiled.code))
			compiled.codeCache.flags |= compiledCacheWritableCode
		}
	}
	keepCodeImage = true
	if guardPageBuilt && cfg.boundsChecks == BoundsChecksSignalsBased {
		compiled.codeCache.flags |= compiledCacheGuardMemory
	}
	validGCFrameRoots := nativeCollectorFrameRootsBuilt && validGCModuleFrameRootPlanKinds(gcFrameRoots, compiledArchitecture == 4 && (ownedExceptionReferences || functionEnvironmentFrames || genericGCExecution && wasm.ModuleFunctionReferenceStorage(m)), compiledArchitecture == 4)
	if nativeCollectorFrameRootsBuilt && (ownedExceptionReferences || compiledArchitecture == 4 && (genericGCExecution || collectorReferenceCallBoundary)) && len(m.Code) == 0 && gcFrameRoots != nil && gcFrameRoots.FunctionCount() == 0 {
		validGCFrameRoots = true // State-only owned values have no native frame or return PC.
	}
	if validGCFrameRoots && len(m.Code) != 0 && (gcAllocationSites || compiledArchitecture != 4 && genericGCExecution && !collectorReferenceCallBoundary) {
		hasAllocationSafepoint := false
		for function := 0; function < gcFrameRoots.FunctionCount(); function++ {
			plan := gcFrameRoots.Function(function)
			hasAllocationSafepoint = hasAllocationSafepoint || plan != nil && plan.SafepointCount() != 0
		}
		validGCFrameRoots = hasAllocationSafepoint
	}
	if exactNativeGCRoots && !validGCFrameRoots {
		diagnostic := "native backend did not produce complete exact root maps"
		if gcFrameRootDiagnostic != "" {
			diagnostic = gcFrameRootDiagnostic
		}
		compiled.setGCRootAdmissionFailure(diagnostic)
	}
	if validGCFrameRoots {
		compiled.setCompiledGCFrameRoots(gcFrameRoots, cm.GCDeadInductionLayouts)
	}
	compiled.setOwnedExceptionReferences(ownedExceptionReferences)
	// GC/typed-reference admission follows validated source requirements; product
	// classifiers below select representations and optimized paths. Exception
	// modules with declared tags are inferred from persisted tag metadata, while
	// tag-free catch_all/try_table modules need an explicit compile-only marker.
	compiled.codeCache.stagedFeatures |= requiredByModule & (CoreFeatureGC | CoreFeatureTypedFunctionReferences)
	if requiredByModule.IsEnabled(CoreFeatureExceptionHandling) && m.TagCount() == 0 {
		compiled.codeCache.stagedFeatures |= CoreFeatureExceptionHandling
	}
	if features.TypedFunctionReferences {
		compiled.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences
	}
	if buildfeatures.TailCall != 0 && features.TailCalls || features.TypedTailCalls {
		compiled.codeCache.stagedFeatures |= CoreFeatureTailCall
	}
	if nullReferenceProduct {
		compiled.codeCache.stagedFeatures |= CoreFeatureGC | CoreFeatureExceptionHandling
	}
	if structuralProduct != 0 {
		compiled.codeCache.stagedFeatures |= CoreFeatureGC
		compiled.codeCache.gcMetadataFlags |= compiledGCMetadataCollectorFreeStructural
	}
	if dynamicFuncRefTest {
		compiled.codeCache.flags |= compiledCacheDynamicFuncRefTest
	}
	if atomicWaitHelpers {
		compiled.codeCache.flags |= compiledCacheAtomicWaitHelpers
		if compiledArchitecture == 4 {
			compiled.needsFuncRefContextHeader = true
		}
	}
	if wasmGCBuilt {
		cachedI31Product := gcI31Product
		if cachedI31Product != 0 && railshotExceptionHandlingSource(m) {
			cachedI31Product = 0
		}
		compiled.codeCache.setGCProducts(compiledGCProducts{
			gcTypeSubtypingProduct: gcTypeSubtypingProduct,
			gcStructProduct:        gcStructProduct,
			gcArrayProduct:         gcArrayProduct,
			gcI31Product:           cachedI31Product,
		})
	}
	if codeProfileEnabled && cfg.codeProfile != nil {
		if err := compiled.installCodeProfile(cfg, wasmBytes, gcCodeStats); err != nil {
			return nil, joinPrimary(err, compiled.Close())
		}
	}
	if rv64RootedReceiptsBuilt && (genericGCExecution || collectorReferenceCallBoundary || ownedExceptionReferences || rootedReferenceDeclarations) && validGCFrameRoots {
		receipt := new(rv64RootedReceipt)
		if err := rv64PopulateSourceReceipt(receipt, compiled); err != nil {
			return nil, joinPrimary(fmt.Errorf("compile: %w", err), compiled.Close())
		}
		receipt.noCollectingBodies = true
		for function := 0; function < gcFrameRoots.FunctionCount(); function++ {
			if gcFrameRoots.Function(function) != nil {
				receipt.noCollectingBodies = false
				break
			}
		}
		compiled.validateMemo.setRootedReceipt(receipt)
	}
	installRV64StorageFreeProof(compiled, m)
	if err := installRV64SourceExecutionContract(compiled); err != nil {
		return nil, joinPrimary(fmt.Errorf("compile: %w", err), compiled.Close())
	}
	published, err := publishCompilerCompiled(compiled)
	if err != nil || staged == nil {
		return published, err
	}
	// The compiler snapshot already owns its copy. Rebase the compatibility
	// view to the caller's original source before closing the temporary mapping.
	stageBase := uintptr(unsafe.Pointer(unsafe.SliceData(source)))
	for i := range published.Data {
		data := &published.Data[i]
		if data.Bytes == nil {
			continue
		}
		if cap(data.Bytes) == 0 {
			// An empty slice at the staged buffer's end need not carry
			// its original address. Keep the caller's original capacity.
			data.Bytes = wasmBytes[len(source):len(source)]
			continue
		}
		offset := uintptr(unsafe.Pointer(unsafe.SliceData(data.Bytes))) - stageBase
		if offset > uintptr(len(source)) || uintptr(len(data.Bytes)) > uintptr(len(source))-offset {
			return nil, joinPrimary(errors.New("compile: invalid staged data range"), published.Close())
		}
		data.Bytes = wasmBytes[int(offset) : int(offset)+len(data.Bytes)]
	}
	return published, nil
}
