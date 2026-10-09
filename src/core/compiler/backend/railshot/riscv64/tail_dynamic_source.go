//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Declared-type indirect source lowering. The production qualification profile
// binds address-free indirect/reference plans through the exact entry owner
// lease, including certified native imports. Host descriptor rows stay closed.
func (f *integerEmitter) dynamicIndirectTailCall(typeIndex uint32, sig *wasm.CompType, key uint64, base, indexSlot int, spill func(int) int32, stride int) {
	if !f.mode.dynamicTailCalls || !f.mode.importTailCalls || !f.mode.invocationContext || !f.mode.foreignTableCalls || f.mode.tailPlans == nil || !f.admitLocalTailFrame() || sig == nil || spill == nil || typeIndex >= MaxFunctions {
		f.err = unsupported("private dynamic indirect source contract")
		return
	}
	n := len(*f.mode.tailPlans)
	if f.mode.tailSiteBase > tailRAMaxSites || n >= tailRAMaxSites-int(f.mode.tailSiteBase) {
		f.err = unsupported("private dynamic indirect site bound")
		return
	}
	p := tailImportPlan{kind: 1, typeIndex: typeIndex, referenceBase: int(spill(indexSlot)), site: f.mode.tailSiteBase + uint32(n), functionIndex: f.mode.functionIndex, sourcePC: f.sourcePC, sourceBase: int(spill(base)), sourceStride: stride}
	if (stride != 8 && stride != 16) || p.sourceBase < callFrameHeader || p.sourceBase > f.frameBytes || len(sig.Params) > (f.frameBytes-p.sourceBase)/stride || p.referenceBase < callFrameHeader || p.referenceBase > f.frameBytes-8 || p.referenceBase&7 != 0 || p.referenceBase < p.sourceBase+len(sig.Params)*stride {
		f.err = unsupported("private dynamic reference/argument homes")
		return
	}
	// Table storage belongs to the retained invocation context. Read only the
	// bounded row and full-width canonical token; the factory establishes token
	// membership before dereferencing any candidate descriptor or producer.
	f.Load(5, spill(indexSlot), false)
	f.a.Ld(enc.X6, enc.X25, -80)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Lwu(enc.X7, enc.X6, 0)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.GEU, enc.X5, enc.X7))
	f.a.Slli(enc.X5, enc.X5, 5)
	f.a.Add(enc.X6, enc.X6, enc.X5)
	f.a.Ld(enc.X7, enc.X6, 8)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X7, enc.Zero))
	f.a.Ld(enc.X7, enc.X6, 16)
	f.a.Constant64(enc.X5, int64(key))
	f.trap(trapIndirectSignature, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	f.a.Ld(enc.X14, enc.X6, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X14, enc.Zero))
	// This i32 index home is eight bytes physically. Sd deliberately preserves
	// every reference bit; a narrow Store would truncate the selected address.
	f.a.Sd(enc.X14, enc.SP, int32(p.referenceBase))
	*f.mode.tailPlans = append(*f.mode.tailPlans, p)
	f.tailRAProofCall(p.site, sig, base, spill)
}

