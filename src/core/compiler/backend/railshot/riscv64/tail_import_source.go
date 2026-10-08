//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Private source milestone. These symbolic plans are never serialized and do
// not enable public TailCall. Each separately compiled image receives an
// explicit base in the enclosing owner's flat capability table.
type tailImportPlan struct {
	site, functionIndex, importIndex, sourcePC uint32
	sourceBase, sourceStride                   int
}

type tailImportImage struct {
	code       []byte
	functions  []FunctionInfo
	signatures []*wasm.CompType // imports followed by definitions, owned decode
	plans      []tailImportPlan
	siteBase   uint32
	imports    int
}

func (f *integerEmitter) importTailCall(index uint32, sig *wasm.CompType, base int, spill func(int) int32, stride int) {
	if !f.mode.importTailCalls || f.mode.tailPlans == nil || !f.admitLocalTailFrame() || spill == nil || sig == nil {
		f.err = unsupported("private imported-tail emitter contract")
		return
	}
	n := len(*f.mode.tailPlans)
	if f.mode.tailSiteBase > tailRAMaxSites || n >= tailRAMaxSites-int(f.mode.tailSiteBase) {
		f.err = unsupported("private imported-tail site bound")
		return
	}
	p := tailImportPlan{site: f.mode.tailSiteBase + uint32(n), functionIndex: f.mode.functionIndex, importIndex: index, sourcePC: f.sourcePC, sourceBase: int(spill(base)), sourceStride: stride}
	// The source homes must fit the measured frame before any dispatch bytes.
	if (stride != 8 && stride != 16) || p.sourceBase < callFrameHeader || p.sourceBase > f.frameBytes || len(sig.Params) > (f.frameBytes-p.sourceBase)/stride {
		f.err = unsupported("private imported-tail source homes")
		return
	}
	*f.mode.tailPlans = append(*f.mode.tailPlans, p)
	f.tailRAProofCall(p.site, sig, base, spill)
}

// Admission is whole-module and state-free. Real source decoding/validation is
// followed by existing scalar/SWAR prewalks before guest code generation. The
// caller cannot obtain an image from a partially admitted hidden function.
func compileTailImportSource(source []byte, siteBase uint32, forceSWAR bool) (*tailImportImage, error) {
	if len(source) < 8 || len(source) > MaxModuleBytes || siteBase > tailRAMaxSites {
		return nil, unsupported("private imported-tail source/base bound")
	}
	owned := bytes.Clone(source)
	if err := admitSWARWirePolicy(owned, swarModuleHosts); err != nil {
		return nil, err
	}
	d, err := wasm.DecodeModuleByteBackedWithLimits(owned, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: MaxModuleBytes})
	if err != nil {
		return nil, err
	}
	m := d.Module
	if len(m.Code) == 0 || len(m.Code) > MaxFunctions || len(m.Types) > MaxFunctions || len(m.Imports) > MaxHostFunctionImports || len(m.Exports) > MaxFunctions || m.UsesCompactImports || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Data) != 0 || len(m.Elements) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil || len(m.BranchHints) != 0 {
		return nil, unsupported("private imported-tail state-free module")
	}
	for _, im := range m.Imports {
		if im.Type.Kind != wasm.ExternFunc {
			return nil, unsupported("private imported-tail function imports only")
		}
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind != wasm.ExternFunc || ex.Index.Index < uint32(len(m.Imports)) || uint64(ex.Index.Index) >= uint64(len(m.Imports)+len(m.Code)) {
			return nil, unsupported("private imported-tail defined exports only")
		}
	}
	for _, rec := range m.Types {
		if len(rec.SubTypes) != 1 || !rec.SubTypes[0].Final || rec.SubTypes[0].HasPrefix || len(rec.SubTypes[0].Supers) != 0 || rec.SubTypes[0].Metadata.Describes.Present() || rec.SubTypes[0].Metadata.Descriptor.Present() {
			return nil, unsupported("private imported-tail type metadata")
		}
		if err := tailRASignature(&rec.SubTypes[0].Comp); err != nil {
			return nil, err
		}
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		return nil, err
	}
	i := &tailImportImage{siteBase: siteBase, imports: len(m.Imports)}
	for n := range m.Imports {
		sig, ok := m.ImportFuncType(n)
		if !ok {
			return nil, unsupported("private imported-tail import type")
		}
		i.signatures = append(i.signatures, sig)
	}
	for n := range m.Code {
		sig, ok := m.LocalFuncType(n)
		if !ok {
			return nil, unsupported("private imported-tail defined type")
		}
		i.signatures = append(i.signatures, sig)
	}
	vector, err := moduleUsesSWAR(m)
	if err != nil {
		return nil, err
	}
	if vector || forceSWAR {
		mode := emissionMode{production: true, interruptible: true, localTailCalls: true, importTailCalls: true, tailPlans: &i.plans, tailSiteBase: siteBase}
		c, err := compileSWARDecodedModule(m, mode, swarModuleHosts)
		if err != nil {
			return nil, err
		}
		i.code, i.functions = c.code, c.functions
	} else {
		opts := CompileOptions{production: true, hostImports: true, Interruptible: true, localTailCalls: true, importTailCalls: true, tailPlans: &i.plans, tailSiteBase: siteBase}
		c, err := compileDecodedModule(m, scalarRegisters[:], opts)
		if err != nil {
			return nil, err
		}
		i.code, i.functions = c.code, c.functions
	}
	return i, nil
}

