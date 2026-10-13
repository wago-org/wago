//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Permission is not an ABI witness. Only actual defined bindings select this
// profile; unused reference control types and ordinary modules keep their ABI.
// Directory bounds precede this allocation-free selection pass.
func hasDefinedReferenceFunctions(m *wasm.Module) (bool, error) {
	if m == nil || len(m.Code) > MaxFunctions || len(m.Types) > MaxFunctions || len(m.FuncTypes) != len(m.Code) {
		return false, unsupported("closed reference selection directory bound")
	}
	references := admitMutableFunctionGlobalLocalCall(m)
	for _, index := range m.FuncTypes {
		if index.Rec || uint64(index.Index) >= uint64(len(m.Types)) || len(m.Types[index.Index].SubTypes) != 1 {
			return false, unsupported("closed reference selection function binding")
		}
		sig := &m.Types[index.Index].SubTypes[0].Comp
		if sig.Kind != wasm.CompFunc || len(sig.Params) > 256 || len(sig.Results) > 256 {
			return false, unsupported("closed reference selection signature bound")
		}
		for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
			for _, v := range list {
				references = references || v.Kind() == wasm.ValRef
			}
		}
	}
	return references, nil
}

func admitClosedReferenceProductionOptions(opts CompileOptions) error {
	if !opts.NativeReferenceFunctions || !opts.referenceFunctions || !opts.Interruptible || !opts.ForeignTableCalls || !opts.InvocationContext || opts.importTailCalls && !opts.localTailCalls || opts.dynamicTailCalls && (!opts.NativeTailCalls || !opts.importTailCalls) || opts.localTailCalls != opts.NativeTailCalls || opts.Profile || opts.SourceMaps || opts.UnwindMaps {
		return unsupported("closed reference staging requires cooperative local-call production")
	}
	if opts.NativeTailCalls && opts.tailPlans == nil || !opts.importTailCalls && opts.tailPlans != nil && len(*opts.tailPlans) != 0 || (opts.referenceTailInstructions || opts.indirectTailInstructions) != opts.dynamicTailCalls || opts.referenceTailInstructions && !opts.NativeReferenceCalls || opts.referenceCallInstructions && !opts.NativeReferenceCalls {
		return unsupported("closed reference staging requires exact tail/call protocols")
	}
	return nil
}

// This is the sole staging entrance for the closed function ABI. It rechecks
// source confinement and the complete emitted mode before any image allocation.
// Legacy staging and isolated instantiation continue to reject private images.
func stageClosedReferenceProductionModule(c *Compiled, opts CompileOptions, m *wasm.Module, vector bool) (*enc.CompiledModule, error) {
	if err := admitClosedReferenceProductionOptions(opts); err != nil {
		return nil, err
	}
	if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
		return nil, err
	}
	if opts.NativeTailCalls && opts.tailModule != m || !opts.NativeTailCalls && opts.tailModule != nil {
		return nil, unsupported("closed reference staged source identity")
	}
	tails, indirect, calls, control, err := admitProductionDynamicTails(m, opts.NativeReferenceCalls, opts.dynamicTailCalls, vector, true)
	if err != nil {
		return nil, err
	}
	if opts.referenceTailInstructions != tails || opts.indirectTailInstructions != indirect || opts.dynamicTailCalls != (tails || indirect) || opts.referenceCallInstructions != calls || opts.referenceControlInstructions != control {
		return nil, unsupported("closed reference staged call/control witness")
	}
	if c == nil || !c.state.referenceFunctions || c.state.importedFunctions != m.ImportedFuncCount() || len(c.functions) != len(m.Code) || len(c.code) == 0 || len(c.code) > MaxCodeBytes {
		return nil, unsupported("closed reference staged function extent")
	}
	wantPublicReferenceCarriers := !vector && admitPublicReferenceCarrierBoundary(m) && publicReferenceCarrierTarget(&m.Types[0].SubTypes[0].Comp)
	if c.state.publicReferenceCarriers != wantPublicReferenceCarriers {
		return nil, unsupported("closed reference staged public argument protocol")
	}
	for _, f := range c.functions {
		mode := f.mode
		if !mode.referenceFunctions || !mode.production || !mode.interruptible || !mode.foreignTableCalls || !mode.invocationContext || mode.importTailCalls != opts.importTailCalls || mode.dynamicTailCalls != opts.dynamicTailCalls || mode.localTailCalls != opts.NativeTailCalls || mode.referenceCalls != opts.NativeReferenceCalls || mode.profileMode != (profileMode{}) || f.FrameHeaderBytes != 16 && f.FrameHeaderBytes != callFrameHeader {
			return nil, unsupported("closed reference staged function mode")
		}
	}
	if c.state.importTailCalls != opts.importTailCalls || c.state.localTailCalls != opts.localTailCalls || c.state.dynamicTailCalls != opts.dynamicTailCalls {
		return nil, unsupported("closed reference staged tail state")
	}
	if err := admitClosedReferenceImportTailPlans(c, opts, m, vector); err != nil {
		return nil, err
	}
	return stageAdmittedProductionModule(c, opts)
}

