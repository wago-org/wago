//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// compileSWARProductionModule stages the qualified SWAR lowering through
// the shared production code-image contract. Public integration additionally enforces
// source, artifact and CPU admission. Decoded callers need separate wire
// provenance admission and instance state setup before making entries callable.
func compileSWARProductionModule(m *wasm.Module, opts CompileOptions) (*enc.CompiledModule, error) {
	if m == nil {
		return nil, fmt.Errorf("rv64: nil module")
	}
	admissionModule := m
	if opts.ExceptionHandlingSource {
		admissionModule = exceptionAdmissionProjection(m)
	}
	if err := admitSWARDecodedBudgetReferences(pluginBudgetModule(admissionModule, opts.pluginDirectory), true, true, opts.NativeReferenceCalls || opts.gcRootedScalar, opts.gcRootedScalar); err != nil {
		return nil, err
	}
	if err := admitProductionOptions(m, &opts, true); err != nil {
		return nil, err
	}
	integerFunctions := make([]bool, len(m.Code))
	var integerState *moduleState
	if opts.gcRootedScalar && !opts.pluginDirectory.hasCustom() {
		for i := range m.Code {
			usesSWAR, err := functionUsesSWAR(m, i)
			if err != nil {
				return nil, err
			}
			integerFunctions[i] = !usesSWAR
		}
		for _, integer := range integerFunctions {
			if !integer {
				continue
			}
			state, err := admitStatePolicy(admissionModule, true, opts.hostImports, true, opts.Interruptible && opts.ForeignTableCalls)
			if err != nil {
				return nil, err
			}
			if opts.ExceptionHandlingSource {
				state.exceptionModule = m
				restoreExceptionStateTypes(m, &state)
			}
			state.localTailCalls = opts.localTailCalls
			state.foreignTableCalls = opts.ForeignTableCalls
			state.importTailCalls = opts.importTailCalls
			state.dynamicTailCalls = opts.dynamicTailCalls
			state.referenceCalls = true
			state.gcRootedScalar = true
			state.i31Core = true
			state.i31RefEq = true
			state.capacity = max(state.capacity, rootedScalarHelperSlots(m))
			var ok bool
			state.recTypeDir, ok = newInternalReferenceTypeDirectory(m.Types)
			if !ok {
				return nil, unsupported("hybrid integer rooted type directory")
			}
			integerState = &state
			break
		}
	}
	policy := swarModulePlain
	if len(m.Tables)+m.ImportedTableCount() != 0 || len(m.Elements) != 0 && !opts.gcRootedScalar {
		policy = swarModuleTables
	} else if len(m.Imports) != 0 {
		policy = swarModuleHosts
	}
	exceptionModule := (*wasm.Module)(nil)
	if opts.ExceptionHandlingSource {
		exceptionModule = m
	}
	image, err := compileSWARDecodedModuleProfile(m, emissionMode{tableModuleSource: m, exceptionModule: exceptionModule, exceptionOperands: opts.exceptionOperands, ownedExceptionReferences: opts.OwnedExceptionReferences, gcFunctionEnvironments: opts.gcFunctionEnvironments, profileMode: newProfileModule(opts.Profile, opts.SourceMaps, len(m.Code)).baseProfileMode(), production: true, threadedMemory: moduleThreadedMemory(m), elideBounds: opts.ElideBoundsChecks, interruptible: opts.Interruptible, foreignTableCalls: opts.ForeignTableCalls, invocationContext: opts.InvocationContext, localTailCalls: opts.localTailCalls, importTailCalls: opts.importTailCalls, dynamicTailCalls: opts.dynamicTailCalls, referenceCalls: opts.NativeReferenceCalls || opts.gcRootedScalar, gcRootedScalar: opts.gcRootedScalar, referenceFunctions: opts.referenceFunctions, tailPlans: opts.tailPlans, tailSiteBase: opts.tailSiteBase, customInstructions: opts.pluginDirectory, customMemory: m.ImportedMemCount()+len(m.Memories) > 0}, policy, swarStateProfile{moduleByteLimit: publicModuleByteLimit(opts), maxFunctionLocals: publicFunctionLocalLimit(opts), publicInitializers: true, publicStarts: true, exports: true, importedMemory: true, indexedMemories: true, importedGlobals: true, importedTable: opts.Interruptible && opts.ForeignTableCalls, functionTableImports: opts.Interruptible && opts.ForeignTableCalls, gcFrameRoots: opts.GCFrameRoots, integerState: integerState, integerFunctions: integerFunctions})
	if err != nil {
		return nil, err
	}
	c := &Compiled{profileModuleState: image.profileModuleState, code: image.code, functions: image.functions, state: moduleState{importedFunctions: len(image.hosts), referenceFunctions: opts.referenceFunctions, localTailCalls: opts.localTailCalls, importTailCalls: opts.importTailCalls, dynamicTailCalls: opts.dynamicTailCalls}}
	c.finalizedReferenceImportTails = image.finalizedReferenceImportTails
	c.referenceImportTailsFinalized = image.referenceImportTailsFinalized
	if opts.referenceFunctions && !opts.ExceptionHandlingSource {
		return stageClosedReferenceProductionModule(c, opts, m, true)
	}
	return stageProductionModule(c, opts)
}

