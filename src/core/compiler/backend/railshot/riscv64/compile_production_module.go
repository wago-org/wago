//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"fmt"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	rv64runtime "github.com/wago-org/wago/src/core/runtime/riscv64"
)

// AdmitProductionSource checks wire provenance before the decoded public
// frontend discards present empty sections and explicit singleton rec groups.
// It is an execution policy for already decoded/validated source, not a decoder.
func AdmitProductionSource(data []byte) error {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return fmt.Errorf("rv64: invalid source header")
	}
	if len(data) > MaxModuleBytes {
		return unsupported("bounded source size")
	}
	if admitCore3BrOnNullDeclarationsWire(data) || admitCore3BrOnNonNullDeclarationsWire(data) || admitCore3RefAsNonNullDeclarationsWire(data) || admitCore3RefIsNullDeclarationsWire(data) || admitPublicReferenceCallBoundaryWire(data) || admitPublicHostReferenceBoundaryWire(data) || admitPublicDynamicHostReferenceBoundaryWire(data) || admitRV64I31CoreWire(data) || admitRV64GCRefEqWire(data) || admitRV64GCStructGetWire(data) || admitRV64GCStructMutationWire(data) || admitRV64GCStructNullWire(data) || admitRV64GCStructPackedWire(data) || admitRV64GCStructBasicWire(data) || admitRV64GCStructNumericGlobalsWire(data) || admitRV64GCArrayLocalWire(data) || admitRV64GCArrayUniformWire(data) || admitRV64GCArrayFixedLocalWire(data) || admitRV64GCArrayFixedOfficialWire(data) || admitRV64GCArrayDefaultOfficialWire(data) || admitRV64GCArrayPackedDataWire(data) || admitRV64GCArrayReferenceWire(data) || admitRV64GCArrayInitElemWire(data) || admitRV64GCArrayInitDataWire(data) || admitRV64GCArrayFillWire(data) || admitRV64GCArrayCopyWire(data) {
		return nil
	}
	if err := admitWireStartProfile(data, true, true, true); err != nil {
		return err
	}
	// Public state/import/table support makes their empty sections inert.
	// Private pilots preserve the original nonempty-section execution policy.
	return admitProductionWireProfile(data, true)
}

