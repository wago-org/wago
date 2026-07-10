package wago

import (
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// moduleRequiredFeatures records the optional core features that the compiled
// module actually uses. The byte-sized on-disk mask is intentionally narrower
// than CoreFeatures: codec v20 rejects unknown/high bits rather than silently
// loading code produced for a feature this build cannot identify.
func moduleRequiredFeatures(m *wasm.Module) CoreFeatures {
	if m == nil {
		return 0
	}
	var out CoreFeatures
	if frontend.ModuleRequiresSIMD(m) {
		out |= CoreFeatureSIMD
	}

	for _, rec := range m.Types {
		for _, sub := range rec.SubTypes {
			if sub.Comp.Kind != wasm.CompFunc {
				continue
			}
			if len(sub.Comp.Results) > 1 {
				out |= CoreFeatureMultiValue
			}
			out |= requiredFeaturesForValTypes(sub.Comp.Params)
			out |= requiredFeaturesForValTypes(sub.Comp.Results)
		}
	}
	for _, im := range m.Imports {
		switch im.Type.Kind {
		case wasm.ExternGlobal:
			out |= requiredFeaturesForValType(im.Type.Global.Type)
			if im.Type.Global.Mutable {
				out |= CoreFeatureMutableGlobal
			}
		case wasm.ExternTable:
			if wasm.EqualValType(wasm.RefVal(im.Type.Table.Ref), wasm.ExternRef) {
				out |= CoreFeatureReferenceTypes
			}
		}
	}
	for _, g := range m.Globals {
		out |= requiredFeaturesForValType(g.Type.Type)
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind == wasm.ExternGlobal {
			if gt, ok := m.GlobalTypeByIndex(uint32(ex.Index.Index)); ok && gt.Mutable {
				out |= CoreFeatureMutableGlobal
			}
		}
	}
	if m.TableCount() > 1 {
		out |= CoreFeatureReferenceTypes
	}
	for _, table := range m.Tables {
		if wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.ExternRef) || table.Init != nil {
			out |= CoreFeatureReferenceTypes
		}
	}
	for _, elem := range m.Elements {
		if elem.Mode.Kind != wasm.ElemActive {
			out |= CoreFeatureBulkMemoryOperations
		}
		if elem.Kind.Kind != wasm.ElemFuncs {
			out |= CoreFeatureReferenceTypes
		}
	}
	for _, data := range m.Data {
		if data.Mode.Kind == wasm.DataPassive {
			out |= CoreFeatureBulkMemoryOperations
		}
	}
	for _, fn := range m.Code {
		for _, local := range fn.Locals.Runs {
			out |= requiredFeaturesForValType(local.Type)
		}
		out |= requiredFeaturesForInstructions(m, fn.Body.Instrs)
	}
	return out
}

func requiredFeaturesForValTypes(types []wasm.ValType) CoreFeatures {
	var out CoreFeatures
	for _, typ := range types {
		out |= requiredFeaturesForValType(typ)
	}
	return out
}

func requiredFeaturesForValType(typ wasm.ValType) CoreFeatures {
	switch typ.Kind {
	case wasm.ValRef:
		return CoreFeatureReferenceTypes
	case wasm.ValVec:
		if wasm.EqualValType(typ, wasm.V128) {
			return CoreFeatureSIMD
		}
	}
	return 0
}

func requiredFeaturesForInstructions(m *wasm.Module, instrs []wasm.Instruction) CoreFeatures {
	var out CoreFeatures
	for _, in := range instrs {
		switch in.Kind {
		case wasm.InstrI32Extend8S, wasm.InstrI32Extend16S, wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
			out |= CoreFeatureSignExtensionOps
		case wasm.InstrMemoryInit, wasm.InstrMemoryCopy, wasm.InstrMemoryFill, wasm.InstrDataDrop,
			wasm.InstrTableInit, wasm.InstrElemDrop, wasm.InstrTableCopy:
			out |= CoreFeatureBulkMemoryOperations
		case wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableSize, wasm.InstrTableFill,
			wasm.InstrRefNull, wasm.InstrRefIsNull, wasm.InstrRefFunc, wasm.InstrRefEq:
			out |= CoreFeatureReferenceTypes
		case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U,
			wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
			out |= CoreFeatureNonTrappingFloatToIntConversion
		}
		if in.Kind == wasm.InstrCallIndirect && in.Index2 != 0 {
			out |= CoreFeatureReferenceTypes
		}
		out |= requiredFeaturesForValTypes(in.ValTypes())
		bt := in.BlockType()
		if bt.Kind == wasm.BlockVal {
			out |= requiredFeaturesForValType(bt.Val)
		} else if bt.Kind == wasm.BlockTypeIndex {
			if sig, ok := m.TypeFunc(bt.Type.Index); ok {
				if len(sig.Params) != 0 || len(sig.Results) > 1 {
					out |= CoreFeatureMultiValue
				}
				out |= requiredFeaturesForValTypes(sig.Params)
				out |= requiredFeaturesForValTypes(sig.Results)
			}
		}
		switch in.Kind {
		case wasm.InstrBlock, wasm.InstrLoop:
			out |= requiredFeaturesForInstructions(m, in.Body().Instrs)
		case wasm.InstrIf:
			out |= requiredFeaturesForInstructions(m, in.Then())
			out |= requiredFeaturesForInstructions(m, in.Else())
		}
	}
	return out
}

func compiledStructuralRequiredFeatures(c *Compiled) CoreFeatures {
	if c == nil {
		return 0
	}
	out := CoreFeatures(c.requiredFeatures)
	if compiledMetadataUsesSIMD(c) {
		out |= CoreFeatureSIMD
	}
	for _, sig := range c.importFuncSigs {
		out |= requiredFeaturesForPublicValTypes(sig.Params)
		out |= requiredFeaturesForPublicValTypes(sig.Results)
	}
	for _, sig := range c.Funcs {
		out |= requiredFeaturesForPublicValTypes(sig.Params)
		out |= requiredFeaturesForPublicValTypes(sig.Results)
	}
	for _, g := range c.GlobalImports {
		if isReferenceValType(g.Type) {
			out |= CoreFeatureReferenceTypes
		}
	}
	for _, g := range c.Globals {
		if isReferenceValType(g.Type) {
			out |= CoreFeatureReferenceTypes
		}
	}
	if c.hasExternrefTable() || c.tableCount() > 1 || c.NeedsFuncRefDescs {
		out |= CoreFeatureReferenceTypes
	}
	for _, elem := range c.Elems {
		if elem.RefType == ValExternRef || elem.TableIndex != 0 {
			out |= CoreFeatureReferenceTypes
		}
	}
	for _, elem := range c.passiveElems {
		if elem.RefType == ValExternRef {
			out |= CoreFeatureReferenceTypes
		}
		if elem.Mode != ElemModeActive {
			out |= CoreFeatureBulkMemoryOperations
		}
	}
	return out
}

func requiredFeaturesForPublicValTypes(types []ValType) CoreFeatures {
	var out CoreFeatures
	for _, typ := range types {
		if isReferenceValType(typ) {
			out |= CoreFeatureReferenceTypes
		}
		if typ == ValV128 {
			out |= CoreFeatureSIMD
		}
	}
	return out
}