// moduleUsesSWAR walks encoded instructions, skipping their actual immediates;
// an immediate byte equal to the SIMD prefix must not change compiler selection.
// Existing production count/byte budgets precede this walk, and per-type/run
// limits prevent hand-built decoded callers from hiding unbounded metadata.
func moduleUsesSWAR(m *wasm.Module) (bool, error) {
	if m == nil || len(m.Imports) > MaxPublicImports || len(m.Memories) > MaxProductionMemories || m.MemCount() > MaxProductionMemories {
		return false, unsupported("SWAR selector memory/import count bound")
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	flatTypes := 0
	for _, group := range m.Types {
		flatTypes += len(group.SubTypes)
		if flatTypes > MaxPublicCodeBytes/8 {
			return false, unsupported("decoded type directory bound")
		}
		for subtypeIndex := range group.SubTypes {
			sig := &group.SubTypes[subtypeIndex].Comp
			if len(sig.Params) > 256 || len(sig.Results) > 256 {
				return false, unsupported("decoded signature bound")
			}
			for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
				for _, typ := range list {
					if wasm.EqualValType(typ, wasm.V128) {
						return true, nil
					}
				}
			}
		}
	}
	for _, global := range m.Globals {
		if wasm.EqualValType(global.Type.Type, wasm.V128) {
			return true, nil
		}
	}
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternGlobal && wasm.EqualValType(im.Type.GlobalType().Type, wasm.V128) {
			return true, nil
		}
	}
	for _, fn := range m.Code {
		if len(fn.Locals.Runs) > 256 {
			return false, unsupported("decoded local declaration bound")
		}
		for _, run := range fn.Locals.Runs {
			if wasm.EqualValType(run.Type, wasm.V128) {
				return true, nil
			}
		}
		reader := wasm.ReaderFrom(fn.BodyBytes)
		for reader.HasNext() {
			op, err := reader.Byte()
			if err != nil {
				return false, err
			}
			if op == 0xfd {
				return true, nil
			}
			if op == 0x02 || op == 0x03 || op == 0x04 {
				// Inline block types are single bytes; indexed signatures
				// were already examined above. Peek without disturbing the
				// reader used by the ordinary immediate classifier below.
				look := reader
				if typ, err := look.Byte(); err == nil && typ == 0x7b {
					return true, nil
				}
			}
			if op == 0x1c {
				// Typed select has exactly one valtype. Check its count before
				// any general classifier could expand unvalidated metadata.
				look := reader
				count, err := look.U32()
				if err != nil || count != 1 {
					return false, unsupported("typed select valtype count")
				}
				if typ, err := look.Byte(); err == nil && typ == 0x7b {
					return true, nil
				}
			}
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(&reader, op, &imm); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

// functionUsesSWAR identifies functions whose own physical frame requires the
// padded vector layout. References and GC instructions alone remain eligible
// for the integer/GC lowering even when another function in the module uses
// SIMD. Unsupported cross-function vector flows still fail in the selected
// lowerer's typed instruction walk before any image is published.
func functionUsesSWAR(m *wasm.Module, function int) (bool, error) {
	if m == nil || function < 0 || function >= len(m.Code) {
		return false, unsupported("SWAR function selector index")
	}
	ft, ok := m.LocalFuncType(function)
	if !ok {
		return false, unsupported("SWAR function selector signature")
	}
	for _, values := range [][]wasm.ValType{ft.Params, ft.Results} {
		for _, typ := range values {
			if wasm.EqualValType(typ, wasm.V128) {
				return true, nil
			}
		}
	}
	for _, run := range m.Code[function].Locals.Runs {
		if wasm.EqualValType(run.Type, wasm.V128) {
			return true, nil
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	r := wasm.ReaderFrom(m.Code[function].BodyBytes)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false, err
		}
		if op == 0xfd {
			return true, nil
		}
		if op == 0x02 || op == 0x03 || op == 0x04 {
			// A vector block signature changes the operand stack layout even when
			// all vector-producing instructions are calls or global accesses.
			look := r
			lead, ok := look.Peek()
			if !ok {
				return false, unsupported("SWAR function selector block type")
			}
			if lead == 0x7b {
				return true, nil
			}
			if lead != 0x40 && !swarBlockValTypeLead(lead) {
				typeIndex, err := look.S33()
				if err != nil || typeIndex < 0 {
					return false, unsupported("SWAR function selector block type")
				}
				sig, ok := m.ResolvedTypeFunc(uint32(typeIndex))
				if !ok {
					return false, unsupported("SWAR function selector block signature")
				}
				if swarSignatureUsesVector(sig) {
					return true, nil
				}
			}
		}
		if op == 0x1c {
			look := r
			count, err := look.U32()
			if err != nil || count != 1 {
				return false, unsupported("typed select valtype count")
			}
			if typ, err := look.Byte(); err == nil && typ == 0x7b {
				return true, nil
			}
		}
		var imm wasm.InstructionImmediate
		if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
			return false, err
		}
		switch imm.Kind {
		case wasm.InstrCall, wasm.InstrReturnCall:
			sig, ok := m.FuncSignature(imm.Index)
			if !ok {
				return false, unsupported("SWAR function selector call signature")
			}
			if swarSignatureUsesVector(sig) {
				return true, nil
			}
		case wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef:
			sig, ok := m.ResolvedTypeFunc(imm.Index)
			if !ok {
				return false, unsupported("SWAR function selector typed call signature")
			}
			if swarSignatureUsesVector(sig) {
				return true, nil
			}
		case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
			global, ok := m.GlobalTypeByIndex(imm.Index)
			if !ok {
				return false, unsupported("SWAR function selector global type")
			}
			if wasm.EqualValType(global.Type, wasm.V128) {
				return true, nil
			}
		}
	}
	return false, nil
}

func swarSignatureUsesVector(sig *wasm.CompType) bool {
	if sig == nil || sig.Kind != wasm.CompFunc {
		return false
	}
	for _, values := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, typ := range values {
			if wasm.EqualValType(typ, wasm.V128) {
				return true
			}
		}
	}
	return false
}