// CompileModuleWith compiles decoded modules for the production wrapper ABI.
// The public integration also calls AdmitProductionSource. Current bounded
// admission is deliberately retained until the complete port is qualified.
func CompileModuleWith(m *wasm.Module, opts CompileOptions) (*enc.CompiledModule, error) {
	if opts.referenceFunctions {
		return nil, unsupported("private reference function modules cannot be staged")
	}
	if opts.dynamicTailCalls {
		return nil, unsupported("private dynamic source plans have no public artifact/runtime contract")
	}
	if opts.InvocationContext && !opts.Interruptible {
		return nil, unsupported("invocation views require cooperative production")
	}
	if opts.NativeReferenceCalls && (!opts.InvocationContext || !opts.Interruptible || !opts.ForeignTableCalls) {
		return nil, unsupported("typed references require retained cooperative invocation ABI")
	}
	if opts.NativeTailCalls {
		if !opts.InvocationContext || !opts.Interruptible {
			return nil, unsupported("staged tails require authenticated cooperative invocation ABI")
		}
		opts.tailModule = m
		opts.localTailCalls = true
		opts.importTailCalls = true
		// Generations authenticate defined and certified native-import owners under
		// the exact entry lease. Runtime admission rejects host/relay bindings.
		opts.dynamicTailCalls = m != nil && opts.ForeignTableCalls
		var plans []tailImportPlan
		opts.tailPlans = &plans
	}
	if m == nil {
		return nil, fmt.Errorf("rv64: nil module")
	}
	decodedI31Core := admitRV64I31Core(m)
	if opts.I31CoreSource != decodedI31Core {
		return nil, unsupported("i31 core source provenance")
	}
	opts.i31Core = decodedI31Core
	decodedGCRefEq := admitRV64GCRefEq(m)
	if opts.GCRefEqSource != decodedGCRefEq {
		return nil, unsupported("gc ref.eq source provenance")
	}
	opts.gcRefEq = decodedGCRefEq
	decodedGCStructGet := admitRV64GCStructGet(m)
	if opts.GCStructGetSource != decodedGCStructGet {
		return nil, unsupported("gc struct.get source provenance")
	}
	opts.gcStructGet = decodedGCStructGet
	decodedGCStructMutation := admitRV64GCStructMutation(m)
	if opts.GCStructMutationSource != decodedGCStructMutation {
		return nil, unsupported("gc struct mutation source provenance")
	}
	opts.gcStructMutation = decodedGCStructMutation
	decodedGCStructNull := admitRV64GCStructNull(m)
	if opts.GCStructNullSource != decodedGCStructNull {
		return nil, unsupported("gc struct null source provenance")
	}
	opts.gcStructNull = decodedGCStructNull
	decodedGCStructPacked := admitRV64GCStructPacked(m)
	if opts.GCStructPackedSource != decodedGCStructPacked {
		return nil, unsupported("gc struct packed source provenance")
	}
	opts.gcStructPacked = decodedGCStructPacked
	decodedGCStructBasic := admitRV64GCStructBasic(m)
	if opts.GCStructBasicSource != decodedGCStructBasic {
		return nil, unsupported("gc struct basic source provenance")
	}
	opts.gcStructBasic = decodedGCStructBasic
	decodedGCStructNumericGlobals := admitRV64GCStructNumericGlobals(m)
	if opts.GCStructGlobalsSource != decodedGCStructNumericGlobals {
		return nil, unsupported("gc struct numeric globals source provenance")
	}
	opts.gcStructGlobals = decodedGCStructNumericGlobals
	decodedGCArrayLocal := admitRV64GCArrayLocal(m)
	if opts.GCArrayLocalSource != decodedGCArrayLocal {
		return nil, unsupported("gc array local source provenance")
	}
	opts.gcArrayLocal = decodedGCArrayLocal
	decodedGCArrayUniform := admitRV64GCArrayUniform(m)
	if opts.GCArrayUniformSource != decodedGCArrayUniform {
		return nil, unsupported("gc array uniform source provenance")
	}
	opts.gcArrayUniform = decodedGCArrayUniform
	decodedGCArrayFixedLocal := admitRV64GCArrayFixedLocal(m)
	if opts.GCArrayFixedLocalSource != decodedGCArrayFixedLocal {
		return nil, unsupported("gc array fixed local source provenance")
	}
	opts.gcArrayFixedLocal = decodedGCArrayFixedLocal
	decodedGCArrayFixedOfficial := admitRV64GCArrayFixedOfficial(m)
	if opts.GCArrayFixedOfficialSource != decodedGCArrayFixedOfficial {
		return nil, unsupported("gc array fixed official source provenance")
	}
	decodedGCArrayDefaultOfficial := admitRV64GCArrayDefaultOfficial(m)
	if opts.GCArrayDefaultOfficialSource != decodedGCArrayDefaultOfficial {
		return nil, unsupported("gc array default official source provenance")
	}
	opts.gcArrayFixedOfficial = decodedGCArrayFixedOfficial || decodedGCArrayDefaultOfficial
	decodedGCArrayPackedData := admitRV64GCArrayPackedData(m)
	if opts.GCArrayPackedDataSource != decodedGCArrayPackedData {
		return nil, unsupported("gc array packed data source provenance")
	}
	opts.gcArrayPackedData = decodedGCArrayPackedData
	decodedGCArrayReference := admitRV64GCArrayReference(m)
	if opts.GCArrayReferenceSource != decodedGCArrayReference {
		return nil, unsupported("gc array reference source provenance")
	}
	opts.gcArrayReference = decodedGCArrayReference
	decodedGCArrayInitElem := admitRV64GCArrayInitElem(m)
	if opts.GCArrayInitElemSource != decodedGCArrayInitElem {
		return nil, unsupported("gc array init elem source provenance")
	}
	opts.gcArrayInitElem = decodedGCArrayInitElem
	decodedGCArrayInitData := admitRV64GCArrayInitData(m)
	if opts.GCArrayInitDataSource != decodedGCArrayInitData {
		return nil, unsupported("gc array init data source provenance")
	}
	opts.gcArrayInitData = decodedGCArrayInitData
	decodedGCArrayFill := admitRV64GCArrayFill(m)
	if opts.GCArrayFillSource != decodedGCArrayFill {
		return nil, unsupported("gc array fill source provenance")
	}
	opts.gcArrayFill = decodedGCArrayFill
	decodedGCArrayCopy := admitRV64GCArrayCopy(m)
	if opts.GCArrayCopySource != decodedGCArrayCopy {
		return nil, unsupported("gc array copy source provenance")
	}
	opts.gcArrayCopy = decodedGCArrayCopy
	if err := AdmitPublicInitializers(m); err != nil {
		return nil, err
	}
	if _, err := admitTableProfile(m, true, opts.Interruptible && opts.ForeignTableCalls); err != nil {
		return nil, err
	}
	if len(m.Imports) > MaxProductionImports || m.MemCount() > MaxProductionMemories || len(m.Code) > MaxFunctions || len(m.Types) > MaxFunctions || len(m.Globals) > 256 || len(m.Data) > rv64runtime.MaxDataSegments || len(opts.Codegen.Module.GCTypeDescs) > MaxFunctions {
		return nil, unsupported("decoded module count bound")
	}
	// Decoded callers do not necessarily carry the original byte-size limit.
	// Bound instruction and data bytes before validation or payload copies.
	remaining := MaxModuleBytes
	consume := func(size int) bool {
		if size > remaining {
			return false
		}
		remaining -= size
		return true
	}
	for _, fn := range m.Code {
		if !consume(len(fn.BodyBytes)) {
			return nil, unsupported("decoded instruction/data size bound")
		}
	}
	for _, data := range m.Data {
		if !consume(len(data.Init)) {
			return nil, unsupported("decoded instruction/data size bound")
		}
	}
	usesSWAR, err := moduleUsesSWAR(m)
	if err != nil {
		return nil, err
	}
	if opts.NativeReferenceFunctions {
		opts.referenceFunctions, err = hasDefinedReferenceFunctions(m)
		if err != nil {
			return nil, err
		}
		if opts.referenceFunctions {
			// Permission alone cannot change the qualified direct-tail path.
			// The complete source scan below selects actual reference tails.
			opts.dynamicTailCalls = false
			if err := admitClosedReferenceFunctionModule(m, usesSWAR); err != nil {
				return nil, err
			}
		}
	}
	var referenceTails, indirectTails, referenceCalls, referenceControl bool
	if !opts.i31Core && !opts.gcRefEq && !opts.gcStructGet && !opts.gcStructMutation && !opts.gcStructNull && !opts.gcStructPacked && !opts.gcStructBasic && !opts.gcStructGlobals && !opts.gcArrayLocal && !opts.gcArrayUniform && !opts.gcArrayFixedLocal && !opts.gcArrayFixedOfficial && !opts.gcArrayPackedData && !opts.gcArrayReference && !opts.gcArrayInitElem && !opts.gcArrayInitData && !opts.gcArrayFill && !opts.gcArrayCopy {
		referenceTails, indirectTails, referenceCalls, referenceControl, err = admitProductionDynamicTails(m, opts.NativeReferenceCalls, opts.dynamicTailCalls, usesSWAR, opts.referenceFunctions)
		if err != nil {
			return nil, err
		}
	}
	if referenceControl && len(m.Code) == 0 {
		if m.FuncCount() != 0 {
			return nil, unsupported("typed control declarations require a defined code owner")
		}
		// A function-free module retains exact declared type metadata but owns no
		// executable carrier state.  Keep the source feature requirement while
		// withholding the native control ABI/owner-lifetime witness.
		referenceControl = false
	}
	opts.referenceTailInstructions = referenceTails
	opts.indirectTailInstructions = indirectTails
	opts.referenceCallInstructions = referenceCalls
	opts.referenceControlInstructions = referenceControl
	if opts.referenceFunctions {
		opts.dynamicTailCalls = referenceTails || indirectTails
		if err := admitClosedReferenceProductionOptions(opts); err != nil {
			return nil, err
		}
	}
	if usesSWAR {
		return compileSWARProductionModule(m, opts)
	}
	if err := chargeDeclaredTypeBytes(m, &remaining); err != nil {
		return nil, err
	}
	if len(m.Customs) > MaxFunctions {
		return nil, unsupported("decoded custom section count bound")
	}
	if err := admitPublicBranchHints(m, true, &remaining); err != nil {
		return nil, err
	}
	// Reuse the existing decoded metadata accounting with the instruction/data
	// budget. Opaque custom payloads must stay bounded for hand-built callers,
	// and the decoded/raw name views must not duplicate retained byte charges.
	if err := admitSWARDecodedMetadataBudget(m, &remaining); err != nil {
		return nil, err
	}
	if err := admitProductionOptions(m, &opts, false); err != nil {
		return nil, err
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{CompactImports: true, ExtendedConstGlobals: true, MultiMemory: true, GCConstExpr: opts.i31Core || opts.gcRefEq || opts.gcStructGet || opts.gcStructMutation || opts.gcStructNull || opts.gcStructPacked || opts.gcStructBasic || opts.gcStructGlobals || opts.gcArrayLocal || opts.gcArrayUniform || opts.gcArrayFixedLocal || opts.gcArrayFixedOfficial || opts.gcArrayPackedData || opts.gcArrayReference || opts.gcArrayInitElem || opts.gcArrayInitData || opts.gcArrayFill || opts.gcArrayCopy}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		return nil, err
	}
	opts.production = true
	opts.publicStarts = true
	c, err := compileDecodedModule(m, scalarRegisters[:], opts)
	if err != nil {
		return nil, err
	}
	if opts.referenceFunctions {
		return stageClosedReferenceProductionModule(c, opts, m, false)
	}
	return stageProductionModule(c, opts)
}

