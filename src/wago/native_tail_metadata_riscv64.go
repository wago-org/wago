//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"sync/atomic"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Qualification only. Public feature defaults and SupportedFeatures stay
// closed until all tail instruction/target families have been qualified.
type railshotRuntimeConfigState struct{ tailProducts bool }
type railshotTailCompiledState struct {
	sourceCodeLimit                                  uint64 // immutable compile-origin policy; never serialized
	module                                           *encoder.NativeTailModule
	retired                                          []*railshot.NativeTailDispatcher // codeCache.mu protects failed cleanup
	retiredGenerations                               []rv64RetiredTailGeneration
	retiredGenerationCode, retiredGenerationMetadata int
	// Mapping-owned exact logical span, independent of readable execution views
	// and page-rounded cleanup storage. Set once at mapping under codeCache.mu.
	mappedCodeBytes int
}

func railshotRecordMappedCodeBytes(c *Compiled, n int) {
	// Called with the cache lock, or by the exclusive unpublished compiler/
	// decoder owner. Hand-built modules may not have a validation memo yet.
	if c.loadValidateMemo() == nil {
		memo := new(validateMemo)
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&c.validateMemo)), unsafe.Pointer(memo))
	}
	c.ensureCompileIndexes().railshotTail.mappedCodeBytes = n
}

func railshotStagedTailFeatures(c *RuntimeConfig) CoreFeatures {
	if c != nil && c.railshotState.tailProducts {
		return CoreFeatureTailCall | CoreFeatureTypedFunctionReferences
	}
	return 0
}
func railshotConfigureNativeTail(required CoreFeatures, cfg *RuntimeConfig, opts *railshotCompileOptions) {
	opts.NativeTailCalls = required.IsEnabled(CoreFeatureTailCall)
	opts.NativeReferenceCalls = required.IsEnabled(CoreFeatureTypedFunctionReferences)
	opts.NativeReferenceFunctions = cfg != nil && cfg.railshotState.tailProducts
}
func rv64NativeTailModule(c *Compiled) *encoder.NativeTailModule {
	if x := c.loadCompileIndexes(); x != nil {
		return x.railshotTail.module
	}
	return nil
}
func rv64StoreNativeTailModule(c *Compiled, m *encoder.NativeTailModule) {
	if c.validateMemo == nil {
		c.validateMemo = &validateMemo{}
	}
	c.ensureCompileIndexes().railshotTail.module = m
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTailCall
}

func rv64HasReferenceTail(c *Compiled) bool {
	if m := rv64NativeTailModule(c); m != nil {
		for _, p := range m.Plans {
			if p.Kind == encoder.NativeTailReference {
				return true
			}
		}
	}
	return false
}
func railshotStoreNativeTail(c *Compiled, image *railshotCompiledModule, cfg *RuntimeConfig) error {
	if cfg != nil {
		c.ensureCompileIndexes().railshotTail.sourceCodeLimit = cfg.maxNativeCodeBytes
	}
	c.nativeReferenceCallABI = image.NativeReferenceCalls
	c.nativeReferenceControlABI = image.NativeReferenceControl
	c.nativeReferenceFunctionABI = image.NativeReferenceFunctions
	if image.NativeTail == nil {
		return nil
	}
	m := &encoder.NativeTailModule{Plans: slices.Clone(image.NativeTail.Plans)}
	rv64StoreNativeTailModule(c, m)
	return nil // finalized signatures are assigned next; validation precedes publication
}

// Validation borrows frozen metadata. It does not allocate converted type
// slices per site, including for repeated declared signatures near the budget.
type rv64TailSignatureView struct {
	params, results []ValType
	declared        *DefinedTypeDescriptor
}

func rv64PublicReferenceTailPlan(c *Compiled, p encoder.NativeTailPlan) bool {
	if p.Kind != encoder.NativeTailReference || p.TypeIndex != 0 || !rv64PublicReferenceCallBoundaryWithPlan(c, false) {
		return false
	}
	wantPC := uint32(1)
	for i := 0; i <= len(c.Funcs[0].Params); i++ {
		wantPC += 1 + rv64PublicReferenceU32Bytes(uint32(i))
	}
	// The canonical wrapper has N+1 parameters and a peak stack of N+1:
	// its argument homes begin after the fixed header and local homes, the
	// target reference immediately follows the N carrier homes, and the full
	// frame remains 16-byte aligned.
	n := len(c.Funcs[0].Params)
	return p.FunctionIndex == 2 && p.ImportIndex == 0 && p.SourcePC == wantPC && p.FrameBytes == 64+16*n && p.SourceBase == 56+8*n && p.SourceStride == 8 && p.ReferenceBase == 56+16*n
}