type tailImportBinding struct {
	image     *tailImportImage
	functions []tailRAFunction
	targets   []tailRAFunction // exact import ordinal, authentic defined entries
}

func tailImportExactSignature(a, b *wasm.CompType) bool {
	if a == nil || b == nil || a.Kind != wasm.CompFunc || b.Kind != wasm.CompFunc || len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) {
		return false
	}
	for _, pair := range [][2][]wasm.ValType{{a.Params, b.Params}, {a.Results, b.Results}} {
		for j, typ := range pair[0] {
			if !wasm.EqualValType(typ, pair[1][j]) {
				return false
			}
		}
	}
	return true
}

// Validate the independently compiled site bases and original entries before
// assembling capability rows. Existing ordinary import thunks are not accepted
// as tail targets: their linked activation frames would accumulate.
func tailImportOwnerSpec(bindings []tailImportBinding, s tailRAOwnerSpec) (tailRAOwnerSpec, error) {
	if len(bindings) == 0 || len(bindings) > MaxFunctions || len(s.sites) != 0 {
		return tailRAOwnerSpec{}, unsupported("private imported-tail binding counts")
	}
	count := 0
	for _, b := range bindings {
		i := b.image
		if i == nil || i.siteBase != uint32(count) || len(i.functions) == 0 || len(i.functions) != len(b.functions) || i.imports != len(b.targets) || len(i.signatures) != i.imports+len(i.functions) || len(i.plans) > tailRAMaxSites-count {
			return tailRAOwnerSpec{}, unsupported("private imported-tail binding shape/base")
		}
		for n, f := range b.functions {
			info := i.functions[n]
			first := b.functions[0]
			if f.code != first.code || f.job != first.job || tailRAPointer(f.context) != tailRAPointer(first.context) || tailRAPointer(f.directory) != tailRAPointer(first.directory) || !bytes.Equal(f.snapshot, i.code) || f.info.Entry != info.Entry || f.info.CodeBytes != info.CodeBytes || f.info.FrameBytes != info.FrameBytes || f.info.FrameHeaderBytes != info.FrameHeaderBytes || f.info.Params != info.Params || f.info.Results != info.Results || f.info.mode.functionIndex != info.mode.functionIndex || !tailImportExactSignature(f.signature, i.signatures[i.imports+n]) {
				return tailRAOwnerSpec{}, unsupported("private imported-tail frozen caller image")
			}
		}
		for n, p := range i.plans {
			local := int(p.functionIndex) - i.imports
			if p.site != uint32(count+n) || local < 0 || local >= len(b.functions) || uint64(p.importIndex) >= uint64(len(b.targets)) || !tailImportExactSignature(b.targets[p.importIndex].signature, i.signatures[p.importIndex]) {
				return tailRAOwnerSpec{}, unsupported("private imported-tail symbolic target/site")
			}
			s.sites = append(s.sites, tailRAOwnerSite{caller: b.functions[local], target: b.targets[p.importIndex], sourcePC: p.sourcePC, sourceBase: p.sourceBase, sourceStride: p.sourceStride})
		}
		count += len(i.plans)
	}
	if count == 0 {
		return tailRAOwnerSpec{}, unsupported("private imported-tail no live sites")
	}
	if _, _, err := tailRAOwnedBound(s); err != nil {
		return tailRAOwnerSpec{}, fmt.Errorf("private imported-tail owner: %w", err)
	}
	return s, nil
}
