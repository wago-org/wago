package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// stagedStructuralTypeProduct identifies the collector-free type-rec products
// whose struct definitions affect only function identity. No struct/array value
// is created, stored, returned, or exposed by these shapes.
type stagedStructuralTypeProduct uint8

const (
	stagedStructuralRefFuncGlobal stagedStructuralTypeProduct = iota + 1
	stagedStructuralFunctionLink
	stagedStructuralCallIndirect
)

func (p stagedStructuralTypeProduct) gateReason() string {
	switch p {
	case stagedStructuralRefFuncGlobal:
		return "recursive struct metadata with an immutable local ref.func global"
	case stagedStructuralFunctionLink:
		return "recursive struct metadata in cross-instance function link matching"
	case stagedStructuralCallIndirect:
		return "recursive struct metadata in ordinary funcref call_indirect matching"
	default:
		return "unknown collector-free structural product"
	}
}

func stagedStructuralTypeProductShape(m *wasm.Module) (stagedStructuralTypeProduct, error) {
	if m == nil {
		return 0, fmt.Errorf("nil module")
	}
	if m.Start != nil || m.TagCount() != 0 || m.MemCount() != 0 || len(m.Data) != 0 {
		return 0, fmt.Errorf("collector-free structural products reject start, tag, memory, and data state")
	}
	if err := stagedStructuralTypeMetadataShape(m); err != nil {
		return 0, err
	}
	if p, ok := stagedStructuralRefFuncGlobalShape(m); ok {
		return p, nil
	}
	if p, ok := stagedStructuralFunctionLinkShape(m); ok {
		return p, nil
	}
	if p, ok := stagedStructuralCallIndirectShape(m); ok {
		return p, nil
	}
	return 0, fmt.Errorf("module is outside the exact ref.func-global, function-link, and call_indirect structural products")
}

func stagedStructuralTypeMetadataShape(m *wasm.Module) error {
	hasStruct := false
	for gi := range m.Types {
		for si := range m.Types[gi].SubTypes {
			st := &m.Types[gi].SubTypes[si]
			if st.HasPrefix || len(st.Supers) != 0 || st.Metadata.Describes != nil || st.Metadata.Descriptor != nil {
				return fmt.Errorf("collector-free structural products reject subtype and descriptor metadata")
			}
			switch st.Comp.Kind {
			case wasm.CompFunc:
			case wasm.CompStruct:
				hasStruct = true
				for i := range st.Comp.Fields {
					if st.Comp.Fields[i].Mut != 0 {
						return fmt.Errorf("collector-free structural products reject mutable struct fields")
					}
				}
			case wasm.CompArray:
				return fmt.Errorf("collector-free structural products do not yet admit array definitions")
			default:
				return fmt.Errorf("collector-free structural products reject unknown composite type %d", st.Comp.Kind)
			}
		}
	}
	if !hasStruct {
		return fmt.Errorf("collector-free structural product requires at least one struct definition")
	}
	return nil
}

func stagedStructuralRefFuncGlobalShape(m *wasm.Module) (stagedStructuralTypeProduct, bool) {
	if len(m.Imports) != 0 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Globals) != 1 || m.TableCount() != 0 || len(m.Elements) != 0 || len(m.Exports) != 0 {
		return 0, false
	}
	if len(m.Code[0].Locals.Runs) != 0 || !isExactEndBody(m.Code[0].BodyBytes) {
		return 0, false
	}
	g := &m.Globals[0]
	if g.Type.Mutable || !isNonNullIndexedFunctionRef(m, g.Type.Type) || !isExactRefFuncBody(g.Init.BodyBytes, 0) {
		return 0, false
	}
	return stagedStructuralRefFuncGlobal, true
}