func rv64PublicReferenceTailDeclaredCarrier(v ValueTypeDescriptor, referenceCarriers bool) bool {
	if v.Kind <= ValueTypeV128 {
		return v.Ref == (ReferenceTypeDescriptor{})
	}
	return referenceCarriers && v.Kind == ValueTypeReference && !v.Ref.Exact && v.Ref.Nullable && !v.Ref.Heap.Defined && (v.Ref.Heap.Abstract == AbstractHeapFunc || v.Ref.Heap.Abstract == AbstractHeapExtern)
}

func (s rv64TailSignatureView) paramCount() int {
	if s.declared != nil {
		return len(s.declared.Params)
	}
	return len(s.params)
}
func (s rv64TailSignatureView) resultCount() int {
	if s.declared != nil {
		return len(s.declared.Results)
	}
	return len(s.results)
}
func (s rv64TailSignatureView) param(i int) ValType {
	if s.declared != nil {
		v, _ := s.declared.Params[i].ABIType(nil)
		return v
	}
	return s.params[i]
}
func (s rv64TailSignatureView) result(i int) ValType {
	if s.declared != nil {
		v, _ := s.declared.Results[i].ABIType(nil)
		return v
	}
	return s.results[i]
}
func rv64NativeTailPlanSignature(c *Compiled, p encoder.NativeTailPlan) (rv64TailSignatureView, error) {
	if p.Kind == encoder.NativeTailDirectImport {
		if uint64(p.ImportIndex) >= uint64(len(c.importFuncSigs)) || p.TypeIndex != 0 || p.ReferenceBase != 0 {
			return rv64TailSignatureView{}, fmt.Errorf("RV64 direct tail target/canonical fields")
		}
		sig := c.importFuncSigs[p.ImportIndex]
		return rv64TailSignatureView{params: sig.Params, results: sig.Results}, nil
	}
	if p.Kind != encoder.NativeTailIndirect && p.Kind != encoder.NativeTailReference || p.ImportIndex != 0 || uint64(p.TypeIndex) >= uint64(len(c.Types)) || !c.needsFuncRefDescs() {
		return rv64TailSignatureView{}, fmt.Errorf("RV64 dynamic tail declared type/kind/descriptor contract")
	}
	if p.Kind == encoder.NativeTailIndirect && (c.tableCount() != 1 || c.TableAddr64) && !rv64OwnedTable64IndirectTailPlanShape(c) && !rv64OwnedTwoTable64IndirectTail(c) && !rv64MixedTable64ActiveIndirectTailPlanShape(c) && !rv64ImportedTable64IndirectTailPlanShape(c) {
		return rv64TailSignatureView{}, fmt.Errorf("RV64 dynamic tail table shape")
	}
	d := &c.Types[p.TypeIndex]
	if d.Kind != CompositeTypeFunction || !d.Final || len(d.Supers) != 0 || d.HasDescribes || d.HasDescriptor || len(d.Params) > 256 || len(d.Results) > 256 {
		return rv64TailSignatureView{}, fmt.Errorf("RV64 dynamic tail plain declared function type")
	}
	referenceCarriers := rv64PublicReferenceTailPlan(c, p) && rv64PublicReferenceCarrierTarget(c, 0)
	for _, list := range [][]ValueTypeDescriptor{d.Params, d.Results} {
		slots := 0
		for _, v := range list {
			if !rv64PublicReferenceTailDeclaredCarrier(v, referenceCarriers) {
				return rv64TailSignatureView{}, fmt.Errorf("RV64 dynamic tail numeric declared signature")
			}
			slots++
			if v.Kind == ValueTypeV128 {
				slots++
			}
		}
		if slots > 256 {
			return rv64TailSignatureView{}, fmt.Errorf("RV64 dynamic tail physical slot bound")
		}
	}
	return rv64TailSignatureView{declared: d}, nil
}
func rv64ValidateNativeTail(c *Compiled) error {
	m := rv64NativeTailModule(c)
	if c.requiredFeatures.IsEnabled(CoreFeatureTailCall) != (m != nil) {
		return fmt.Errorf("RV64 native tail feature/plan contract")
	}
	if m == nil {
		return nil
	}
	if rv64HasReferenceTail(c) && !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
		return fmt.Errorf("RV64 reference tail requires typed function references")
	}
	return rv64ValidateNativeTailPlans(c, m.Plans)
}