// A typed reference is already a full-width stack carrier. Its declared type
// remains separate from the source/target function namespace. Only the retained
// factory may authenticate or dereference the selected canonical descriptor.
func (f *integerEmitter) dynamicReferenceTailCall(typeIndex uint32, sig *wasm.CompType, base, referenceSlot int, spill func(int) int32, stride int) {
	if !f.mode.dynamicTailCalls || !f.mode.importTailCalls || !f.mode.invocationContext || !f.mode.foreignTableCalls || f.mode.tailPlans == nil || !f.admitLocalTailFrame() || sig == nil || spill == nil || typeIndex >= MaxFunctions {
		f.err = unsupported("private dynamic reference source contract")
		return
	}
	n := len(*f.mode.tailPlans)
	if f.mode.tailSiteBase > tailRAMaxSites || n >= tailRAMaxSites-int(f.mode.tailSiteBase) {
		f.err = unsupported("private dynamic reference site bound")
		return
	}
	p := tailImportPlan{kind: 2, typeIndex: typeIndex, referenceBase: int(spill(referenceSlot)), site: f.mode.tailSiteBase + uint32(n), functionIndex: f.mode.functionIndex, sourcePC: f.sourcePC, sourceBase: int(spill(base)), sourceStride: stride}
	if (stride != 8 && stride != 16) || p.sourceBase < callFrameHeader || p.sourceBase > f.frameBytes || len(sig.Params) > (f.frameBytes-p.sourceBase)/stride || p.referenceBase < callFrameHeader || p.referenceBase > f.frameBytes-8 || p.referenceBase&7 != 0 || p.referenceBase < p.sourceBase+len(sig.Params)*stride {
		f.err = unsupported("private dynamic typed reference/argument homes")
		return
	}
	// Maximum numeric transport can put this full-width home beyond the
	// instruction's signed 12-bit offset. Use the bounded frame address helper.
	f.Load(14, int32(p.referenceBase), true)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X14, enc.Zero))
	*f.mode.tailPlans = append(*f.mode.tailPlans, p)
	f.tailRAProofCall(p.site, sig, base, spill)
}

func compileDynamicTailSource(source []byte, siteBase uint32, forceSWAR bool) (*tailImportImage, error) {
	if len(source) < 8 || len(source) > MaxModuleBytes || siteBase > tailRAMaxSites {
		return nil, unsupported("private dynamic source/base bound")
	}
	owned := bytes.Clone(source)
	d, err := wasm.DecodeModuleByteBackedWithLimits(owned, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: MaxModuleBytes})
	if err != nil {
		return nil, err
	}
	m := d.Module
	if len(m.Code) == 0 || len(m.Code) > MaxFunctions || len(m.Types) > MaxFunctions || len(m.Imports) > MaxHostFunctionImports || len(m.Tables) != 1 || m.Tables[0].Init != nil || m.Tables[0].Type.Limits.Addr64 || !wasm.EqualValType(wasm.RefVal(m.Tables[0].Type.Ref), wasm.FuncRef) || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Data) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return nil, unsupported("private dynamic owned-table whole-module profile")
	}
	for _, im := range m.Imports {
		if im.Type.Kind != wasm.ExternFunc {
			return nil, unsupported("private dynamic function import profile")
		}
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind != wasm.ExternFunc || ex.Index.Index < uint32(len(m.Imports)) || uint64(ex.Index.Index) >= uint64(len(m.Imports)+len(m.Code)) {
			return nil, unsupported("private dynamic defined export profile")
		}
	}
	i := &tailImportImage{siteBase: siteBase, imports: len(m.Imports)}
	for _, rec := range m.Types {
		if len(rec.SubTypes) != 1 || !rec.SubTypes[0].Final || rec.SubTypes[0].HasPrefix || len(rec.SubTypes[0].Supers) != 0 || rec.SubTypes[0].Metadata.Describes.Present() || rec.SubTypes[0].Metadata.Descriptor.Present() {
			return nil, unsupported("private dynamic exact function types")
		}
		sig := &rec.SubTypes[0].Comp
		if err := tailRASignature(sig); err != nil {
			return nil, err
		}
		i.types = append(i.types, sig)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		return nil, err
	}
	for n := range m.Imports {
		sig, ok := m.ImportFuncType(n)
		if !ok {
			return nil, unsupported("private dynamic import signature")
		}
		i.signatures = append(i.signatures, sig)
	}
	for n := range m.Code {
		sig, ok := m.LocalFuncType(n)
		if !ok {
			return nil, unsupported("private dynamic defined signature")
		}
		i.signatures = append(i.signatures, sig)
	}
	vector, err := moduleUsesSWAR(m)
	if err != nil {
		return nil, err
	}
	if vector || forceSWAR {
		mode := emissionMode{production: true, interruptible: true, invocationContext: true, foreignTableCalls: true, localTailCalls: true, importTailCalls: true, dynamicTailCalls: true, tailPlans: &i.plans, tailSiteBase: siteBase}
		c, err := compileSWARDecodedModuleProfile(m, mode, swarModuleTables, swarStateProfile{publicInitializers: true, publicStarts: true, exports: true, importedTable: true, functionTableImports: true})
		if err != nil {
			return nil, err
		}
		i.code, i.functions = c.code, c.functions
	} else {
		opts := CompileOptions{production: true, publicStarts: true, hostImports: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, localTailCalls: true, importTailCalls: true, dynamicTailCalls: true, tailPlans: &i.plans, tailSiteBase: siteBase}
		c, err := compileDecodedModule(m, scalarRegisters[:], opts)
		if err != nil {
			return nil, err
		}
		i.code, i.functions = c.code, c.functions
	}
	return i, nil
}