func stagedStructuralFunctionLinkShape(m *wasm.Module) (stagedStructuralTypeProduct, bool) {
	if len(m.Globals) != 0 || m.TableCount() != 0 || len(m.Elements) != 0 {
		return 0, false
	}
	if len(m.Imports) == 0 {
		if len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Exports) != 1 || m.Exports[0].Name != "f" || m.Exports[0].Index.Kind != wasm.ExternFunc || m.Exports[0].Index.Index != 0 {
			return 0, false
		}
		if len(m.Code[0].Locals.Runs) != 0 || !isExactEndBody(m.Code[0].BodyBytes) {
			return 0, false
		}
		return stagedStructuralFunctionLink, true
	}
	if len(m.Imports) != 1 || len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Exports) != 0 {
		return 0, false
	}
	im := &m.Imports[0]
	if im.Type.Kind != wasm.ExternFunc || im.Module != "M" || im.Name != "f" {
		return 0, false
	}
	return stagedStructuralFunctionLink, true
}

func stagedStructuralCallIndirectShape(m *wasm.Module) (stagedStructuralTypeProduct, bool) {
	if len(m.Imports) != 0 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Globals) != 0 || len(m.Tables) != 1 || len(m.Elements) != 1 || len(m.Exports) != 1 {
		return 0, false
	}
	if len(m.Code[0].Locals.Runs) != 0 || !isExactEndBody(m.Code[0].BodyBytes) || len(m.Code[1].Locals.Runs) != 0 {
		return 0, false
	}
	t := &m.Tables[0].Type
	if !wasm.EqualValType(wasm.RefVal(t.Ref), wasm.FuncRef) || t.Limits.Addr64 || t.Limits.Min != 1 || t.Limits.Max == nil || *t.Limits.Max != 1 || m.Tables[0].Init != nil {
		return 0, false
	}
	e := &m.Elements[0]
	if e.Mode.Kind != wasm.ElemActive || e.Mode.Table != 0 || !isExactI32ConstZeroBody(e.Mode.Offset.BodyBytes) || e.Kind.Kind != wasm.ElemFuncExprs || len(e.Kind.Exprs) != 1 || !isExactRefFuncBody(e.Kind.Exprs[0].BodyBytes, 0) {
		return 0, false
	}
	ex := &m.Exports[0]
	if ex.Name != "run" || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != 1 {
		return 0, false
	}
	callType, ok := exactCallIndirectZeroBody(m.Code[1].BodyBytes)
	if !ok {
		return 0, false
	}
	for _, typeIndex := range []uint32{m.FuncTypes[0].Index, m.FuncTypes[1].Index, callType} {
		ft, ok := m.ResolvedTypeFunc(typeIndex)
		if !ok || len(ft.Params) != 0 || len(ft.Results) != 0 {
			return 0, false
		}
	}
	return stagedStructuralCallIndirect, true
}

func isNonNullIndexedFunctionRef(m *wasm.Module, typ wasm.ValType) bool {
	if typ.Kind != wasm.ValRef || typ.Ref.Nullable || typ.Ref.Exact || typ.Ref.Heap.Kind != wasm.HeapTypeIndex {
		return false
	}
	_, ok := m.ResolvedTypeFunc(typ.Ref.Heap.Type.Index)
	return ok
}

func isExactEndBody(body []byte) bool {
	return len(body) == 1 && body[0] == 0x0b
}

func isExactRefFuncBody(body []byte, index uint32) bool {
	r := wasm.NewReader(body)
	op, err := r.Byte()
	if err != nil || op != 0xd2 {
		return false
	}
	got, err := r.U32()
	if err != nil || got != index {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && r.BytesLeft() == 0
}

func isExactI32ConstZeroBody(body []byte) bool {
	r := wasm.NewReader(body)
	op, err := r.Byte()
	if err != nil || op != 0x41 {
		return false
	}
	value, err := r.S33()
	if err != nil || value != 0 {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && r.BytesLeft() == 0
}

func exactCallIndirectZeroBody(body []byte) (uint32, bool) {
	r := wasm.NewReader(body)
	op, err := r.Byte()
	if err != nil || op != 0x41 {
		return 0, false
	}
	value, err := r.S33()
	if err != nil || value != 0 {
		return 0, false
	}
	op, err = r.Byte()
	if err != nil || op != 0x11 {
		return 0, false
	}
	typeIndex, err := r.U32()
	if err != nil {
		return 0, false
	}
	tableIndex, err := r.U32()
	if err != nil || tableIndex != 0 {
		return 0, false
	}
	end, err := r.Byte()
	return typeIndex, err == nil && end == 0x0b && r.BytesLeft() == 0
}
