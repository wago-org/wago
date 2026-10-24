package wago

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"

	"errors"
	"fmt"
	"github.com/wago-org/wago/internal/functionworkers"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	goruntime "runtime"

	"crypto/sha256"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// DefaultArtifactLimits returns the limits used by Compiled.ReadFrom.
func DefaultArtifactLimits() ArtifactLimits {
	return ArtifactLimits{MaxCodeBytes: 1 << 30, MaxMetadataBytes: 256 << 20, MaxDecodedBytes: 256 << 20}
}

// Compile decodes, validates, and compiles a wasm module to native code.
//
// On success ownership of wasmBytes transfers to the returned Compiled. The
// caller must not mutate or reuse its backing array: decoded segment metadata
// may retain slices into that storage. This avoids an input-sized copy while the
// compiled artifact is live.
//
// It accepts both the current explicit form:
//
//	Compile(cfg, wasmBytes)
//
// and the original default-config shorthand:
//
//	Compile(wasmBytes)
//
// Pass nil as cfg, or omit it, to use NewRuntimeConfig.
func Compile(args ...any) (*Compiled, error) {
	cfg, wasmBytes, err := compileArgs(args)
	if err != nil {
		return nil, err
	}
	return compileWithConfig(cfg, wasmBytes)
}

func moduleDynamicFuncrefEscapeWithValidation(m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis) bool {
	if analysis.ValidFor(m) {
		return analysis.Flags()&wasm.ValidatedFuncDynamicReferenceCall != 0
	}
	return moduleDynamicFuncrefEscape(m)
}

func compileArgs(args []any) (*RuntimeConfig, []byte, error) {
	switch len(args) {
	case 1:
		wasmBytes, ok := args[0].([]byte)
		if !ok {
			return nil, nil, fmt.Errorf("wago: Compile expects []byte or (*RuntimeConfig, []byte), got %T", args[0])
		}
		return nil, wasmBytes, nil
	case 2:
		var cfg *RuntimeConfig
		if args[0] != nil {
			var ok bool
			cfg, ok = args[0].(*RuntimeConfig)
			if !ok {
				return nil, nil, fmt.Errorf("wago: Compile config must be *RuntimeConfig or nil, got %T", args[0])
			}
		}
		wasmBytes, ok := args[1].([]byte)
		if !ok {
			return nil, nil, fmt.Errorf("wago: Compile wasm bytes must be []byte, got %T", args[1])
		}
		return cfg, wasmBytes, nil
	default:
		return nil, nil, fmt.Errorf("wago: Compile expects []byte or (*RuntimeConfig, []byte), got %d arguments", len(args))
	}
}

// functionWorkersForModule resolves the configured policy once so validation
// and codegen use the same bounded worker count for a module.
func functionWorkersForModule(m *wasm.Module, policy int) int {
	totalBody := 0
	for i := range m.Code {
		totalBody += len(m.Code[i].BodyBytes)
	}
	return functionworkers.Resolve(policy, len(m.Code), totalBody)
}

func compileWithConfig(cfg *RuntimeConfig, wasmBytes []byte) (*Compiled, error) {
	return compileWithConfigAndInstructions(cfg, wasmBytes, nil)
}