// This stages only immutable symbolic metadata. Public compilation and native
// runtime admission still reject these dynamic plans until leases are wired.
func (i *tailImportImage) symbolicTailPlans() (*enc.NativeTailModule, error) {
	if i == nil || i.siteBase != 0 || len(i.plans) > tailRAMaxSites {
		return nil, unsupported("private symbolic tail image/count")
	}
	if _, _, err := NativeTailInstanceBudget(len(i.plans), len(i.code)); err != nil {
		return nil, err
	}
	values := 0
	for n, p := range i.plans {
		local := int(p.functionIndex) - i.imports
		if p.site != uint32(n) || local < 0 || local >= len(i.functions) || uint64(p.functionIndex) >= uint64(len(i.signatures)) || i.functions[local].FrameHeaderBytes != callFrameHeader || p.kind > enc.NativeTailReference {
			return nil, unsupported("private symbolic tail source frame/kind")
		}
		var sig *wasm.CompType
		if p.kind == enc.NativeTailDirectImport {
			if uint64(p.importIndex) >= uint64(i.imports) || p.typeIndex != 0 || p.referenceBase != 0 {
				return nil, unsupported("private symbolic direct tail fields")
			}
			sig = i.signatures[p.importIndex]
		} else {
			if p.importIndex != 0 || uint64(p.typeIndex) >= uint64(len(i.types)) {
				return nil, unsupported("private symbolic declared tail type")
			}
			sig = i.types[p.typeIndex]
		}
		if err := tailRASignature(sig); err != nil {
			return nil, err
		}
		if err := admitSWARLocalTailSignature(i.signatures[p.functionIndex], sig); err != nil {
			return nil, err
		}
		frame := i.functions[local].FrameBytes
		if frame < callFrameHeader || frame > MaxFrameBytes || frame&15 != 0 || p.sourceBase < callFrameHeader || p.sourceBase > frame || p.sourceBase&7 != 0 || p.sourceStride != 8 && p.sourceStride != 16 || len(sig.Params) > (frame-p.sourceBase)/p.sourceStride {
			return nil, unsupported("private symbolic tail parameter homes")
		}
		for _, typ := range sig.Params {
			if wasm.EqualValType(typ, wasm.V128) && p.sourceStride != 16 {
				return nil, unsupported("private symbolic tail vector home")
			}
		}
		if p.kind != enc.NativeTailDirectImport && (p.referenceBase < p.sourceBase+len(sig.Params)*p.sourceStride || p.referenceBase > frame-8 || p.referenceBase&7 != 0) {
			return nil, unsupported("private symbolic tail reference home")
		}
		values += len(sig.Params) + len(sig.Results)
		if err := NativeTailMetadataBudget(len(i.plans), values); err != nil {
			return nil, err
		}
	}
	m := &enc.NativeTailModule{Plans: make([]enc.NativeTailPlan, len(i.plans))}
	for n, p := range i.plans {
		m.Plans[n] = enc.NativeTailPlan{Kind: p.kind, TypeIndex: p.typeIndex, ReferenceBase: p.referenceBase, FunctionIndex: p.functionIndex, ImportIndex: p.importIndex, SourcePC: p.sourcePC, FrameBytes: i.functions[int(p.functionIndex)-i.imports].FrameBytes, SourceBase: p.sourceBase, SourceStride: p.sourceStride}
	}
	return m, nil
}