// admitProductionOptions keeps semantic option admission identical across the
// scalar compiler and the private SWAR production staging prerequisite.
func admitProductionOptions(m *wasm.Module, opts *CompileOptions, allowSWAR bool) error {
	if opts.localTailCalls && (!opts.Interruptible || opts.Profile || opts.SourceMaps || opts.UnwindMaps) {
		return unsupported("local tails require cooperative production without profiling metadata")
	}
	if opts.ForeignTableCalls && !opts.Interruptible {
		return unsupported("foreign table calls require the cooperative framed runtime ABI")
	}
	if err := admitProductionStart(m); err != nil {
		return err
	}
	if len(opts.Codegen.Module.GCTypeDescs) > MaxFunctions {
		return unsupported("production function descriptor count bound")
	}
	if opts.SyncHostSlots < 0 || opts.SyncHostSlots > 65535 {
		return fmt.Errorf("rv64: invalid synchronous host slot capacity %d", opts.SyncHostSlots)
	}
	if err := admitHostBindings(m, opts); err != nil {
		return err
	}
	if opts.ElideBoundsChecks || opts.GCTypeSubtypingRefTest || (!opts.gcRefEq && !opts.gcStructGet && !opts.gcStructMutation && !opts.gcStructNull && !opts.gcStructPacked && !opts.gcStructBasic && !opts.gcStructGlobals && !opts.gcArrayLocal && !opts.gcArrayUniform && !opts.gcArrayFixedLocal && !opts.gcArrayFixedOfficial && !opts.gcArrayPackedData && !opts.gcArrayReference && !opts.gcArrayInitElem && !opts.gcArrayInitData && !opts.gcArrayFill && !opts.gcArrayCopy && (opts.GCStructHelpers || opts.GCArrayHelpers || opts.GCFrameRoots != nil || len(opts.Codegen.Module.GCTypeLayouts) != 0)) || len(opts.CustomInstructions) != 0 || opts.Codegen.Runtime != nil || opts.Codegen.Heap != nil || opts.Codegen.Module.Features.GC && !opts.i31Core && !opts.gcRefEq && !opts.gcStructGet && !opts.gcStructMutation && !opts.gcStructNull && !opts.gcStructPacked && !opts.gcStructBasic && !opts.gcStructGlobals && !opts.gcArrayLocal && !opts.gcArrayUniform && !opts.gcArrayFixedLocal && !opts.gcArrayFixedOfficial && !opts.gcArrayPackedData && !opts.gcArrayReference && !opts.gcArrayInitElem && !opts.gcArrayInitData && !opts.gcArrayFill && !opts.gcArrayCopy || !allowSWAR && opts.Codegen.Module.Features.V128 {
		return unsupported("production host/guard/GC/custom lowering is pending")
	}
	if opts.gcRefEq && (!opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 2) {
		return unsupported("gc ref.eq helper contract")
	}
	if opts.gcStructGet && (!opts.GCStructHelpers || opts.GCArrayHelpers || opts.SyncHostSlots < 3) {
		return unsupported("gc struct.get helper contract")
	}
	if opts.gcStructMutation && (!opts.GCStructHelpers || opts.GCArrayHelpers || opts.SyncHostSlots < 4) {
		return unsupported("gc struct mutation helper contract")
	}
	if opts.gcStructNull && (!opts.GCStructHelpers || opts.GCArrayHelpers || opts.SyncHostSlots < 4) {
		return unsupported(fmt.Sprintf("gc struct null helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcStructPacked && (!opts.GCStructHelpers || opts.GCArrayHelpers || opts.SyncHostSlots < 4) {
		return unsupported(fmt.Sprintf("gc struct packed helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcStructBasic && (!opts.GCStructHelpers || opts.GCArrayHelpers || opts.SyncHostSlots < 4) {
		return unsupported(fmt.Sprintf("gc struct basic helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayLocal && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 4) {
		return unsupported(fmt.Sprintf("gc array local helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayUniform && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 4) {
		return unsupported(fmt.Sprintf("gc array uniform helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayFixedLocal && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 5) {
		return unsupported(fmt.Sprintf("gc array fixed local helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayFixedOfficial && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 5) {
		return unsupported(fmt.Sprintf("gc array fixed official helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayPackedData && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 4) {
		return unsupported(fmt.Sprintf("gc array packed data helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayReference && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 4) {
		return unsupported(fmt.Sprintf("gc array reference helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayInitElem && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 6) {
		return unsupported(fmt.Sprintf("gc array init elem helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayInitData && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 6) {
		return unsupported(fmt.Sprintf("gc array init data helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayFill && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 5) {
		return unsupported(fmt.Sprintf("gc array fill helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcArrayCopy && (opts.GCStructHelpers || !opts.GCArrayHelpers || opts.SyncHostSlots < 7) {
		return unsupported(fmt.Sprintf("gc array copy helper contract struct=%t array=%t slots=%d", opts.GCStructHelpers, opts.GCArrayHelpers, opts.SyncHostSlots))
	}
	if opts.gcRefEq || opts.gcStructGet || opts.gcStructMutation || opts.gcStructNull || opts.gcStructPacked || opts.gcStructBasic || opts.gcStructGlobals || opts.gcArrayLocal || opts.gcArrayUniform || opts.gcArrayFixedLocal || opts.gcArrayFixedOfficial || opts.gcArrayPackedData || opts.gcArrayReference || opts.gcArrayInitElem || opts.gcArrayInitData || opts.gcArrayFill || opts.gcArrayCopy {
		expected, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			return err
		}
		if opts.GCFrameRoots != nil || !reflect.DeepEqual(opts.Codegen.Module.GCTypeDescs, expected.Descs) || !reflect.DeepEqual(opts.Codegen.Module.GCTypeLayouts, expected.Layouts) {
			return unsupported("gc ref.eq collector metadata contract")
		}
	}
	// The public frontend emits inert function sentinels for numeric types.
	// They do not require heap lowering, but nonfunction descriptors do.
	for _, desc := range opts.Codegen.Module.GCTypeDescs {
		if !opts.gcRefEq && !opts.gcStructGet && !opts.gcStructMutation && !opts.gcStructNull && !opts.gcStructPacked && !opts.gcStructBasic && !opts.gcStructGlobals && !opts.gcArrayLocal && !opts.gcArrayUniform && !opts.gcArrayFixedLocal && !opts.gcArrayFixedOfficial && !opts.gcArrayPackedData && !opts.gcArrayReference && !opts.gcArrayInitElem && !opts.gcArrayInitData && !opts.gcArrayFill && !opts.gcArrayCopy && (desc.Kind != gc.KindFunc || !desc.Final || desc.HasSuper) {
			return unsupported("production GC descriptor lowering is pending")
		}
	}
	if err := gc.ValidateTypeDescs(opts.Codegen.Module.GCTypeDescs); err != nil {
		return err
	}
	if opts.Stats != nil && !diagnosticsEnabled {
		return unsupported("compiler diagnostics require -tags=wago_codegenstats or wago_profile")
	}
	if opts.UnwindMaps || opts.CollectInlineReport {
		return unsupported("unwind metadata and inline reports are pending")
	}
	if opts.Profile && !profileEnabled {
		return unsupported("profiling requires -tags=wago_profile")
	}
	if opts.SourceMaps && !opts.Profile {
		return unsupported("source maps require profiling")
	}
	if opts.Profile && opts.Stats == nil {
		opts.Stats = &ModuleStats{}
	}
	if _, err := optimizationBindings.ResolveSnapshot(opts.Optimizations, opts.OptimizationSnapshot, opts.OptimizationDeltas); err != nil {
		return err
	}
	return nil
}

func stageProductionModule(c *Compiled, opts CompileOptions) (*enc.CompiledModule, error) {
	if err := admitProductionFunctionABI(c); err != nil {
		return nil, err
	}
	return stageAdmittedProductionModule(c, opts)
}

// Both callers authenticate the selected function ABI before this shared
// profile/ownership staging implementation can allocate a code image.
func stageAdmittedProductionModule(c *Compiled, opts CompileOptions) (*enc.CompiledModule, error) {
	var tail *enc.NativeTailModule
	if opts.NativeTailCalls {
		if _, _, err := NativeTailInstanceBudget(len(*opts.tailPlans), len(c.code)); err != nil {
			return nil, err
		}
		tail = &enc.NativeTailModule{}
		values := 0
		for _, p := range *opts.tailPlans {
			if opts.tailModule == nil {
				return nil, unsupported("staged tail module metadata")
			}
			var sig *wasm.CompType
			var ok bool
			switch p.kind {
			case enc.NativeTailDirectImport:
				sig, ok = opts.tailModule.FuncSignature(p.importIndex)
			case enc.NativeTailIndirect, enc.NativeTailReference:
				if !opts.dynamicTailCalls {
					return nil, unsupported("indirect tail requires native owner generations")
				}
				sig, ok = opts.tailModule.TypeFunc(p.typeIndex)
			default:
				return nil, unsupported("unknown native tail runtime attachment")
			}
			if !ok {
				return nil, unsupported("staged tail signature metadata")
			}
			values += len(sig.Params) + len(sig.Results)
			if err := NativeTailMetadataBudget(len(*opts.tailPlans), values); err != nil {
				return nil, err
			}
			local := int(p.functionIndex) - c.state.importedFunctions
			if local < 0 || local >= len(c.functions) || c.functions[local].FrameHeaderBytes != callFrameHeader {
				return nil, unsupported("staged tail finalized frame metadata")
			}
			tail.Plans = append(tail.Plans, enc.NativeTailPlan{Kind: p.kind, TypeIndex: p.typeIndex, ReferenceBase: p.referenceBase, FunctionIndex: p.functionIndex, ImportIndex: p.importIndex, SourcePC: p.sourcePC, FrameBytes: c.functions[local].FrameBytes, SourceBase: p.sourceBase, SourceStride: p.sourceStride})
		}
	}
	if opts.referenceTailInstructions {
		qualified := false
		if tail != nil {
			for _, p := range tail.Plans {
				qualified = qualified || p.Kind == enc.NativeTailReference
			}
		}
		if !qualified {
			return nil, unsupported("typed tails require a qualified reference plan")
		}
	}
	if opts.referenceFunctions && opts.indirectTailInstructions {
		qualified := false
		if tail != nil {
			for _, p := range tail.Plans {
				qualified = qualified || p.Kind == enc.NativeTailIndirect
			}
		}
		if !qualified {
			return nil, unsupported("private indirect tails require a qualified indirect plan")
		}
	}
	if err := c.prepareProfile(len(c.code)); err != nil {
		return nil, err
	}
	if opts.MemoryPressure != nil && (opts.MemoryPressureAt == 0 || len(c.code) >= opts.MemoryPressureAt) {
		opts.MemoryPressure()
	}
	image, err := stageProductionImage(c, opts.DeferCodeMapping)
	if err != nil {
		return image, err
	}
	// The shared compiler contract represents deferred heap ownership as Code
	// with no CodeImage. A nonnil CodeImage promises Take can transfer an RW
	// mapping to the public cache; heap staging cannot fulfill that promise.
	if opts.DeferCodeMapping && image.CodeImage != nil {
		image.Code, err = image.CodeImage.(*coreruntime.CodeBuffer).TakeHeap()
		if err != nil {
			return nil, err
		}
		image.CodeImage = nil
	}
	image.NativeTail = tail
	image.NativeReferenceCalls = opts.referenceCallInstructions
	image.NativeReferenceControl = opts.referenceControlInstructions
	image.NativeReferenceFunctions = c.state.referenceFunctions
	if diagnosticsEnabled && opts.Stats != nil {
		collectFinalizedStats(c, opts.Stats)
	}
	return image, nil
}

// Source instruction requirements are persisted independently of reachability.
// Ordinary references require their distinct retained normal-return protocol.
// Reference control retains carriers without requiring declared-call transport.
// Instruction bytes were bounded before this pass.
func admitProductionDynamicTails(m *wasm.Module, referenceCalls, dynamicTails, vector, referenceFunctions bool) (bool, bool, bool, bool, error) {
	referenceTails, indirectTails, hasReferenceCall, hasReferenceControl := false, false, false, false
	publicReferenceBoundary := referenceFunctions && !vector && admitPublicReferenceCarrierBoundary(m)
	types := internalReferenceTypes{
		decoded:                  m.Types,
		decodedSWAR:              vector,
		declarationExtern:        !vector && referenceFunctions && (admitCore3BrOnNullDeclarations(m) || admitCore3BrOnNonNullDeclarations(m)),
		executionExtern:          !vector && referenceFunctions && isCore3RefIsNullExecution(m),
		declarationNonNullExtern: !vector && referenceFunctions && (admitCore3RefAsNonNullDeclarations(m) || admitCore3RefIsNullDeclarations(m)),
		publicReferenceCarriers:  publicReferenceBoundary && publicReferenceCarrierTarget(&m.Types[0].SubTypes[0].Comp),
	}
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 {
			return false, false, false, false, unsupported("control declaration group")
		}
		sub := &group.SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
			return false, false, false, false, unsupported("control declaration metadata")
		}
		_, witness, err := admitDeclaredControlSignature(&sub.Comp, types, true, referenceCalls, vector)
		if err != nil {
			return false, false, false, false, err
		}
		hasReferenceControl = hasReferenceControl || witness
	}
	// Control-only declarations do not relax actual function signatures. The
	// closed function ABI is selected separately after whole-module proof.
	for i := 0; i < m.FuncCount(); i++ {
		sig, ok := m.FuncSignature(uint32(i))
		if !ok {
			return false, false, false, false, unsupported("function signature binding")
		}
		var err error
		if referenceFunctions {
			_, _, err = admitDeclaredControlSignature(sig, types, true, referenceCalls, vector)
		} else if vector {
			err = swarCallSignature(sig)
		} else {
			err = integerNumericSignature(sig, true)
		}
		if err != nil {
			return false, false, false, false, err
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for _, fn := range m.Code {
		for _, run := range fn.Locals.Runs {
			if run.Type.Kind() == wasm.ValRef {
				if !internalFunctionReferenceWithTypes(run.Type, referenceCalls, types) {
					return false, false, false, false, unsupported("reference local requires an admitted retained function-reference type")
				}
				hasReferenceControl = hasReferenceControl || !run.Type.Ref().Nullable() || run.Type.Ref().Heap().Kind() == wasm.HeapTypeIndex
			}
		}
		r := wasm.ReaderFrom(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false, false, false, false, err
			}
			if op == 0x02 || op == 0x03 || op == 0x04 {
				if lead, ok := r.Peek(); ok && (lead == 0x63 || lead == 0x64) {
					probe := r
					witness, err := internalExplicitBlockReferenceType(&probe, referenceCalls, types)
					if err != nil {
						return false, false, false, false, err
					}
					hasReferenceControl = hasReferenceControl || witness
				}
			}
			if op == 0x1c {
				probe := r
				count, err := probe.U32()
				if err != nil {
					return false, false, false, false, err
				}
				if count != 1 {
					return false, false, false, false, unsupported("typed select requires one value type")
				}
				if lead, ok := probe.Peek(); ok && (lead == 0x63 || lead == 0x64) {
					witness, err := internalExplicitReferenceType(&probe, referenceCalls, types)
					if err != nil {
						return false, false, false, false, err
					}
					hasReferenceControl = hasReferenceControl || witness
				}
			}
			if op == 0x14 {
				if !referenceCalls {
					return false, false, false, false, unsupported("call_ref requires retained native reference protocol")
				}
				hasReferenceCall = true
			}
			if op == 0xd0 {
				probe := r
				heap, err := probe.S33()
				if err != nil {
					return false, false, false, false, err
				}
				if heap >= 0 {
					if !(referenceCalls || dynamicTails && !referenceFunctions) || !types.function(uint32(heap)) {
						return false, false, false, false, unsupported("indexed ref.null requires retained typed-reference protocol and an in-range function type")
					}
					// A typed null still requires the persisted feature witness even
					// without a reference call, or when its body is unreachable.
					hasReferenceControl = true
				}
			}
			if op == 0xd4 || op == 0xd5 || op == 0xd6 {
				if !referenceCalls {
					return false, false, false, false, unsupported("reference control requires retained typed-reference protocol")
				}
				hasReferenceControl = true
			}
			if op == 0x15 {
				referenceTails = true
				// Public tail arguments need the same invocation-scoped owner
				// graph as ordinary call_ref. Private reference tails keep their
				// existing internal-only marker.
				hasReferenceCall = hasReferenceCall || publicReferenceBoundary
			}
			indirectTails = indirectTails || op == 0x13
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
				return false, false, false, false, err
			}
		}
	}
	return referenceTails, indirectTails, hasReferenceCall, hasReferenceControl, nil
}