// Shared by frozen source/artifact admission and entry generation construction.
func rv64ValidateNativeTailPlans(c *Compiled, plans []encoder.NativeTailPlan) error {
	return rv64ValidateNativeTailPlansWithCodeBytes(c, plans, len(c.code))
}

// Runtime callers supply an authenticated retained image extent. Closing an
// execution snapshot may clear c.code without retiring that physical mapping.
func rv64ValidateNativeTailPlansWithCodeBytes(c *Compiled, plans []encoder.NativeTailPlan, codeBytes int) error {
	if !c.nativeContextTrapABI || len(plans) > 4096 {
		return fmt.Errorf("RV64 native tail context/count contract")
	}
	if _, _, err := railshot.NativeTailInstanceBudget(len(plans), codeBytes); err != nil {
		return err
	}
	values := 0
	for _, p := range plans {
		local := int(p.FunctionIndex) - c.NumImports
		if local < 0 || local >= len(c.Funcs) || p.FrameBytes < 48 || p.FrameBytes > railshot.MaxFrameBytes || p.FrameBytes&15 != 0 || p.SourceBase < 48 || p.SourceBase > p.FrameBytes || p.SourceBase&7 != 0 || p.SourceStride != 8 && p.SourceStride != 16 {
			return fmt.Errorf("RV64 native tail source frame/index contract")
		}
		sig, err := rv64NativeTailPlanSignature(c, p)
		if err != nil {
			return err
		}
		if sig.paramCount() > 256 || sig.resultCount() > 256 {
			return fmt.Errorf("RV64 native tail signature count")
		}
		referenceCarriers := rv64PublicReferenceTailPlan(c, p) && rv64PublicReferenceCarrierTarget(c, 0)
		for _, result := range []bool{false, true} {
			count, slots := sig.paramCount(), 0
			if result {
				count = sig.resultCount()
			}
			for n := 0; n < count; n++ {
				v := sig.param
				if result {
					v = sig.result
				}
				switch v(n) {
				case ValI32, ValI64, ValF32, ValF64:
					slots++
				case ValFuncRef, ValExternRef:
					if !referenceCarriers {
						return fmt.Errorf("RV64 native tail numeric signature")
					}
					slots++
				case ValV128:
					slots += 2
				default:
					return fmt.Errorf("RV64 native tail numeric signature")
				}
			}
			if slots > 256 {
				return fmt.Errorf("RV64 native tail physical slots")
			}
		}
		values += sig.paramCount() + sig.resultCount()
		if err := railshot.NativeTailMetadataBudget(len(plans), values); err != nil {
			return err
		}
		if len(c.Funcs[local].Results) != sig.resultCount() {
			return fmt.Errorf("RV64 native tail result count")
		}
		for i, v := range c.Funcs[local].Results {
			if v != sig.result(i) {
				return fmt.Errorf("RV64 native tail result contract")
			}
		}
		at := p.SourceBase
		for i := 0; i < sig.paramCount(); i++ {
			width := 8
			switch sig.param(i) {
			case ValI32, ValI64, ValF32, ValF64:
			case ValFuncRef, ValExternRef:
				if !referenceCarriers {
					return fmt.Errorf("RV64 native tail parameter type")
				}
			case ValV128:
				width = 16
				if p.SourceStride != 16 {
					return fmt.Errorf("RV64 native tail pair home")
				}
			default:
				return fmt.Errorf("RV64 native tail parameter type")
			}
			if at > p.FrameBytes-width {
				return fmt.Errorf("RV64 native tail source home extent")
			}
			at += p.SourceStride
		}
		if p.Kind != encoder.NativeTailDirectImport && (p.ReferenceBase < 48 || p.ReferenceBase&7 != 0 || p.ReferenceBase > p.FrameBytes-8 || p.ReferenceBase < at) {
			return fmt.Errorf("RV64 dynamic tail full-width reference home")
		}
	}
	return nil
}

