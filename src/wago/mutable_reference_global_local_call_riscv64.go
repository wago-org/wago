//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64MutableFuncrefLocalCallCodeDigest = [sha256.Size]byte{0x68, 0x0e, 0xb8, 0xec, 0x42, 0xa6, 0xf8, 0xb2, 0x5d, 0x83, 0x13, 0x7f, 0xfe, 0xb0, 0xd9, 0x85, 0xc8, 0x20, 0x89, 0x77, 0x66, 0xf9, 0x85, 0xcb, 0x5a, 0x0d, 0x02, 0x32, 0xf7, 0x91, 0xc5, 0x51}

var rv64MutableFuncrefLocalCallI32CodeDigest = [sha256.Size]byte{0xcb, 0x2b, 0xa8, 0x09, 0x51, 0xa2, 0xdb, 0x46, 0xbc, 0x71, 0xd5, 0xe8, 0xc3, 0x65, 0x64, 0x0a, 0xd0, 0xc7, 0xfe, 0x4b, 0xb5, 0x3c, 0xd0, 0x7f, 0x44, 0x77, 0xe0, 0x5d, 0xc0, 0xc9, 0x4d, 0xee}

var rv64MutableFuncrefLocalCallI64CodeDigest = [sha256.Size]byte{0x24, 0x0c, 0x58, 0xf8, 0x50, 0x94, 0xd9, 0x15, 0x6f, 0xdf, 0xf8, 0x0e, 0xa9, 0x8a, 0xce, 0xf7, 0x69, 0x94, 0x6b, 0xf3, 0x84, 0x9a, 0x5d, 0xc2, 0x85, 0x8f, 0x82, 0x5b, 0x8f, 0x9c, 0x47, 0x1c}

var rv64MutableFuncrefLocalCallF32CodeDigest = [sha256.Size]byte{0xcb, 0xcd, 0xda, 0xa1, 0x8d, 0x46, 0xaf, 0xfa, 0x27, 0x50, 0xa3, 0xde, 0x2b, 0xe2, 0x0a, 0x60, 0x72, 0x04, 0xcc, 0x28, 0xc9, 0x9e, 0xef, 0x9d, 0x70, 0xee, 0x1c, 0x5e, 0x5a, 0x3e, 0x56, 0xff}

var rv64MutableFuncrefLocalCallF64CodeDigest = [sha256.Size]byte{0xb2, 0xdb, 0xb6, 0xc6, 0x89, 0x6f, 0x4e, 0x92, 0x9d, 0x29, 0x15, 0xbb, 0x9e, 0xa9, 0x38, 0x79, 0x06, 0x5c, 0x4d, 0x33, 0x5b, 0x0c, 0xc7, 0xa6, 0x3c, 0x54, 0x29, 0xbe, 0x01, 0x15, 0xea, 0xfa}

var rv64MutableFuncrefLocalCallMixedCodeDigest = [sha256.Size]byte{0x49, 0x44, 0x35, 0x48, 0x00, 0x5b, 0x2e, 0x97, 0x55, 0x33, 0x90, 0xe0, 0xdb, 0x28, 0xec, 0xb3, 0xfb, 0xed, 0x8d, 0x07, 0xea, 0x76, 0xdb, 0xab, 0xdc, 0xe2, 0x05, 0xae, 0x46, 0x93, 0x4c, 0xfa}

var rv64MutableFuncrefLocalCallWideI32CodeDigest = [sha256.Size]byte{0x64, 0x95, 0xf2, 0x5e, 0x9d, 0xed, 0xbe, 0x9b, 0xf2, 0xfc, 0x70, 0xe8, 0x47, 0x64, 0xfa, 0xe9, 0xfb, 0x30, 0x94, 0xe3, 0x4b, 0x07, 0x81, 0x9d, 0x2d, 0x56, 0xb6, 0x11, 0xc3, 0x10, 0xd5, 0xe5}

var rv64MutableFuncrefLocalCallVectorCodeDigest = [sha256.Size]byte{0x8d, 0x3a, 0xce, 0x25, 0x5f, 0xea, 0x3a, 0x8e, 0x6c, 0xc4, 0x15, 0x05, 0x36, 0x1b, 0x8e, 0xa4, 0xc0, 0x13, 0xc9, 0x18, 0x55, 0x73, 0x26, 0xca, 0xf1, 0x13, 0xd0, 0xd8, 0x61, 0x99, 0xfb, 0x52}

