//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"fmt"

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
	if admitCore3BrOnNullDeclarationsWire(data) || admitCore3BrOnNonNullDeclarationsWire(data) || admitCore3RefAsNonNullDeclarationsWire(data) || admitCore3RefIsNullDeclarationsWire(data) {
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
	referenceTails, indirectTails, referenceCalls, referenceControl, err := admitProductionDynamicTails(m, opts.NativeReferenceCalls, opts.dynamicTailCalls, usesSWAR, opts.referenceFunctions)
	if err != nil {
		return nil, err
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
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{CompactImports: true, ExtendedConstGlobals: true, MultiMemory: true}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
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
	if opts.ElideBoundsChecks || opts.GCTypeSubtypingRefTest || opts.GCStructHelpers || opts.GCArrayHelpers || opts.GCFrameRoots != nil || len(opts.CustomInstructions) != 0 || len(opts.Codegen.Module.GCTypeLayouts) != 0 || opts.Codegen.Runtime != nil || opts.Codegen.Heap != nil || opts.Codegen.Module.Features.GC || !allowSWAR && opts.Codegen.Module.Features.V128 {
		return unsupported("production host/guard/GC/custom lowering is pending")
	}
	// The public frontend emits inert function sentinels for numeric types.
	// They do not require heap lowering, but nonfunction descriptors do.
	for _, desc := range opts.Codegen.Module.GCTypeDescs {
		if desc.Kind != gc.KindFunc || !desc.Final || desc.HasSuper {
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
	types := internalReferenceTypes{
		decoded:                  m.Types,
		decodedSWAR:              vector,
		declarationExtern:        !vector && referenceFunctions && (admitCore3BrOnNullDeclarations(m) || admitCore3BrOnNonNullDeclarations(m)),
		declarationNonNullExtern: !vector && referenceFunctions && (admitCore3RefAsNonNullDeclarations(m) || admitCore3RefIsNullDeclarations(m)),
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
			referenceTails = referenceTails || op == 0x15
			indirectTails = indirectTails || op == 0x13
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
				return false, false, false, false, err
			}
		}
	}
	return referenceTails, indirectTails, hasReferenceCall, hasReferenceControl, nil
}