// RV64 architecture 4 + required TailCall determines this appendix. It has no
// addresses, and occupies a fixed position before GC descriptors/root maps.
func railshotWriteNativeTail(w *compiledWriter, c *Compiled) error {
	if err := rv64ValidateNativeTail(c); err != nil {
		return err
	}
	m := rv64NativeTailModule(c)
	if m == nil {
		return nil
	}
	version := uint32(1)
	for _, p := range m.Plans {
		if p.Kind != encoder.NativeTailDirectImport {
			version = 2
			break
		}
	}
	w.u32(version)
	w.uvar(uint64(len(m.Plans)))
	for _, p := range m.Plans {
		if version == 2 {
			w.u32(p.Kind)
			w.u32(p.TypeIndex)
			w.u32(uint32(p.ReferenceBase))
		}
		w.u32(p.FunctionIndex)
		w.u32(p.ImportIndex)
		w.u32(p.SourcePC)
		w.u32(uint32(p.FrameBytes))
		w.u32(uint32(p.SourceBase))
		w.u32(uint32(p.SourceStride))
	}
	return nil
}
func railshotReadNativeTail(r *compiledReader, c *Compiled) error {
	if !c.requiredFeatures.IsEnabled(CoreFeatureTailCall) {
		return nil
	}
	v, err := r.u32()
	if err != nil {
		return err
	}
	if v != 1 && v != 2 {
		return fmt.Errorf("RV64 native tail plan version %d", v)
	}
	wire := 24
	if v == 2 {
		wire = 36
	}
	n, err := r.countElements("RV64 native tail plans", wire, unsafe.Sizeof(encoder.NativeTailPlan{}))
	if err != nil {
		return err
	}
	if n > 4096 {
		return fmt.Errorf("RV64 native tail plan count")
	}
	m := &encoder.NativeTailModule{Plans: make([]encoder.NativeTailPlan, n)}
	for i := range m.Plans {
		var kind, typeIndex, referenceBase uint32
		if v == 2 {
			kind, err = r.u32()
			if err != nil {
				return err
			}
			typeIndex, err = r.u32()
			if err != nil {
				return err
			}
			referenceBase, err = r.u32()
			if err != nil {
				return err
			}
		}
		var fields [6]uint32
		for j := range fields {
			fields[j], err = r.u32()
			if err != nil {
				return err
			}
		}
		m.Plans[i] = encoder.NativeTailPlan{Kind: kind, TypeIndex: typeIndex, ReferenceBase: int(referenceBase), FunctionIndex: fields[0], ImportIndex: fields[1], SourcePC: fields[2], FrameBytes: int(fields[3]), SourceBase: int(fields[4]), SourceStride: int(fields[5])}
	}
	rv64StoreNativeTailModule(c, m)
	if err := rv64ValidateNativeTail(c); err != nil {
		return err
	}
	// Decoder-owned, unpublished state. Source compilation derives this marker
	// from validated Wasm requirements; warm validation never mutates it.
	if rv64HasReferenceTail(c) {
		c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences
	}
	return nil
}
func rv64HasDynamicTail(m *encoder.NativeTailModule) bool {
	if m != nil {
		for _, p := range m.Plans {
			if p.Kind != encoder.NativeTailDirectImport {
				return true
			}
		}
	}
	return false
}

// Mixed sites share one entry-owned generation and one capability slot.
func rv64StaticTailPlanCount(m *encoder.NativeTailModule) int {
	if m == nil || rv64HasDynamicTail(m) {
		return 0
	}
	n := 0
	for _, p := range m.Plans {
		if p.Kind == encoder.NativeTailDirectImport {
			n++
		}
	}
	return n
}

func railshotNativeTailArenaBytes(c *Compiled) int {
	bytes := rv64TailTargetsArenaBytes(c)
	if n := rv64StaticTailPlanCount(rv64NativeTailModule(c)); n != 0 {
		_, arena, err := railshot.NativeTailInstanceBudget(n, len(c.code))
		if err == nil {
			return bytes + arena
		}
	}
	return bytes
}
func railshotNativeCodeBytes(c *Compiled) uint64 {
	size := len(c.code)
	if n := rv64StaticTailPlanCount(rv64NativeTailModule(c)); n != 0 {
		code, _, err := railshot.NativeTailInstanceBudget(n, size)
		if err != nil {
			return ^uint64(0)
		}
		size += code
	}
	return uint64(size)
}

func railshotNativeTailImport(c *Compiled, index int) bool {
	m := rv64NativeTailModule(c)
	if m != nil {
		for _, p := range m.Plans {
			if p.Kind == encoder.NativeTailDirectImport && int(p.ImportIndex) == index {
				return true
			}
		}
	}
	return false
}