var rv64MutableFuncrefLocalCallMixedVectorCodeDigest = [sha256.Size]byte{0xba, 0xc0, 0xa5, 0x55, 0xe1, 0x3d, 0x47, 0xb4, 0x98, 0x70, 0x31, 0xc6, 0x59, 0x59, 0x6d, 0x71, 0xc7, 0x43, 0x01, 0x23, 0xc4, 0x6d, 0x64, 0xf7, 0x3c, 0x6e, 0x94, 0x7a, 0xec, 0xa2, 0xdb, 0x30}

var rv64MutableFuncrefLocalTailCallI32CodeDigest = [sha256.Size]byte{0xf8, 0x1b, 0x9a, 0x9b, 0x5e, 0x91, 0xaf, 0x10, 0xe6, 0x5a, 0x22, 0x3e, 0xa6, 0x11, 0x70, 0x4a, 0x60, 0x06, 0xb7, 0x2c, 0x36, 0xc9, 0xcb, 0xec, 0x51, 0x6a, 0xf6, 0x97, 0x7b, 0x62, 0x5a, 0xce}

var rv64MutableFuncrefLocalTailCallVectorCodeDigest = [sha256.Size]byte{0xe6, 0x77, 0xe2, 0x39, 0x39, 0x37, 0x36, 0xcd, 0xac, 0x5a, 0xb5, 0xbc, 0xdf, 0x28, 0x3a, 0xc3, 0x6d, 0x5c, 0x5c, 0xba, 0x95, 0x7d, 0x2d, 0xe7, 0xa9, 0x6b, 0x86, 0x22, 0xab, 0x1b, 0xf9, 0xc2}

var rv64MutableFuncrefLocalTailCallMixedVectorCodeDigest = [sha256.Size]byte{0x59, 0x1f, 0xc3, 0xa6, 0x6d, 0x4c, 0xb4, 0x71, 0x3e, 0x6b, 0xa5, 0x62, 0xcd, 0x3c, 0xcf, 0x8a, 0x34, 0x36, 0x18, 0xe6, 0xa9, 0xe2, 0xd0, 0x0f, 0x2a, 0xe0, 0x0c, 0xac, 0x75, 0xcb, 0x07, 0x55}

var rv64MutableFuncrefLocalTailCallZeroCodeDigest = [sha256.Size]byte{0x31, 0xf2, 0xdc, 0x9c, 0xd5, 0x1b, 0x3b, 0x57, 0x47, 0x50, 0xbf, 0x6b, 0x47, 0x25, 0xef, 0xf3, 0x82, 0x90, 0x44, 0x7f, 0xe8, 0x34, 0x93, 0x9e, 0x0f, 0x1a, 0xe8, 0xc1, 0xba, 0xfb, 0xea, 0xb2}

var rv64MutableFuncrefLocalTailCallI64CodeDigest = [sha256.Size]byte{0xc1, 0x07, 0x27, 0xc9, 0xbf, 0x29, 0xfb, 0x08, 0xbc, 0xb9, 0xa7, 0xa9, 0x98, 0xd5, 0xf6, 0x90, 0x62, 0xf7, 0x95, 0x88, 0x49, 0x7b, 0xaa, 0x66, 0xb9, 0x5f, 0x84, 0xad, 0xac, 0xca, 0x15, 0xb7}

var rv64MutableFuncrefLocalTailCallF32CodeDigest = [sha256.Size]byte{0xeb, 0xfe, 0x25, 0x36, 0x35, 0xe5, 0xbf, 0xfa, 0xe4, 0xff, 0xbc, 0x80, 0x18, 0x90, 0x8b, 0xec, 0xb7, 0xd0, 0x0f, 0x78, 0x4a, 0x30, 0x18, 0x25, 0x43, 0x85, 0xd3, 0x9c, 0x9b, 0x62, 0x48, 0xb6}

var rv64MutableFuncrefLocalTailCallF64CodeDigest = [sha256.Size]byte{0x7f, 0x43, 0x2b, 0x1b, 0xa7, 0xc1, 0xdf, 0xb0, 0x53, 0x57, 0x09, 0x14, 0x65, 0xd7, 0x3a, 0x04, 0x8e, 0x5b, 0xc1, 0x6e, 0x1e, 0x4f, 0xf0, 0x85, 0x55, 0x9a, 0x3b, 0x06, 0x4d, 0xdb, 0x9a, 0x80}