func compileWithConfigAndInstructions(cfg *RuntimeConfig, wasmBytes []byte, instructions map[instructionKey]*registeredInstruction) (*Compiled, error) {
	if cfg == nil {
		cfg = NewRuntimeConfig()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	compiled, err := compileWithFrontendFeaturesAndInstructions(cfg, wasmBytes, cfg.frontendFeatures(), instructions)
	// TinyGo's conservative collector must not reclaim compiler inputs whose
	// derived slices and pointers remain in use below this public boundary.
	// Keep the complete owners live until decoding and code generation finish.
	goruntime.KeepAlive(wasmBytes)
	goruntime.KeepAlive(cfg)
	goruntime.KeepAlive(instructions)
	return compiled, err
}

func stagedNullReferenceProductShape(m *wasm.Module) (stagedNullReferenceProduct, error) {
	if m == nil || len(m.Imports) != 0 || m.Start != nil || m.TagCount() != 0 || m.MemCount() != 0 || m.TableCount() != 0 || len(m.Elements) != 0 || len(m.Data) != 0 {
		return 0, errors.New("null-reference product requires only local functions and immutable local globals")
	}
	if len(m.Types) == 10 && m.FuncCount() == 9 && len(m.Code) == 9 && len(m.Globals) == 18 && len(m.Exports) == 9 {
		if err := stagedBottomNullReferenceProductShape(m); err != nil {
			return 0, err
		}
		return stagedNullReferenceProductBottomGlobals, nil
	}
	if len(m.Types) != 6 || m.FuncCount() != 5 || len(m.Code) != 5 || len(m.Globals) != 5 || len(m.Exports) != 5 {
		return 0, errors.New("null-reference product is not one of the two exact pinned module shapes")
	}
	var base wasm.CompType
	if !m.ResolveTypeFunc(0, &base) || len(base.Params) != 0 || len(base.Results) != 0 {
		return 0, errors.New("first null-reference product type 0 must be () -> ()")
	}
	indexed := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	results := []wasm.ValType{
		wasm.RefVal(wasm.AbsRef(wasm.HeapAny)),
		wasm.FuncRef,
		wasm.RefVal(wasm.AbsRef(wasm.HeapExn)),
		wasm.ExternRef,
		indexed,
	}
	heaps := []int64{-18, -16, -23, -17, 0}
	exports := []string{"anyref", "funcref", "exnref", "externref", "ref"}
	for i := range results {
		ft, found := m.FuncSignature(uint32(i))
		if !found || len(ft.Params) != 0 || len(ft.Results) != 1 || !wasm.EqualValType(ft.Results[0], results[i]) {
			return 0, fmt.Errorf("first null-reference product function %d has an unexpected signature", i)
		}
		if len(m.Code[i].Locals.Runs) != 0 || !isExactRefNullBody(m.Code[i].BodyBytes, heaps[i]) {
			return 0, fmt.Errorf("first null-reference product function %d must return exactly one null", i)
		}
		g := &m.Globals[i]
		if g.Type.Mutable || !wasm.EqualValType(g.Type.Type, results[i]) || !isExactRefNullBody(g.Init.BodyBytes, heaps[i]) {
			return 0, fmt.Errorf("first null-reference product global %d must be an immutable exact null", i)
		}
	}
	for i, ex := range m.Exports {
		if ex.Name != exports[i] || ex.Index.Kind != wasm.ExternFunc || int(ex.Index.Index) != i {
			return 0, fmt.Errorf("first null-reference product export %d is outside the exact function export set", i)
		}
	}
	return stagedNullReferenceProductFirst, nil
}

func narrowFrontendFeatures(features *frontend.Features, requiredByModule CoreFeatures) {
	if !requiredByModule.IsEnabled(CoreFeatureTypedFunctionReferences) && !requiredByModule.IsEnabled(CoreFeatureGC) {
		features.TypedFunctionReferences = false
		features.TypedTailCalls = false
	}
	if !requiredByModule.IsEnabled(CoreFeatureTailCall) {
		features.TailCalls = false
		features.TypedTailCalls = false
	}
	if !requiredByModule.IsEnabled(CoreFeatureExceptionHandling) {
		features.ExceptionHandling = false
		features.ExceptionReferences = false
	}
}

func compileWithFrontendFeaturesAndInstructions(cfg *RuntimeConfig, wasmBytes []byte, features frontend.Features, instructions map[instructionKey]*registeredInstruction) (*Compiled, error) {
	if !sourceCompilationEnabled {
		return nil, wrapContextError("compile", errors.New("source compilation is unavailable in a precompiled runtime"))
	}
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
	m, deferredSyntax, err := decodeCompilerSource(wasmBytes, decodeFeatures)
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
	var hintFactory wasm.ValidationHintFactory
	if len(instructions) == 0 {
		hintFactory = railshotValidationHintFactory(cfg, workers)
	}
	if err := wasm.ValidateModuleWithHintObserver(m, validationFeatures, workers, wasm.ValidationLimits{
		MaxFunctionLocals:    cfg.maxFunctionLocals,
		MaxMemoriesPerModule: cfg.maxMemoriesPerModule,
	}, &validationAnalysis, hintFactory); err != nil {
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
	if err := railshotAdmitSource(wasmBytes, m); err != nil {
		return nil, wrapContextError("compile", err)
	}
	exactI31CoreSource := railshotI31CoreSource(wasmBytes)
	exactGCRefEqSource := railshotGCRefEqSource(wasmBytes)
	exactGCRefTestTableSource := railshotGCRefTestTableSource(wasmBytes)
	exactGCRefTestConcreteSource := railshotGCRefTestConcreteSource(wasmBytes)
	exactGCRefTestAbstractSource := railshotGCRefTestAbstractSource(wasmBytes)
	exactGCExternSource := railshotGCExternSource(wasmBytes)
	exactGCRefCastAbstractSource := railshotGCRefCastAbstractSource(wasmBytes)
	exactGCRefCastConcreteSource := railshotGCRefCastConcreteSource(wasmBytes)
	exactGCBrOnCastAbstractSource := railshotGCBrOnCastAbstractSource(wasmBytes)
	exactGCBrOnCastFailAbstractSource := railshotGCBrOnCastFailAbstractSource(wasmBytes)
	exactGCBrOnCastConcreteSource := railshotGCBrOnCastConcreteSource(wasmBytes)
	exactGCBrOnCastFailConcreteSource := railshotGCBrOnCastFailConcreteSource(wasmBytes)
	exactGCBrOnCastNullabilitySource := railshotGCBrOnCastNullabilitySource(wasmBytes)
	exactGCBrOnCastFailNullabilitySource := railshotGCBrOnCastFailNullabilitySource(wasmBytes)
	gcTypeSubtypingDeclarationsSourceVariant := railshotGCTypeSubtypingDeclarationsSource(wasmBytes)
	gcTypeSubtypingRecursiveFunctionsSourceVariant := railshotGCTypeSubtypingRecursiveFunctionsSource(wasmBytes)
	gcTypeSubtypingRefFuncGlobalsSourceVariant := railshotGCTypeSubtypingRefFuncGlobalsSource(wasmBytes)
	exactGCTypeSubtypingRuntimeCallCastSource := railshotGCTypeSubtypingRuntimeCallCastSource(wasmBytes)
	exactGCTypeSubtypingRuntimeFinalityCallCastSource := railshotGCTypeSubtypingRuntimeFinalityCallCastSource(wasmBytes)
	exactGCTypeSubtypingRuntimeTypedTableCallSource := railshotGCTypeSubtypingRuntimeTypedTableCallSource(wasmBytes)
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
	exactGCTypeSubtypingNonFlatExportSource := railshotGCTypeSubtypingNonFlatExportSource(wasmBytes)
	exactGCRootedScalarSource := railshotGCRootedScalarSource(wasmBytes)
	exactGCStructGetSource := railshotGCStructGetSource(wasmBytes)
	exactGCStructMutationSource := railshotGCStructMutationSource(wasmBytes)
	exactGCStructNullSource := railshotGCStructNullSource(wasmBytes)
	exactGCStructPackedSource := railshotGCStructPackedSource(wasmBytes)
	exactGCStructBasicSource := railshotGCStructBasicSource(wasmBytes)
	exactGCStructNumericGlobalsSource := railshotGCStructNumericGlobalsSource(wasmBytes)
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
	if requiredByModule.IsEnabled(CoreFeatureThreads) {
		if err := validateThreadedExecutionBoundary(m, cfg.boundsChecks, &validationAnalysis); err != nil {
			return nil, wrapContextError("compile", err)
		}
	}
	var functionIndex uint32
	for i := range m.Imports {
		imp := &m.Imports[i]
		if imp.Type.Kind != wasm.ExternFunc {
			continue
		}
		if ins := instructions[instructionKey{imp.Module, imp.Name}]; ins != nil {
			ft, ok := m.FuncSignature(functionIndex)
			if !ok {
				return nil, fmt.Errorf("compile: instruction import %q has no function signature", imp.Module+"."+imp.Name)
			}
			sig := FuncSig{Params: valTypesFromWasm(ft.Params), Results: valTypesFromWasm(ft.Results)}
			if err := validateInstructionSignature(imp.Module+"."+imp.Name, ins.spec, sig); err != nil {
				return nil, err
			}
		}
		functionIndex++
	}
	customInstructions := resolveInstructionLowerings(m, instructions)
	if !exactGCRootedScalarSource && len(customInstructions) != 0 {
		exactGCRootedScalarSource = railshotGCRootedScalarCustomModule(m, customInstructions)
	}
	atomicWaitHelpers := requirements.atomicWaitHelpers || threadedGrowthHelpers(m)
	structuralProduct := stagedStructuralTypeProduct(0)
	gcTypeSubtypingProduct := stagedGCTypeSubtypingProduct(0)
	gcStructProduct := stagedGCStructProduct(0)
	gcArrayProduct := stagedGCArrayProduct(0)
	gcI31Product := stagedGCI31Product(0)
	nullReferenceProduct := false
	metadataOnlyTypes := railshotTypeDeclarationsSource(m) || railshotPrimitiveFunctionGraphSource(m)
	// GC product scans cannot accept a module without GC; the override retains full attribution.
	generalI31Source := compilerGCProductAnalysisNeeded(requiredByModule) && (compiledArchitecture == 4 && railshotGeneralI31Module(m) || supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && generalCollectorFreeI31Module(m))
	if features.StructuralTypeProducts && hasStructuralHeapTypes(m) && !metadataOnlyTypes && !exactGCRootedScalarSource {
		// Structural execution is shape-proven; historical suite hashes no longer
		// define the product boundary now that Core 3 support is complete.
		if product, shapeErr := stagedStructuralTypeProductShape(m); shapeErr == nil {
			if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) {
				return nil, fmt.Errorf("compile: unsupported collector-free structural product staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
			}
			structuralProduct = product
		}
	}
	if features.GCTypeSubtypingProducts && requiredByModule.IsEnabled(CoreFeatureGC) && !metadataOnlyTypes && !exactGCRootedScalarSource {
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
	if features.GCStructProducts && !metadataOnlyTypes && gcProductAnalysisNeeded(requiredByModule) && gcTypeSubtypingProduct == 0 {
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
	if features.GCArrayProducts && !metadataOnlyTypes && gcProductAnalysisNeeded(requiredByModule) && gcStructProduct != stagedGCStructRefTestAbstract && gcStructProduct != stagedGCStructExtern && gcStructProduct != stagedGCStructRefEq && gcStructProduct != stagedGCStructRefCastAbstract && gcStructProduct != stagedGCStructBrOnCastAbstract && gcStructProduct != stagedGCStructBrOnCastFailAbstract {
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
	if features.GCI31Products && gcProductAnalysisNeeded(requiredByModule) && gcStructProduct != stagedGCStructRefTestAbstract && gcStructProduct != stagedGCStructExtern && gcStructProduct != stagedGCStructRefEq && gcStructProduct != stagedGCStructRefCastAbstract && gcStructProduct != stagedGCStructRefCastConcrete && gcStructProduct != stagedGCStructBrOnCastAbstract && gcStructProduct != stagedGCStructBrOnCastFailAbstract {
		if product, ok := stagedGCI31ExecutionProduct(wasmBytes); ok {
			if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !exactI31CoreSource {
				return nil, fmt.Errorf("compile: unsupported i31 product staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
			}
			gcI31Product = product
		}
	}
	if features.GCI31Products && requiredByModule.IsEnabled(CoreFeatureGC) && gcI31Product == 0 && gcTypeSubtypingProduct == 0 && (!moduleHasGCHeapType(m) || generalI31Source) && !metadataOnlyTypes && !railshotPrimitiveFunctionGraphSource(m) && moduleUsesI31Operations(m) {
		// Immediate i31 execution is complete and does not require a collector.
		// Prefer the table/global initializer product when its structural proof
		// succeeds; otherwise use the general non-allocating i31 lowering.
		if _, initErr := stagedGCI31TableInitializer(m); initErr == nil {
			gcI31Product = stagedGCI31ProductTableGlobalInitializer
		} else {
			gcI31Product = stagedGCI31ProductCore
		}
	}
	if features.GCI31Products && requiredByModule.IsEnabled(CoreFeatureGC) && exactI31CoreSource && gcI31Product == 0 {
		gcI31Product = stagedGCI31ProductCore
	}
	if features.GCStructProducts && gcProductAnalysisNeeded(requiredByModule) && !gcStructProduct.requiresExternConversion() && moduleUsesGCExternConversion(m) && !exactI31CoreSource && !exactGCRootedScalarSource {
		// Conversion identity is an orthogonal runtime obligation. The extern
		// product uses the same complete struct/array helpers while additionally
		// provisioning the bounded anyref/externref identity bridge.
		gcStructProduct = stagedGCStructExtern
	}
	if features.GCStructProducts && !metadataOnlyTypes && requiredByModule.IsEnabled(CoreFeatureGC) && moduleHasGCHeapType(m) && gcStructProduct == 0 && structuralProduct == 0 && gcTypeSubtypingProduct == 0 && (gcI31Product == 0 || moduleUsesGenericGCStructHelpers(m)) && !exactGCRefTestTableSource && !exactGCRefTestConcreteSource && !exactGCRefTestAbstractSource && !exactGCExternSource && !exactGCRefCastAbstractSource && !exactGCRefCastConcreteSource && !exactGCBrOnCastAbstractSource && !exactGCBrOnCastFailAbstractSource && !exactGCBrOnCastConcreteSource && !exactGCBrOnCastFailConcreteSource && !exactGCBrOnCastNullabilitySource && !exactGCBrOnCastFailNullabilitySource && !exactGCStructMutationSource && !exactGCStructNullSource && !exactGCStructPackedSource && !exactGCStructBasicSource && !exactGCStructNumericGlobalsSource && !exactGCArrayLocalSource && !exactGCArrayUniformSource && !exactGCArrayFixedLocalSource && !exactGCArrayFixedOfficialSource && !exactGCArrayDefaultOfficialSource && !exactGCArrayPackedDataSource && !exactGCArrayReferenceSource && !exactGCArrayInitElemSource && !exactGCArrayInitDataSource && !exactGCArrayFillSource && !exactGCArrayCopySource {
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
	if goruntime.GOARCH == "arm64" && !gcStructProduct.requiresHelpers() && requirements.arm64GCRefTestHelper {
		// ARM64 lowers collector-reference ref.test through the generic checked
		// helper. Provision the collector and helper dispatch even when a narrower
		// metadata-only product recognized the module.
		gcStructProduct = stagedGCStructGeneric
	}
	if features.NullReferenceProducts && !railshotExceptionHandlingSource(m) {
		if _, shapeErr := stagedNullReferenceProductShape(m); shapeErr == nil {
			nullReferenceProduct = true
			if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) {
				return nil, fmt.Errorf("compile: unsupported null-reference product staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
			}
		}
	}
	if features.MultiMemory && m.MemCount() > 1 {
		if !supportsMultiMemoryBackend(goruntime.GOOS, goruntime.GOARCH) {
			return nil, fmt.Errorf("compile: unsupported memory multi-memory staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
		}
	}
	if features.TailCalls && requiredByModule.IsEnabled(CoreFeatureTailCall) && !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !railshotNativeTailSource() && railshotStagedTailFeatures(cfg) == 0 {
		return nil, fmt.Errorf("compile: unsupported instruction tail-call staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	if features.ExceptionHandling {
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
	if features.Table64 && usesTable64 {
		if err := validateCompileTable64(m); err != nil {
			return nil, err
		}
	}
	if features.Memory64 && usesMemory64 {
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
	var gcMetadata frontend.GCTypeMetadata
	needsGCTypeMetadata := gcTypeMetadataNeeded(requiredByModule) || len(customInstructions) != 0
	if needsGCTypeMetadata {
		gcMetadata, err = frontend.BuildGCTypeMetadata(m)
		if err != nil {
			return nil, wrapContextError("gc descriptors", err)
		}
	}
	gcDescs := gcMetadata.Descs
	moduleFacts := requirements.moduleFacts
	if err := frontend.RejectUnsupportedWithFeaturesFactsAndValidation(m, features, moduleFacts, &validationAnalysis); err != nil {
		return nil, wrapContextError("compile", err)
	}
	var unsafeDirectTailImports []uint64
	if features.TailCalls {
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
	collectorReferenceCallBoundary := needsGCTypeMetadata && moduleHasCollectorReferenceCallBoundary(m)
	gcAllocationSites := moduleHasGCAllocationSitesWithValidation(m, &validationAnalysis)
	// These exact source products retain their bounded persistent-root proof and
	// fixed artifact image. Generic collector calls use frame-root maps.
	boundedPersistentGCRoots := exactGCRefTestAbstractSource || exactGCExternSource || exactGCRefCastAbstractSource || exactGCBrOnCastAbstractSource || exactGCBrOnCastFailAbstractSource ||
		exactGCStructBasicSource || exactGCArrayFixedOfficialSource || exactGCArrayDefaultOfficialSource || exactGCArrayPackedDataSource
	ownedExceptionReferences := railshotManagedExceptionReferences(m)
	opaqueExternFrames := compiledArchitecture == 4 && len(m.Code) > 0 && (requiredByModule.IsEnabled(CoreFeatureGC) || moduleHasOpaqueExternTable(m)) && exactGCRootedScalarSource && moduleHasOpaqueExternSignatures(m) && !genericGCExecution && !boundedPersistentGCRoots && !ownedExceptionReferences && !collectorReferenceCallBoundary && !moduleHasInternalCollectorReferenceFunctions(m)
	rootedReferenceDeclarations := opaqueExternFrames || compiledArchitecture == 4 && exactGCRootedScalarSource && (!metadataOnlyTypes || opaqueExternFrames) && gcStructProduct == 0 && gcArrayProduct == 0 && !gcAllocationSites && (opaqueExternFrames || moduleHasInternalCollectorReferenceFunctions(m) || collectorReferenceCallBoundary)
	exactNativeGCRoots := genericGCExecution || collectorReferenceCallBoundary && !boundedPersistentGCRoots || ownedExceptionReferences || rootedReferenceDeclarations
	functionEnvironmentFrames := compiledArchitecture == 4 && exactNativeGCRoots && !ownedExceptionReferences && !wasm.ModuleFunctionReferenceStorage(m) && wasm.ModuleFunctionReferenceCarriers(m)
	var gcFrameRootDiagnostic string
	var gcFrameRoots *shared.GCModuleFrameRootPlan
	if exactNativeGCRoots {
		gcFrameRoots = newGCFrameRootPlanWithInstructions(m, true, genericGCExecution, &gcFrameRootDiagnostic, &validationAnalysis, customInstructions)
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
	usesGCHelpers := ownedExceptionReferences || rootedReferenceDeclarations || gcStructProduct.requiresHelpers() || gcArrayProduct.requiresHelpers() || gcStructProduct.requiresArrayHelpers()
	if usesGCHelpers {
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
	// ARM64 serial lowering can publish its owned RW image directly, avoiding
	// heap staging and its GC traffic. Instantiation seals that same mapping.
	// Parallel lowering and other backends retain deferred heap publication.
	deferCodeMapping := goruntime.GOARCH != "arm64" || workers > 1
	compileOptions := railshotCompileOptions{BitCountFeatures: bitCountHostFeaturesSupported(), Workers: workers, DeferCodeMapping: deferCodeMapping, Optimizations: cfg.optimizations, OptimizationSnapshot: cfg.optimizationSnapshot, OptimizationDeltas: cfg.optimizationDeltas, ElideBoundsChecks: elide, NoBoundsFacts: cfg.noDeferBounds, ImportBindings: dynamicBindings, SyncHostCalls: atomicWaitHelpers || ownedExceptionReferences || foreignFunctionSubtypeHelpers, SyncHostSlots: syncHostSlots, GCTypeSubtypingRefTest: gcFunctionRefTest, GCStructHelpers: gcStructProduct.requiresHelpers(), GCArrayHelpers: gcArrayProduct.requiresHelpers() || gcStructProduct.requiresArrayHelpers(), GCFrameRoots: gcFrameRoots, Interruptible: !wruntime.HostInterruptSupported(), MemoryPressureAt: pressureAt, MemoryPressure: pressure, CustomInstructions: customInstructions, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: gcMetadata.Descs, GCTypeLayouts: gcMetadata.Layouts}}, Stats: gcCodeStats, SourceMaps: codeProfileEnabled && cfg.codeProfile.IncludeSources(), UnwindMaps: codeProfileEnabled && cfg.codeProfile.IncludeUnwind(), Profile: codeProfileEnabled && cfg.codeProfile != nil}
	railshotAttachValidationHintProof(&compileOptions, &validationAnalysis)
	railshotConfigureResourceLimits(cfg, &compileOptions)
	railshotConfigureI31Core(exactI31CoreSource, &compileOptions)
	if compiledArchitecture == 4 {
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

	typeConverter := newWasmTypeDescriptorConverter(m)
	types, err := typeConverter.typeDescriptors()
	if err != nil {
		return nil, wrapContextError("type metadata", err)
	}
	nativeGCABIVersion := uint32(0)
	if exactNativeGCRoots || gcStructProduct.requiresHelpers() || gcArrayProduct.requiresHelpers() || gcStructProduct.requiresArrayHelpers() {
		nativeGCABIVersion = gc.NativeABIVersion
	}
	c := newCompilerCompiled()
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
	c.GCTypeDescs = gcDescs
	c.requiredFeatures = requiredByModule
	c.dynamicImports = importedFuncs > 0
	c.dynamicFuncrefEscape = moduleDynamicFuncrefEscapeWithValidation(m, &validationAnalysis)
	c.customInstructions = customInstructions
	c.requiresNotifyPending = atomicWaitHelpers && cfg.optimizations["atomic-notify-empty"]
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
	if err := railshotStoreNativeTail(c, cm, cfg); err != nil {
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
	if gcStructProduct != 0 && gcStructProduct != stagedGCStructGeneric {
		gcGlobals, err := stagedGCStructGlobalInitializers(m)
		if err != nil {
			return nil, wrapContextError("compile: staged GC struct globals", err)
		}
		c.memoryDir.gcStructGlobals = gcGlobals
	}
	if gcArrayProduct != 0 {
		if gcArrayProduct != stagedGCArrayProductNewData && gcArrayProduct != stagedGCArrayProductNewElem && gcArrayProduct != stagedGCArrayProductGeneric {
			gcGlobals, err := stagedGCArrayGlobalInitializers(m, gcArrayProduct)
			if err != nil {
				return nil, wrapContextError("compile: staged GC array globals", err)
			}
			c.memoryDir.gcArrayGlobals = gcGlobals
		}
		if gcArrayProduct == stagedGCArrayProductReferenceElements {
			init, err := stagedGCArrayElementInitializer(m)
			if err != nil {
				return nil, wrapContextError("compile: staged GC array element segment", err)
			}
			c.memoryDir.gcArrayElement = init
		}
	}

	if err := compileModuleMetadata(c, constExprCtx, features, &requirements, unsafeDirectTailImports, gcArrayProduct, gcTypeSubtypingProduct, indexedFunctionRefOps); err != nil {
		return nil, err
	}
	compiled := installCompilerCompiledFinalizer(c)
	if len(cm.GCI31Inductions) != 0 {
		compiled.ensureCompileIndexes().gcI31Inductions = cm.GCI31Inductions
	}
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
		compiled.codeCache.mem = mapping
		compiled.codeCache.base = base
		railshotRecordMappedCodeBytes(compiled, len(compiled.code))
		compiled.codeCache.flags |= compiledCacheWritableCode
	}
	keepCodeImage = true
	if guardPageBuilt && cfg.boundsChecks == BoundsChecksSignalsBased {
		compiled.codeCache.flags |= compiledCacheGuardMemory
	}
	validGCFrameRoots := validGCModuleFrameRootPlanKinds(gcFrameRoots, compiledArchitecture == 4 && (ownedExceptionReferences || functionEnvironmentFrames || genericGCExecution && wasm.ModuleFunctionReferenceStorage(m)), compiledArchitecture == 4)
	if (ownedExceptionReferences || compiledArchitecture == 4 && (genericGCExecution || collectorReferenceCallBoundary)) && len(m.Code) == 0 && gcFrameRoots != nil && gcFrameRoots.FunctionCount() == 0 {
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
		rootMap := &compiledGCFrameRoots{deadInductionLayouts: cm.GCDeadInductionLayouts}
		var offsetInterner gcFrameOffsetInterner
		for function := 0; function < gcFrameRoots.FunctionCount(); function++ {
			plan := gcFrameRoots.Function(function)
			if plan == nil {
				continue
			}
			functionBase := uint32(compiled.Entry[function])
			if plan.AdapterReturnOffset != 0 {
				rootMap.adapterReturnOffsets = append(rootMap.adapterReturnOffsets, functionBase+plan.AdapterReturnOffset)
			}
			_ = plan.VisitSafepoints(func(i int, offsets []uint32) bool {
				rootMap.safepoints = append(rootMap.safepoints, compiledGCFrameSafepoint{id: plan.SafepointBase + uint32(i) + 1, frameBytes: plan.FrameBytes, offsets: offsetInterner.intern(offsets, true)})
				return true
			})
			_ = plan.VisitCallsites(func(_ int, callsite shared.GCFrameCallsite) bool {
				rootMap.callsites = append(rootMap.callsites, compiledGCFrameCallsite{returnOffset: functionBase + callsite.ReturnOffset(), frameBytes: plan.FrameBytes, stackAdjust: callsite.StackAdjust(), offsets: offsetInterner.intern(callsite.Offsets(), true)})
				return true
			})
		}
		rootMap.adapterReturnOffsets = normalizeAdapterReturnOffsets(rootMap.adapterReturnOffsets)
		compiled.validateMemo.gcFrameRoots = rootMap
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
	if features.TailCalls || features.TypedTailCalls {
		compiled.codeCache.stagedFeatures |= CoreFeatureTailCall
	}
	if nullReferenceProduct {
		compiled.codeCache.stagedFeatures |= CoreFeatureGC | CoreFeatureExceptionHandling
	}
	if structuralProduct != 0 {
		compiled.codeCache.stagedFeatures |= CoreFeatureGC
		compiled.codeCache.gcMetadataFlags |= compiledGCMetadataCollectorFreeStructural
	}
	if gcTypeSubtypingProduct != 0 {
		compiled.codeCache.stagedFeatures |= CoreFeatureGC
		compiled.codeCache.gcTypeSubtypingProduct = gcTypeSubtypingProduct
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
	if gcStructProduct != 0 {
		compiled.codeCache.stagedFeatures |= CoreFeatureGC
		compiled.codeCache.gcStructProduct = gcStructProduct
	}
	if gcArrayProduct != 0 {
		compiled.codeCache.stagedFeatures |= CoreFeatureGC
		compiled.codeCache.gcArrayProduct = gcArrayProduct
		if gcArrayProduct.metadataOnly() {
			compiled.codeCache.gcMetadataFlags |= compiledGCMetadataCollectorFreeArray
		}
	}
	if gcI31Product != 0 && !railshotExceptionHandlingSource(m) {
		compiled.codeCache.stagedFeatures |= CoreFeatureGC
		compiled.codeCache.gcI31Product = gcI31Product
	}
	if codeProfileEnabled && cfg.codeProfile != nil {
		if err := compiled.installCodeProfile(cfg, wasmBytes, gcCodeStats); err != nil {
			return nil, joinPrimary(err, compiled.Close())
		}
	}
	if compiledArchitecture == 4 && (genericGCExecution || collectorReferenceCallBoundary || ownedExceptionReferences || rootedReferenceDeclarations) && validGCFrameRoots {
		rootedTypeDigest, validRootedTypes := rv64RootedTypeDigest(compiled)
		if !validRootedTypes {
			return nil, joinPrimary(fmt.Errorf("compile: invalid rooted type receipt"), compiled.Close())
		}
		rootedDataDigest, validRootedData := rv64RootedActiveDataDigest(compiled)
		if !validRootedData {
			return nil, joinPrimary(fmt.Errorf("compile: invalid rooted active data receipt"), compiled.Close())
		}
		rootedExportDigest, validRootedExports := rv64RootedMemoryExportDigest(compiled)
		if !validRootedExports {
			return nil, joinPrimary(fmt.Errorf("compile: invalid rooted memory export receipt"), compiled.Close())
		}
		rootedGlobalDigest, validRootedGlobals := rv64RootedGlobalDigest(compiled)
		if !validRootedGlobals {
			return nil, joinPrimary(fmt.Errorf("compile: invalid rooted global receipt"), compiled.Close())
		}
		rootedTableDigest, validRootedTable := rv64RootedTableDigest(compiled)
		if !validRootedTable {
			return nil, joinPrimary(fmt.Errorf("compile: invalid rooted table receipt"), compiled.Close())
		}
		rootedImportDigest, validRootedImports := rv64RootedImportDigest(compiled)
		if !validRootedImports {
			return nil, joinPrimary(fmt.Errorf("compile: invalid rooted import receipt"), compiled.Close())
		}
		compiled.validateMemo.rv64FreshRootedGC = true
		compiled.validateMemo.rv64RootedNoCollectingBodies = true
		for function := 0; function < gcFrameRoots.FunctionCount(); function++ {
			if gcFrameRoots.Function(function) != nil {
				compiled.validateMemo.rv64RootedNoCollectingBodies = false
				break
			}
		}
		compiled.validateMemo.rv64FreshRootedGCDigest = sha256.Sum256(compiled.code)
		compiled.validateMemo.rv64FreshRootedGCTypeDigest = rootedTypeDigest
		compiled.validateMemo.rv64FreshRootedGCSyncHostSlots = compiled.syncHostSlots
		compiled.validateMemo.rv64FreshRootedGCReserveMax = compiled.MemMaxPages
		compiled.validateMemo.rv64FreshRootedGCDataDigest = rootedDataDigest
		compiled.validateMemo.rv64FreshRootedGCExportDigest = rootedExportDigest
		compiled.validateMemo.rv64FreshRootedGCGlobalDigest = rootedGlobalDigest
		compiled.validateMemo.rv64FreshRootedGCTableDigest = rootedTableDigest
		compiled.validateMemo.rv64FreshRootedGCImportDigest = rootedImportDigest
		compiled.validateMemo.rv64FreshRootedGCHasStart = compiled.HasStart
		if compiled.HasStart {
			compiled.validateMemo.rv64FreshRootedGCStartImport = compiled.StartIsImport
			if compiled.StartIsImport {
				compiled.validateMemo.rv64FreshRootedGCStartImportIndex = uint32(compiled.StartImportIdx)
			} else {
				compiled.validateMemo.rv64FreshRootedGCStartLocal = uint32(compiled.StartLocalFunc)
			}
		}
		if directory := compiled.memoryDir; directory != nil {
			compiled.validateMemo.rv64FreshRootedGCMemories = uint8(len(directory.defs))
			compiled.validateMemo.rv64FreshRootedGCMemoryStaged = directory.staged
			if len(directory.defs) != 0 {
				compiled.validateMemo.rv64FreshRootedGCMemMin = directory.defs[0].Min
				compiled.validateMemo.rv64FreshRootedGCMemMax = directory.defs[0].Max
			}
		}
	}
	installRV64StorageFreeProof(compiled, m)
	if err := installRV64SourceExecutionContract(compiled); err != nil {
		return nil, joinPrimary(fmt.Errorf("compile: %w", err), compiled.Close())
	}
	return publishCompilerCompiled(compiled)
}

// effectiveCompileBoundsMode keeps zero-minimum memories correct on ARM64.
// The current ARM64 guard entry places control words immediately below linMem;
// when linMem begins on the first inaccessible linear page, entry is not reliable
// across the supported Linux and Darwin signal trampolines. Compile those rare
// modules with explicit checks and classic growable memory instead.
func effectiveCompileBoundsMode(requested BoundsCheckMode, m *wasm.Module) BoundsCheckMode {
	if requested != BoundsChecksSignalsBased || goruntime.GOARCH != "arm64" {
		return requested
	}
	if min, ok := moduleInitialMemoryPages(m); ok && min == 0 {
		return BoundsChecksExplicit
	}
	return requested
}

func compileMemoryPressure(sourceBytes int) (int, func()) {
	// One checkpoint is enough to prevent dead per-function state from riding the
	// GC growth curve to the end of real-world modules. Small modules stay on the
	// zero-overhead path. runtime.GC does not modify GOGC or GOMEMLIMIT.
	if sourceBytes < 8<<20 {
		return 0, nil
	}
	return 0, goruntime.GC
}

func elemModeFromWasm(mode wasm.ElemModeKind) ElemMode {
	switch mode {
	case wasm.ElemPassive:
		return ElemModePassive
	case wasm.ElemDeclarative:
		return ElemModeDeclarative
	default:
		return ElemModeActive
	}
}

func elementPayloads(m *wasm.Module, types []DefinedTypeDescriptor, constExprCtx *constExprCompileContext, e *wasm.Elem) (ValType, ValueTypeDescriptor, []RefInit, error) {
	switch e.Kind.Kind {
	case wasm.ElemFuncs:
		out := make([]RefInit, len(e.Kind.Funcs))
		for i, fidx := range e.Kind.Funcs {
			out[i] = RefInit{FuncIndex: uint32(fidx)}
		}
		exact, err := valueTypeDescriptorInModule(m, wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false)))
		if err != nil {
			return 0, ValueTypeDescriptor{}, nil, err
		}
		return ValFuncRef, exact, out, nil
	case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
		refType := ValFuncRef
		exact, _ := valueTypeDescriptorFromValType(refType)
		if e.Kind.Kind == wasm.ElemTypedExprs {
			var err error
			exact, err = valueTypeDescriptorInModule(m, wasm.RefVal(e.Kind.Ref))
			if err != nil {
				return 0, ValueTypeDescriptor{}, nil, err
			}
			refType, err = valTypeFromWasmInModule(m, wasm.RefVal(e.Kind.Ref), types)
			if err != nil {
				return 0, ValueTypeDescriptor{}, nil, err
			}
		}
		out := make([]RefInit, len(e.Kind.Exprs))
		for i, ex := range e.Kind.Exprs {
			body := ex.BodyBytes
			if len(body) == 0 {
				var err error
				body, err = wasm.EncodeExpr(ex)
				if err != nil {
					return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d encode: %w", i, err)
				}
			}
			r := wasm.NewReader(body)
			if op, err := r.Byte(); err == nil && op == 0x23 {
				globalIndex, readErr := r.U32()
				end, endErr := r.Byte()
				if readErr == nil && endErr == nil && end == 0x0b && r.BytesLeft() == 0 {
					gt, ok := m.GlobalTypeByIndex(globalIndex)
					if !ok || gt.Mutable {
						return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d has invalid global.get initializer", i)
					}
					globalType, err := valueTypeDescriptorInModule(m, gt.Type)
					if err != nil || !valueTypeSubtype(globalType, types, exact, types) {
						return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d global type does not match segment type", i)
					}
					out[i] = RefInit{GlobalIndex: globalIndex, HasGlobal: true}
					continue
				}
			}
			if refType == ValI31Ref {
				if len(body) != 0 && body[0] == 0xd0 {
					result, err := evalConstExprBytesWithContext(body, wasm.RefVal(e.Kind.Ref), constExprCtx)
					if err == nil && isNullReferenceConstExprResult(result) {
						out[i] = RefInit{Null: true}
						continue
					}
				}
				result, matched, err := evalI31ConstExprBytes(body, wasm.RefVal(e.Kind.Ref), constExprCtx)
				if err != nil {
					return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d is not an exact i31 initializer: %v", i, err)
				}
				if !matched {
					result, err = evalConstExprBytesWithContext(body, wasm.RefVal(e.Kind.Ref), constExprCtx)
					if err != nil || len(result.Expr) == 0 {
						return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d is not an exact i31 initializer: %v", i, err)
					}
					out[i] = RefInit{Expr: append([]byte(nil), result.Expr...)}
					continue
				}
				if len(result.Expr) != 0 {
					r := wasm.NewReader(result.Expr)
					op, opErr := r.Byte()
					globalIndex, indexErr := r.U32()
					if opErr != nil || op != 0x23 || indexErr != nil {
						return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d has malformed i31 global initializer", i)
					}
					out[i] = RefInit{GlobalIndex: globalIndex, HasGlobal: true, I31Wrap: true}
					continue
				}
				if result.bits == 0 || result.bits>>32 != 0 || uint32(result.bits)&1 == 0 {
					return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d has invalid i31 immediate", i)
				}
				out[i] = RefInit{FuncIndex: uint32(result.bits)}
				continue
			}
			result, exprErr := evalConstExprBytesWithContext(body, wasm.RefVal(e.Kind.Ref), constExprCtx)
			if exprErr == nil {
				if len(result.Expr) != 0 {
					out[i] = RefInit{Expr: append([]byte(nil), result.Expr...)}
					continue
				}
				// Core 3 element expressions may use a null from any reference
				// hierarchy. The Release 2 fallback parser only covers func/extern.
				if isNullReferenceConstExprResult(result) {
					out[i] = RefInit{Null: true}
					continue
				}
				// A direct ref.i31 evaluates to tagged bits. Preserve its semantic
				// expression when the declared segment type is a supertype such as
				// anyref; FuncIndex is reserved for function-reference payloads here.
				if isI31ReferenceConstExprResult(result) {
					out[i] = RefInit{Expr: append([]byte(nil), body...)}
					continue
				}
			}
			payload, err := wasm.ParseElementExpr(ex)
			if err != nil {
				return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d: %w", i, err)
			}
			payloadType, err := valueTypeDescriptorInModule(m, wasm.RefVal(payload.RefType))
			if err != nil {
				return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d type: %w", i, err)
			}
			payloadABI, ok := payloadType.ABIType(types)
			if !ok || payloadABI != refType {
				return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d type does not match segment ABI type %s", i, refType)
			}
			out[i] = RefInit{FuncIndex: payload.FuncIndex, Null: payload.Null}
		}
		return refType, exact, out, nil
	default:
		return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("unsupported element kind %d", e.Kind.Kind)
	}
}

func memoryDefFromWasm(mt wasm.MemType) memoryDef {
	def := memoryDef{Min: mt.Limits.Min, Addr64: mt.Limits.Addr64, Shared: mt.Shared}
	if mt.Limits.HasMax {
		def.Max = mt.Limits.Max
		def.HasMax = true
	}
	return def
}

func maxInt() int { return int(^uint(0) >> 1) }

// CodeSize reports the exact native-code byte length. It returns zero after
// the compiled module is closed.
func (c *Compiled) CodeSize() int {
	if c == nil {
		return 0
	}
	c.ensureCodeCache()
	c.codeCache.mu.Lock()
	defer c.codeCache.mu.Unlock()
	if c.codeCache.closed {
		return 0
	}
	return len(c.code)
}
