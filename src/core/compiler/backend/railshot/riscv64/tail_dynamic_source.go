//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Private declared-type source stage. Public source/codec/runtime admission
// remains closed until address-free plans and entry/resume leases are connected.
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