var rv64MutableFuncrefLocalTailCallMixedCodeDigest = [sha256.Size]byte{0x40, 0xf3, 0x50, 0x8e, 0x7f, 0xdd, 0x6b, 0xfe, 0x77, 0xc2, 0xbc, 0x06, 0x73, 0x48, 0x1a, 0xa9, 0xbb, 0x7c, 0x25, 0xf0, 0x26, 0x1f, 0x82, 0x2b, 0xfb, 0x47, 0x2f, 0x8e, 0x94, 0xfc, 0x9b, 0x64}

var rv64MutableFuncrefLocalTailCallWideI32CodeDigest = [sha256.Size]byte{0x55, 0x99, 0xe1, 0x83, 0x21, 0x2a, 0x61, 0xcd, 0x65, 0xab, 0x8c, 0xf6, 0x44, 0xa3, 0x85, 0xe4, 0xed, 0x07, 0x76, 0xdf, 0x74, 0xe9, 0x88, 0x53, 0xf2, 0x95, 0x9d, 0xa6, 0x40, 0x84, 0x51, 0x79}

func rv64MutableFuncrefGlobalLocalCallExecutionCode(c *Compiled) bool {
	return rv64MutableFuncrefGlobalLocalCallCodeVariant(c) != 0
}

func rv64MutableFuncrefGlobalLocalCallCodeVariant(c *Compiled) int {
	if c == nil {
		return 0
	}
	digest := sha256.Sum256(c.code)
	if len(c.code) == 6812 && digest == rv64MutableFuncrefLocalCallCodeDigest {
		return 1
	}
	if len(c.code) == 6924 && digest == rv64MutableFuncrefLocalCallI32CodeDigest {
		return 2
	}
	if len(c.code) == 6892 && digest == rv64MutableFuncrefLocalCallI64CodeDigest {
		return 3
	}
	if len(c.code) == 6924 && digest == rv64MutableFuncrefLocalCallF32CodeDigest {
		return 4
	}
	if len(c.code) == 6908 && digest == rv64MutableFuncrefLocalCallF64CodeDigest {
		return 5
	}
	if len(c.code) == 7324 && digest == rv64MutableFuncrefLocalCallMixedCodeDigest {
		return 6
	}
	if len(c.code) == 8044 && digest == rv64MutableFuncrefLocalCallWideI32CodeDigest {
		return 7
	}
	if len(c.code) == 6972 && digest == rv64MutableFuncrefLocalCallVectorCodeDigest {
		return 8
	}
	if len(c.code) == 7580 && digest == rv64MutableFuncrefLocalCallMixedVectorCodeDigest {
		return 9
	}
	if len(c.code) == 4748 && digest == rv64MutableFuncrefLocalTailCallI32CodeDigest {
		return 10
	}
	if len(c.code) == 4780 && digest == rv64MutableFuncrefLocalTailCallVectorCodeDigest {
		return 11
	}
	if len(c.code) == 5292 && digest == rv64MutableFuncrefLocalTailCallMixedVectorCodeDigest {
		return 12
	}
	if len(c.code) == 4668 && digest == rv64MutableFuncrefLocalTailCallZeroCodeDigest {
		return 13
	}
	if len(c.code) == 4732 && digest == rv64MutableFuncrefLocalTailCallI64CodeDigest {
		return 14
	}
	if len(c.code) == 4748 && digest == rv64MutableFuncrefLocalTailCallF32CodeDigest {
		return 15
	}
	if len(c.code) == 4748 && digest == rv64MutableFuncrefLocalTailCallF64CodeDigest {
		return 16
	}
	if len(c.code) == 5052 && digest == rv64MutableFuncrefLocalTailCallMixedCodeDigest {
		return 17
	}
	if len(c.code) == 5628 && digest == rv64MutableFuncrefLocalTailCallWideI32CodeDigest {
		return 18
	}
	return 0
}