// Selected private frames may be sources without becoming descriptor targets.
// Recheck symbolic direct-import/reference authority against finalized frames and
// validated result types before common staging can allocate or publish an image.
func admitClosedReferenceImportTailPlans(c *Compiled, opts CompileOptions, m *wasm.Module, vector bool) error {
	if opts.importTailCalls != c.referenceImportTailsFinalized {
		return unsupported("closed reference imported-tail finalization mode")
	}
	if opts.tailPlans == nil {
		if len(c.finalizedReferenceImportTails) != 0 {
			return unsupported("closed reference imported-tail missing directory")
		}
		return nil
	} // Non-tail modules were checked above.
	// Persisted production plans and dispatcher rows start at zero. The private
	// multi-image lowering helper has a separate enclosing-owner site contract.
	if opts.tailSiteBase != 0 || len(*opts.tailPlans) > tailRAMaxSites {
		return unsupported("closed reference imported-tail site bound")
	}
	if opts.importTailCalls && !c.referenceImportTailsFinalized || !slices.Equal(*opts.tailPlans, c.finalizedReferenceImportTails) {
		return unsupported("closed reference imported-tail finalized directory")
	}
	values := 0
	for i, p := range *opts.tailPlans {
		local := int(p.functionIndex) - c.state.importedFunctions
		if !opts.importTailCalls || opts.tailModule != m || p.site != opts.tailSiteBase+uint32(i) || local < 0 || local >= len(c.functions) || uint64(p.sourcePC) < uint64(m.Code[local].LocalDeclBytes) || uint64(p.sourcePC)-uint64(m.Code[local].LocalDeclBytes) >= uint64(len(m.Code[local].BodyBytes)) {
			return unsupported("closed reference tail source identity")
		}
		source := &m.Types[m.FuncTypes[local].Index].SubTypes[0].Comp
		var target *wasm.CompType
		opcode, immediate := byte(0x12), p.importIndex
		switch p.kind {
		case enc.NativeTailDirectImport:
			if p.typeIndex != 0 || p.referenceBase != 0 || p.importIndex >= uint32(c.state.importedFunctions) {
				return unsupported("closed reference direct tail canonical fields")
			}
			var ok bool
			target, ok = m.FuncSignature(p.importIndex)
			if !ok {
				return unsupported("closed reference imported-tail target binding")
			}
		case enc.NativeTailIndirect:
			if !opts.dynamicTailCalls || !opts.indirectTailInstructions || p.importIndex != 0 || len(m.Tables) != 1 && !admitOwnedTwoTable64IndirectTail(m, true, true) && !admitMixedTable64ActiveIndirectTail(m, true, true) && !admitImportedTable64IndirectTail(m, true, true) || uint64(p.typeIndex) >= uint64(len(m.Types)) {
				return unsupported("closed reference numeric indirect-tail binding")
			}
			target = &m.Types[p.typeIndex].SubTypes[0].Comp
			opcode, immediate = 0x13, p.typeIndex
		case enc.NativeTailReference:
			if !opts.dynamicTailCalls || !opts.referenceTailInstructions || p.importIndex != 0 || uint64(p.typeIndex) >= uint64(len(m.Types)) {
				return unsupported("closed reference numeric reference-tail binding")
			}
			target = &m.Types[p.typeIndex].SubTypes[0].Comp
			opcode, immediate = 0x15, p.typeIndex
		default:
			return unsupported("closed reference tail kind")
		}
		if err := admitReferenceTailResults(source, target); err != nil {
			return err
		}
		if vector {
			if err := swarImportSignature(target); err != nil {
				return err
			}
		} else if err := integerNumericSignature(target, true); err != nil {
			publicReferenceTail := p.kind == enc.NativeTailReference && ((admitPublicReferenceCallBoundary(m) && p.typeIndex == 0) || (admitPublicDynamicHostReferenceBoundary(m) && p.typeIndex == 2) || (admitMutableFunctionGlobalReferenceCarrierCall(m) && p.typeIndex == 0))
			publicHostTail := p.kind == enc.NativeTailDirectImport && admitPublicHostReferenceBoundary(m) && p.importIndex == 0
			if (!publicReferenceTail && !publicHostTail) || !publicReferenceCarrierTarget(target) {
				return err
			}
		}
		values += len(target.Params) + len(target.Results)
		if err := NativeTailMetadataBudget(len(*opts.tailPlans), values); err != nil {
			return err
		}
		r := wasm.ReaderFrom(m.Code[local].BodyBytes[uint64(p.sourcePC)-uint64(m.Code[local].LocalDeclBytes):])
		op, err := r.Byte()
		if err != nil || op != opcode {
			return unsupported("closed reference tail source opcode")
		}
		targetIndex, err := r.U32()
		if err != nil || targetIndex != immediate {
			return unsupported("closed reference tail source immediate")
		}
		if p.kind == enc.NativeTailIndirect {
			tableIndex, err := r.U32()
			if err != nil || tableIndex != 0 && !admitOwnedTwoTable64IndirectTail(m, true, true) && !admitMixedTable64ActiveIndirectTail(m, true, true) && !admitImportedTable64IndirectTail(m, true, true) {
				return unsupported("closed reference tail source table immediate")
			}
		}
		f := c.functions[local]
		end := len(c.code)
		if local+1 < len(c.functions) {
			end = c.functions[local+1].Entry
		}
		if f.Entry < 0 || end <= f.Entry || end > len(c.code) || f.FrameHeaderBytes != callFrameHeader || f.mode.functionIndex != p.functionIndex || f.FrameBytes < callFrameHeader || f.FrameBytes > MaxFrameBytes || f.FrameBytes&15 != 0 || p.sourceStride != 8 && p.sourceStride != 16 || (p.sourceStride == 16) != vector || p.sourceBase < callFrameHeader || p.sourceBase > f.FrameBytes || p.sourceBase&7 != 0 || len(target.Params) > (f.FrameBytes-p.sourceBase)/p.sourceStride {
			return unsupported("closed reference imported-tail finalized source homes")
		}
		if p.kind != enc.NativeTailDirectImport && (p.referenceBase < callFrameHeader || p.referenceBase&7 != 0 || p.referenceBase > f.FrameBytes-8 || p.referenceBase < p.sourceBase+len(target.Params)*p.sourceStride) {
			return unsupported("closed reference tail full-width nonoverlapping target home")
		}
		frame, err := NativeFrameBytes(c.code[:end], f.Entry)
		if err != nil || frame != f.FrameBytes {
			return unsupported("closed reference imported-tail finalized frame extent")
		}
	}
	return nil
}