func swarBlockValTypeLead(lead byte) bool {
	switch lead {
	case 0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x63, 0x64,
		0x6f, 0x70, 0x6e, 0x6d, 0x6c, 0x6b, 0x6a, 0x69,
		0x71, 0x72, 0x73, 0x74:
		return true
	}
	return false
}

// FunctionUsesSWAR shares the exact physical-frame selector with the public
// root-plan producer.
func FunctionUsesSWAR(m *wasm.Module, function int) (bool, error) {
	return functionUsesSWAR(m, function)
}

// UsesSWAR shares the bounded compiler selector with public import admission.
// The shared frontend does not populate Codegen.Features with Wasm SIMD
// requirements, so that field cannot select the public vector import profile.
func UsesSWAR(m *wasm.Module) (bool, error) {
	if m == nil || len(m.Imports) > MaxPublicImports || len(m.Globals) > MaxPublicGlobals {
		return false, unsupported("SWAR selector decoded count bound")
	}
	remaining := MaxPublicCodeBytes
	for _, fn := range m.Code {
		if len(fn.BodyBytes) > remaining {
			return false, unsupported("SWAR selector instruction/data size bound")
		}
		remaining -= len(fn.BodyBytes)
	}
	for _, data := range m.Data {
		if len(data.Init) > remaining {
			return false, unsupported("SWAR selector instruction/data size bound")
		}
		remaining -= len(data.Init)
	}
	return moduleUsesSWAR(m)
}