// rv64MutableFuncrefGlobalLocalCall authenticates one host-visible nullable
// typed cell and its sole call_ref or return_call_ref consumer. Host ingress
// must match type zero; guest writers can install only the two local targets
// or null.
func rv64MutableFuncrefGlobalLocalCall(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 6 || len(c.FuncTypeID) != 6 || len(c.Exports) != 6 || len(c.Types) != 2 || len(c.GCTypeDescs) != 2 {
		return false
	}
	for name, index := range map[string]int{"target_0": 0, "target_1": 1, "set_0": 2, "set_1": 3, "clear": 4, "call": 5} {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	cellIndex, cellExported := c.GlobalExports["cell"]
	if len(c.GlobalImports) != 0 || len(c.Globals) != 1 || len(c.GlobalExports) != 1 || !cellExported || cellIndex != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 || c.Names != nil || c.HasTable || c.HasMemory || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	variant := rv64MutableFuncrefGlobalLocalCallCodeVariant(c)
	if variant == 0 {
		return false
	}
	wasmParams := []wasm.ValType(nil)
	wasmResults := []wasm.ValType{wasm.I32}
	abiParams := []ValType(nil)
	abiResults := []ValType{ValI32}
	valueParams := []ValueTypeDescriptor(nil)
	valueResults := []ValueTypeDescriptor{{Kind: ValueTypeI32}}
	switch variant {
	case 2, 10:
		wasmParams = []wasm.ValType{wasm.I32}
		abiParams = []ValType{ValI32}
		valueParams = []ValueTypeDescriptor{{Kind: ValueTypeI32}}
	case 3, 14:
		wasmParams, wasmResults = []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}
		abiParams, abiResults = []ValType{ValI64}, []ValType{ValI64}
		valueParams, valueResults = []ValueTypeDescriptor{{Kind: ValueTypeI64}}, []ValueTypeDescriptor{{Kind: ValueTypeI64}}
	case 4, 15:
		wasmParams, wasmResults = []wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32}
		abiParams, abiResults = []ValType{ValF32}, []ValType{ValF32}
		valueParams, valueResults = []ValueTypeDescriptor{{Kind: ValueTypeF32}}, []ValueTypeDescriptor{{Kind: ValueTypeF32}}
	case 5, 16:
		wasmParams, wasmResults = []wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}
		abiParams, abiResults = []ValType{ValF64}, []ValType{ValF64}
		valueParams, valueResults = []ValueTypeDescriptor{{Kind: ValueTypeF64}}, []ValueTypeDescriptor{{Kind: ValueTypeF64}}
	case 6, 17:
		wasmParams = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
		wasmResults = slices.Clone(wasmParams)
		abiParams = []ValType{ValI32, ValI64, ValF32, ValF64}
		abiResults = slices.Clone(abiParams)
		valueParams = []ValueTypeDescriptor{{Kind: ValueTypeI32}, {Kind: ValueTypeI64}, {Kind: ValueTypeF32}, {Kind: ValueTypeF64}}
		valueResults = slices.Clone(valueParams)
	case 7, 18:
		wasmParams = make([]wasm.ValType, 9)
		abiParams = make([]ValType, 9)
		valueParams = make([]ValueTypeDescriptor, 9)
		for i := range wasmParams {
			wasmParams[i] = wasm.I32
			abiParams[i] = ValI32
			valueParams[i] = ValueTypeDescriptor{Kind: ValueTypeI32}
		}
		wasmResults = slices.Clone(wasmParams)
		abiResults = slices.Clone(abiParams)
		valueResults = slices.Clone(valueParams)
	case 8, 11:
		wasmParams, wasmResults = []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}
		abiParams, abiResults = []ValType{ValV128}, []ValType{ValV128}
		valueParams, valueResults = []ValueTypeDescriptor{{Kind: ValueTypeV128}}, []ValueTypeDescriptor{{Kind: ValueTypeV128}}
	case 9, 12:
		wasmParams, wasmResults = []wasm.ValType{wasm.I32, wasm.V128, wasm.F64}, []wasm.ValType{wasm.I32, wasm.V128, wasm.F64}
		abiParams, abiResults = []ValType{ValI32, ValV128, ValF64}, []ValType{ValI32, ValV128, ValF64}
		valueParams, valueResults = []ValueTypeDescriptor{{Kind: ValueTypeI32}, {Kind: ValueTypeV128}, {Kind: ValueTypeF64}}, []ValueTypeDescriptor{{Kind: ValueTypeI32}, {Kind: ValueTypeV128}, {Kind: ValueTypeF64}}
	}
	if variant >= 10 {
		wantPlan := encoder.NativeTailPlan{Kind: encoder.NativeTailReference, TypeIndex: 0, FunctionIndex: 5, SourcePC: 5, FrameBytes: 80, SourceBase: 56, SourceStride: 8, ReferenceBase: 64}
		if variant == 13 {
			wantPlan.SourcePC, wantPlan.FrameBytes, wantPlan.SourceBase, wantPlan.ReferenceBase = 3, 64, 48, 48
		} else if variant == 11 {
			wantPlan.FrameBytes, wantPlan.SourceBase, wantPlan.SourceStride, wantPlan.ReferenceBase = 96, 64, 16, 80
		} else if variant == 12 {
			wantPlan.SourcePC, wantPlan.FrameBytes, wantPlan.SourceBase, wantPlan.SourceStride, wantPlan.ReferenceBase = 9, 160, 96, 16, 144
		} else if variant == 17 {
			wantPlan.SourcePC, wantPlan.FrameBytes, wantPlan.SourceBase, wantPlan.ReferenceBase = 11, 128, 80, 112
		} else if variant == 18 {
			wantPlan.SourcePC, wantPlan.FrameBytes, wantPlan.SourceBase, wantPlan.ReferenceBase = 21, 208, 120, 192
		}
		m := rv64NativeTailModule(c)
		if c.nativeReferenceCallABI || m == nil || !slices.Equal(m.Plans, []encoder.NativeTailPlan{wantPlan}) {
			return false
		}
	} else if !c.nativeReferenceCallABI || rv64HasReferenceTail(c) {
		return false
	}
	scalarType := &wasm.CompType{Kind: wasm.CompFunc, Params: wasmParams, Results: wasmResults}
	voidType := &wasm.CompType{Kind: wasm.CompFunc}
	scalarKey, voidKey := wasm.StructuralFuncTypeKey(scalarType), wasm.StructuralFuncTypeKey(voidType)
	wantTypes := []uint32{0, 0, 1, 1, 1, 0}
	for i, sig := range c.Funcs {
		wantKey := scalarKey
		validResults := slices.Equal(sig.Results, abiResults)
		validParams := slices.Equal(sig.Params, abiParams)
		if wantTypes[i] == 1 {
			wantKey = voidKey
			validResults = len(sig.Results) == 0
			validParams = len(sig.Params) == 0
		}
		if !validParams || !validResults || !sig.HasTypeIndex || sig.TypeIndex != wantTypes[i] || sig.unsafeCrossTail || c.FuncTypeID[i] != wantKey {
			return false
		}
	}
	for i, typ := range c.Types {
		validResults := slices.Equal(typ.Results, valueResults)
		validParams := slices.Equal(typ.Params, valueParams)
		if i == 1 {
			validResults = len(typ.Results) == 0
			validParams = len(typ.Params) == 0
		}
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || !validParams || !validResults || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
			return false
		}
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != corergc.TypeID(i) || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
			return false
		}
	}
	exact := c.ValueTypes[0]
	globalExact, err := c.globalExactType(0)
	global := c.Globals[0]
	if err != nil || globalExact != exact || exact.Kind != ValueTypeReference || !exact.Ref.Nullable || exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.Abstract != 0 || exact.Ref.Heap.TypeIndex != 0 || global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 || global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal | CoreFeatureTypedFunctionReferences
	if variant == 6 || variant == 7 || variant == 9 || variant == 12 || variant == 17 || variant == 18 {
		required |= CoreFeatureMultiValue
	}
	if variant == 8 || variant == 9 || variant == 11 || variant == 12 {
		required |= CoreFeatureSIMD
	}
	if variant >= 10 {
		required |= CoreFeatureTailCall
	}
	wantEntry := []int{0, 608, 1216, 1936, 2656, 3312}
	if variant == 2 || variant == 4 || variant == 5 || variant == 10 || variant == 15 || variant == 16 {
		wantEntry = []int{0, 608, 1264, 1984, 2704, 3360}
	} else if variant == 3 || variant == 14 {
		wantEntry = []int{0, 608, 1248, 1968, 2688, 3344}
	} else if variant == 6 || variant == 17 {
		wantEntry = []int{0, 672, 1504, 2224, 2944, 3600}
	} else if variant == 7 || variant == 18 {
		wantEntry = []int{0, 800, 1952, 2672, 3392, 4048}
	} else if variant == 8 || variant == 11 {
		wantEntry = []int{0, 624, 1280, 2000, 2720, 3360}
	} else if variant == 9 || variant == 12 {
		wantEntry = []int{0, 768, 1680, 2400, 3120, 3760}
	}
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry) && rv64MutableFuncrefGlobalLocalCallExecutionCode(c)
}
