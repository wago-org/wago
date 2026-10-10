//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const (
	MaxOwnedTableEntries    = 1024
	MaxOwnedElementSegments = 256
	trapIndirectBounds      = 8 // distinct from private memory-bounds tag5
	trapIndirectSignature   = 9
	trapTableBounds         = 10
)

// This public profile accepts a single byte-backed abstract funcref literal.
// The shared parser additionally accepts nofunc/global/structured expressions;
// keep those outside the qualified RV64 profile, including unused segments.
func tableElementExpression(expr wasm.Expr) (wasm.FuncrefElementExpr, error) {
	if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 {
		return wasm.FuncrefElementExpr{}, unsupported("element expression representation/byte bound")
	}
	switch expr.BodyBytes[0] {
	case 0xd0:
		r := wasm.ReaderFrom(expr.BodyBytes[1:])
		heap, err := r.S33()
		if err != nil || heap != -16 {
			return wasm.FuncrefElementExpr{}, unsupported("element null requires abstract func")
		}
	case 0xd2:
	default:
		return wasm.FuncrefElementExpr{}, unsupported("element requires ref.null or ref.func")
	}
	value, err := wasm.ParseFuncrefElementExpr(expr)
	if err != nil {
		return wasm.FuncrefElementExpr{}, unsupported("element reference literal")
	}
	return value, nil
}

// Element payloads may copy immutable construction-time globals under the
// retained public ABI. Declarative payloads are validated before being dropped;
// private owners and table initializers cannot resolve shared global cells.
func elementInitializerExpression(expr wasm.Expr, m *wasm.Module, mode wasm.ElemModeKind, public bool) (wasm.ElementExpr, error) {
	if len(expr.BodyBytes) != 0 && expr.BodyBytes[0] == 0x23 {
		if !public || mode != wasm.ElemActive && mode != wasm.ElemPassive && mode != wasm.ElemDeclarative {
			return wasm.ElementExpr{}, unsupported("element global requires a public segment")
		}
		if err := admitGlobalExpression(expr, m, wasm.FuncRef, m.ImportedGlobalCount()+len(m.Globals)); err != nil {
			return wasm.ElementExpr{}, err
		}
		value, err := wasm.ParseElementExpr(expr)
		if err != nil {
			return wasm.ElementExpr{}, unsupported("element global expression")
		}
		return value, nil
	}
	value, err := tableElementExpression(expr)
	return wasm.ElementExpr{RefType: wasm.FuncRef.Ref(), Null: value.Null, FuncIndex: value.FuncIndex}, err
}

// Table initialization shares the literal element profile and canonical public
// descriptors. An imported immutable funcref is materialized by the shared
// frontend as a synthetic active segment. Local globals remain outside this
// profile: Core table initializers may only read imported globals. Validate the
// source even when the initial table size is zero.
func admitTableInitializer(expr wasm.Expr, m *wasm.Module) (global bool, err error) {
	if len(expr.BodyBytes) != 0 && expr.BodyBytes[0] == 0x23 {
		if err := admitGlobalExpression(expr, m, wasm.FuncRef, m.ImportedGlobalCount()); err != nil {
			return false, err
		}
		return true, nil
	}
	value, err := tableElementExpression(expr)
	if err != nil {
		return false, err
	}
	if !value.Null && uint64(value.FuncIndex) >= uint64(m.ImportedFuncCount()+len(m.Code)) {
		return false, unsupported("table initializer function index")
	}
	return false, nil
}

// Bound decoded resource metadata before validation or payload copies.
func ownedTableBounds(m *wasm.Module, public bool) error {
	if len(m.Imports) > MaxProductionImports {
		return unsupported("table import count bound")
	}
	tableCount := len(m.Tables) + m.ImportedTableCount()
	if tableCount > 1 && !admitOwnedTwoTable32Size(m, true, public) && !admitOwnedTwoTable64Size(m, true, public) && !admitOwnedTwoTable64GetSet(m, true, public) && !admitOwnedTwoTable64Grow(m, true, public) && !admitOwnedTwoTable64Fill(m, true, public) && !admitOwnedTwoTable64Copy(m, true, public) && !admitOwnedTwoTable64CrossCopy(m, true, public) && !admitOwnedTwoTable64InitDrop(m, true, public) && !admitOwnedTwoTable64Indirect(m, true, public) || len(m.Elements) > MaxOwnedElementSegments {
		return unsupported("private table/element count")
	}
	for index := 0; index < tableCount; index++ {
		t, _ := m.TableType(uint32(index))
		if t.Limits.Min > MaxOwnedTableEntries || (t.Limits.HasMax && t.Limits.Max > MaxOwnedTableEntries) {
			return unsupported("private table capacity")
		}
	}
	remaining := MaxOwnedTableEntries
	if public && len(m.Tables) == 1 && m.Tables[0].Init != nil {
		global, err := admitTableInitializer(*m.Tables[0].Init, m)
		if err != nil {
			return err
		}
		if global {
			// The shared frontend expands a global initializer to one active
			// segment containing the initial table length. Account for that
			// synthetic retained metadata before ordinary segments.
			if len(m.Elements) >= MaxOwnedElementSegments || m.Tables[0].Type.Limits.Min > uint64(remaining) {
				return unsupported("table global initializer aggregate bound")
			}
			remaining -= int(m.Tables[0].Type.Limits.Min)
		}
	}
	for _, e := range m.Elements {
		if len(e.Kind.Funcs) > remaining || len(e.Kind.Exprs) > remaining {
			return unsupported("element payload bound")
		}
		remaining -= len(e.Kind.Funcs) + len(e.Kind.Exprs)
		if remaining < 0 {
			return unsupported("element payload bound")
		}
		for _, expr := range e.Kind.Exprs {
			if _, err := elementInitializerExpression(expr, m, e.Mode.Kind, public); err != nil {
				return err
			}
		}
	}
	return nil
}

func admitOwnedTable(m *wasm.Module, production bool) (bool, error) {
	return admitTableProfile(m, production, false)
}

// admitStateOnlyTable64 recognizes the first deliberately non-executable
// table64 profile. It permits one bounded owned table to be instantiated and
// exported, but no instruction, initializer, import, or second resource can
// observe 64-bit table operands until those lowering paths are qualified.
func admitStateOnlyTable64(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil {
		return false
	}
	for _, export := range m.Exports {
		if export.Index.Kind != wasm.ExternTable || export.Index.Index != 0 {
			return false
		}
	}
	t := m.Tables[0]
	return t.Type.Limits.Addr64 && wasm.EqualValType(wasm.RefVal(t.Type.Ref), wasm.FuncRef)
}

// admitOwnedTwoTable32Size is the first exact scalar table32 directory slice.
// It observes two bounded owned nullable-funcref lengths through distinct
// fixed indexes without reference payloads, initializers, mutation or calls.
func admitOwnedTwoTable32Size(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		if table.Init != nil || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.Max > MaxOwnedTableEntries || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
			return false
		}
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != 0 || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 || !bytes.Equal(m.Code[i].BodyBytes, []byte{0xfc, 0x10, byte(i), 0x0b}) {
			return false
		}
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 0 || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	var functions, tables [2]bool
	for _, export := range m.Exports {
		if export.Index.Index > 1 {
			return false
		}
		switch export.Index.Kind {
		case wasm.ExternFunc:
			functions[export.Index.Index] = true
		case wasm.ExternTable:
			tables[export.Index.Index] = true
		default:
			return false
		}
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

// admitOwnedTwoTable64Size is the first multi-table descriptor slice. It
// observes two bounded owned table64 lengths through distinct fixed indexes,
// without references, initializers, imports, mutation, or indirect dispatch.
func admitOwnedTwoTable64Size(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.Max > MaxOwnedTableEntries || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
			return false
		}
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != 0 || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 || !bytes.Equal(m.Code[i].BodyBytes, []byte{0xfc, 0x10, byte(i), 0x0b}) {
			return false
		}
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 0 || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I64) {
		return false
	}
	var functions, tables [2]bool
	for _, export := range m.Exports {
		if export.Index.Index > 1 {
			return false
		}
		switch export.Index.Kind {
		case wasm.ExternFunc:
			functions[export.Index.Index] = true
		case wasm.ExternTable:
			tables[export.Index.Index] = true
		default:
			return false
		}
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

// admitOwnedTwoTable64GetSet qualifies the same bounded two-descriptor
// directory for null-only indexed entry access. Each body is pinned to one
// table index, so no other reference flow or mutation shape is admitted.
func admitOwnedTwoTable64GetSet(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0xd0, 0x70, 0x26, byte(i), 0x20, 0, 0x25, byte(i), 0xd1, 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.Max > MaxOwnedTableEntries || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
			return false
		}
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != 0 || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 || !bytes.Equal(m.Code[i].BodyBytes, body) {
			return false
		}
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	var functions, tables [2]bool
	for _, export := range m.Exports {
		if export.Index.Index > 1 {
			return false
		}
		switch export.Index.Kind {
		case wasm.ExternFunc:
			functions[export.Index.Index] = true
		case wasm.ExternTable:
			tables[export.Index.Index] = true
		default:
			return false
		}
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func admitOwnedTwoTable64Grow(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, byte(i), 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.Max > MaxOwnedTableEntries || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
			return false
		}
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != 0 || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 || !bytes.Equal(m.Code[i].BodyBytes, body) {
			return false
		}
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I64) {
		return false
	}
	var functions, tables [2]bool
	for _, export := range m.Exports {
		if export.Index.Index > 1 {
			return false
		}
		switch export.Index.Kind {
		case wasm.ExternFunc:
			functions[export.Index.Index] = true
		case wasm.ExternTable:
			tables[export.Index.Index] = true
		default:
			return false
		}
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

// admitOwnedTwoTable64Fill qualifies null-only full-width fills against either
// descriptor in the bounded two-table directory. Each fixed body selects one
// table, and no reference value crosses the module boundary.
func admitOwnedTwoTable64Fill(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, byte(i), 0x41, 1, 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.Max > MaxOwnedTableEntries || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
			return false
		}
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != 0 || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 || !bytes.Equal(m.Code[i].BodyBytes, body) {
			return false
		}
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 2 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	var functions, tables [2]bool
	for _, export := range m.Exports {
		if export.Index.Index > 1 {
			return false
		}
		switch export.Index.Kind {
		case wasm.ExternFunc:
			functions[export.Index.Index] = true
		case wasm.ExternTable:
			tables[export.Index.Index] = true
		default:
			return false
		}
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

// admitOwnedTwoTable64Copy qualifies overlap-safe same-table copies against
// either descriptor. The distinct cross-table matcher below binds the separate
// source/destination descriptor lowering to its own exact source profile.
func admitOwnedTwoTable64Copy(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, byte(i), byte(i), 0x41, 1, 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.Max > MaxOwnedTableEntries || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
			return false
		}
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != 0 || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 || !bytes.Equal(m.Code[i].BodyBytes, body) {
			return false
		}
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 3 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[2], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	var functions, tables [2]bool
	for _, export := range m.Exports {
		if export.Index.Index > 1 {
			return false
		}
		switch export.Index.Kind {
		case wasm.ExternFunc:
			functions[export.Index.Index] = true
		case wasm.ExternTable:
			tables[export.Index.Index] = true
		default:
			return false
		}
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func admitOwnedTwoTable64CrossCopy(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, byte(i), byte(1 - i), 0x41, 1, 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.Max > MaxOwnedTableEntries || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
			return false
		}
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != 0 || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 || !bytes.Equal(m.Code[i].BodyBytes, body) {
			return false
		}
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 3 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[2], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	var functions, tables [2]bool
	for _, export := range m.Exports {
		if export.Index.Index > 1 {
			return false
		}
		switch export.Index.Kind {
		case wasm.ExternFunc:
			functions[export.Index.Index] = true
		case wasm.ExternTable:
			tables[export.Index.Index] = true
		default:
			return false
		}
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

// admitOwnedTable64Size is the first executable table64 slice. Its sole
// function has no operands and returns the descriptor's uint32 length as an
// i64, so it cannot truncate a guest table index or expose another table
// instruction through an unqualified body.
func admitOwnedTable64Size(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0].Type
	if !table.Limits.Addr64 || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) {
		return false
	}
	if m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 0 || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I64) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0xfc, 0x10, 0, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

// admitImportedTable64Size qualifies one borrowed, bounded table64 descriptor
// and a single size observer. No mutation or reference-valued boundary is
// admitted by this first imported-storage checkpoint.
func admitImportedTable64Size(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
		return false
	}
	if m.Imports[0].Type.Kind != wasm.ExternTable || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table, ok := m.TableType(0)
	if !ok || !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 0 || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I64) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0xfc, 0x10, 0, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

func admitImportedTable64GetSet(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
		return false
	}
	if m.Imports[0].Type.Kind != wasm.ExternTable || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table, ok := m.TableType(0)
	if !ok || !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x20, 0, 0x25, 0, 0xd1, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

func admitImportedTable64Grow(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
		return false
	}
	if m.Imports[0].Type.Kind != wasm.ExternTable || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table, ok := m.TableType(0)
	if !ok || !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I64) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 0, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

func admitImportedTable64Fill(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
		return false
	}
	if m.Imports[0].Type.Kind != wasm.ExternTable || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table, ok := m.TableType(0)
	if !ok || !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 2 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, 0, 0x41, 1, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

func admitImportedTable64Copy(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
		return false
	}
	if m.Imports[0].Type.Kind != wasm.ExternTable || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table, ok := m.TableType(0)
	if !ok || !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 3 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[2], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, 0, 0, 0x41, 1, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

func admitImportedTable64InitDrop(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Elements) != 1 || m.Start != nil {
		return false
	}
	if m.Imports[0].Type.Kind != wasm.ExternTable || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table, ok := m.TableType(0)
	if !ok || !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) {
		return false
	}
	for i := range m.Types {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != uint32(i) || len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
			return false
		}
		if i == 0 && (len(sub.Comp.Params) != 3 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I32) || !wasm.EqualValType(sub.Comp.Params[2], wasm.I32)) || i == 1 && len(sub.Comp.Params) != 0 {
			return false
		}
	}
	e := m.Elements[0]
	if e.Mode.Kind != wasm.ElemPassive || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 || e.Kind.Kind != wasm.ElemTypedExprs || !wasm.EqualValType(wasm.RefVal(e.Kind.Ref), wasm.FuncRef) || len(e.Kind.Funcs) != 0 || len(e.Kind.Exprs) != 1 || len(e.Kind.Exprs[0].Instrs) != 0 || !bytes.Equal(e.Kind.Exprs[0].BodyBytes, []byte{0xd0, 0x70, 0x0b}) {
		return false
	}
	if len(m.Code[0].Locals.Runs) != 0 || len(m.Code[0].Body.Instrs) != 0 || !bytes.Equal(m.Code[0].BodyBytes, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 0, 0, 0x41, 1, 0x0b}) || len(m.Code[1].Locals.Runs) != 0 || len(m.Code[1].Body.Instrs) != 0 || !bytes.Equal(m.Code[1].BodyBytes, []byte{0xfc, 0x0d, 0, 0x41, 1, 0x0b}) {
		return false
	}
	seen := [2]bool{}
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index > 1 {
				return false
			}
			seen[export.Index.Index] = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return seen[0] && seen[1]
}

// admitOwnedTable64Grow admits one null-initialized bounded grow operation.
// The exact i64 parameter/result shape is kept separate from table32 so raw
// upper input bits participate in the capacity failure check.
func admitOwnedTable64Grow(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0].Type
	if !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) {
		return false
	}
	if m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I64) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 0, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

// admitOwnedTable64GetSet admits a single null round-trip over one exact i64
// index. This qualifies both entry-address consumers without admitting a
// reference-valued public signature or arbitrary table contents.
func admitOwnedTable64GetSet(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0].Type
	if !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x20, 0, 0x25, 0, 0xd1, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

// admitOwnedTable64Indirect admits one exact local target and one exported
// caller. A declarative legacy element only declares ref.func 0; it is dropped
// before instantiation and cannot initialize table storage. The caller installs
// that target at its i64 operand and immediately calls it through table 0.
func admitOwnedTable64Indirect(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 1 || len(m.Elements) != 1 || m.Tables[0].Init != nil || m.Start != nil {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0].Type
	if !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) {
		return false
	}
	for i := range m.Types {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != uint32(i) || len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
			return false
		}
		if i == 0 && len(sub.Comp.Params) != 0 || i == 1 && (len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64)) {
			return false
		}
	}
	e := m.Elements[0]
	if e.Mode.Kind != wasm.ElemDeclarative || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 || e.Kind.Kind != wasm.ElemFuncs || len(e.Kind.Funcs) != 1 || e.Kind.Funcs[0] != 0 || len(e.Kind.Exprs) != 0 {
		return false
	}
	if len(m.Code[0].Locals.Runs) != 0 || len(m.Code[0].Body.Instrs) != 0 || !bytes.Equal(m.Code[0].BodyBytes, []byte{0x41, 7, 0x0b}) || len(m.Code[1].Locals.Runs) != 0 || len(m.Code[1].Body.Instrs) != 0 || !bytes.Equal(m.Code[1].BodyBytes, []byte{0x20, 0, 0xd2, 0, 0x26, 0, 0x20, 0, 0x11, 0, 0, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 1 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

func admitOwnedTwoTable64Indirect(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Tables) != 2 || len(m.Elements) != 1 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.Max > MaxOwnedTableEntries || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
			return false
		}
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
			return false
		}
		if i == 0 && len(sub.Comp.Params) != 0 || i == 1 && (len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64)) {
			return false
		}
	}
	for i := range m.FuncTypes {
		want := uint32(1)
		if i == 0 {
			want = 0
		}
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != want || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 {
			return false
		}
	}
	e := m.Elements[0]
	if e.Mode.Kind != wasm.ElemDeclarative || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 || e.Kind.Kind != wasm.ElemFuncs || len(e.Kind.Funcs) != 1 || e.Kind.Funcs[0] != 0 || len(e.Kind.Exprs) != 0 {
		return false
	}
	if !bytes.Equal(m.Code[0].BodyBytes, []byte{0x41, 7, 0x0b}) {
		return false
	}
	for i := 0; i < 2; i++ {
		body := []byte{0x20, 0, 0xd2, 0, 0x26, byte(i), 0x20, 0, 0x11, 0, byte(i), 0x0b}
		if !bytes.Equal(m.Code[i+1].BodyBytes, body) {
			return false
		}
	}
	var functions, tables [2]bool
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index < 1 || export.Index.Index > 2 {
				return false
			}
			functions[export.Index.Index-1] = true
		case wasm.ExternTable:
			if export.Index.Index > 1 {
				return false
			}
			tables[export.Index.Index] = true
		default:
			return false
		}
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

// admitImportedTable64Indirect qualifies a single numeric caller over one
// bounded borrowed table64 descriptor. The target comes only from the retained
// canonical table owner graph; the consumer has no local reference payload or
// initializer that can manufacture a row.
func admitImportedTable64Indirect(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
		return false
	}
	if m.Imports[0].Type.Kind != wasm.ExternTable || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table, ok := m.TableType(0)
	if !ok || !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 1 {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
			return false
		}
		if i == 0 && len(sub.Comp.Params) != 0 || i == 1 && (len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64)) {
			return false
		}
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0x20, 0, 0x11, 0, 0, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

// admitOwnedTable64Fill admits one null fill over exact i64 destination/count
// operands. No reference can enter or leave the module, and the fixed maximum
// keeps the runtime mutation bounded.
func admitOwnedTable64Fill(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0].Type
	if !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 2 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, 0, 0x41, 1, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

// admitOwnedTable64Copy admits one exact same-table copy over i64
// destination/source/count operands. Its fixed maximum bounds all movement.
func admitOwnedTable64Copy(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0].Type
	if !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	sub := m.Types[0].SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 3 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[2], wasm.I64) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, 0, 0, 0x41, 1, 0x0b}) {
		return false
	}
	hasFunctionExport := false
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index != 0 {
				return false
			}
			hasFunctionExport = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasFunctionExport
}

func admitOwnedTable64InitDrop(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 1 || len(m.Elements) != 1 || m.Tables[0].Init != nil || m.Start != nil {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0].Type
	if !table.Limits.Addr64 || !table.Limits.HasMax || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) {
		return false
	}
	for i := range m.Types {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != uint32(i) || len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
			return false
		}
		if i == 0 && (len(sub.Comp.Params) != 3 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I32) || !wasm.EqualValType(sub.Comp.Params[2], wasm.I32)) || i == 1 && len(sub.Comp.Params) != 0 {
			return false
		}
	}
	e := m.Elements[0]
	if e.Mode.Kind != wasm.ElemPassive || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 || e.Kind.Kind != wasm.ElemTypedExprs || !wasm.EqualValType(wasm.RefVal(e.Kind.Ref), wasm.FuncRef) || len(e.Kind.Funcs) != 0 || len(e.Kind.Exprs) != 1 || len(e.Kind.Exprs[0].Instrs) != 0 || !bytes.Equal(e.Kind.Exprs[0].BodyBytes, []byte{0xd0, 0x70, 0x0b}) {
		return false
	}
	if len(m.Code[0].Locals.Runs) != 0 || len(m.Code[0].Body.Instrs) != 0 || !bytes.Equal(m.Code[0].BodyBytes, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 0, 0, 0x41, 1, 0x0b}) || len(m.Code[1].Locals.Runs) != 0 || len(m.Code[1].Body.Instrs) != 0 || !bytes.Equal(m.Code[1].BodyBytes, []byte{0xfc, 0x0d, 0, 0x41, 1, 0x0b}) {
		return false
	}
	seen := [2]bool{}
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index > 1 {
				return false
			}
			seen[export.Index.Index] = true
		case wasm.ExternTable:
			if export.Index.Index != 0 {
				return false
			}
		default:
			return false
		}
	}
	return seen[0] && seen[1]
}

func admitOwnedTwoTable64InitDrop(m *wasm.Module, production, public bool) bool {
	if !production || !public || len(m.Imports) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Tables) != 2 || len(m.Elements) != 1 || m.Start != nil || len(m.Exports) != 5 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > MaxOwnedTableEntries || table.Type.Limits.Max > MaxOwnedTableEntries || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
			return false
		}
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
			return false
		}
		if i == 0 && (len(sub.Comp.Params) != 3 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I64) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I32) || !wasm.EqualValType(sub.Comp.Params[2], wasm.I32)) || i == 1 && len(sub.Comp.Params) != 0 {
			return false
		}
	}
	for i := range m.FuncTypes {
		want := uint32(0)
		if i == 2 {
			want = 1
		}
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != want || len(m.Code[i].Locals.Runs) != 0 || len(m.Code[i].Body.Instrs) != 0 {
			return false
		}
	}
	e := m.Elements[0]
	if e.Mode.Kind != wasm.ElemPassive || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 || e.Kind.Kind != wasm.ElemTypedExprs || !wasm.EqualValType(wasm.RefVal(e.Kind.Ref), wasm.FuncRef) || len(e.Kind.Funcs) != 0 || len(e.Kind.Exprs) != 1 || len(e.Kind.Exprs[0].Instrs) != 0 || !bytes.Equal(e.Kind.Exprs[0].BodyBytes, []byte{0xd0, 0x70, 0x0b}) {
		return false
	}
	for i := 0; i < 2; i++ {
		body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 0, byte(i), 0x41, 1, 0x0b}
		if !bytes.Equal(m.Code[i].BodyBytes, body) {
			return false
		}
	}
	if !bytes.Equal(m.Code[2].BodyBytes, []byte{0xfc, 0x0d, 0, 0x41, 1, 0x0b}) {
		return false
	}
	var functions [3]bool
	var tables [2]bool
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternFunc:
			if export.Index.Index > 2 {
				return false
			}
			functions[export.Index.Index] = true
		case wasm.ExternTable:
			if export.Index.Index > 1 {
				return false
			}
			tables[export.Index.Index] = true
		default:
			return false
		}
	}
	return functions[0] && functions[1] && functions[2] && tables[0] && tables[1]
}

// Imported storage uses the retained canonical target ABI, never the private
// same-instance pilot. Count and capacity limits precede validation/copies.
func admitTableProfile(m *wasm.Module, production, importedTables bool) (bool, error) {
	if err := ownedTableBounds(m, production && importedTables); err != nil {
		return false, err
	}
	if m.ImportedTableCount() != 0 && (!production || !importedTables) {
		return false, unsupported("imported table requires the retained public table ABI")
	}
	hasTable := len(m.Tables)+m.ImportedTableCount() != 0
	if !hasTable && len(m.Elements) == 0 {
		return false, nil
	}
	if !production || !hasTable && !importedTables {
		return false, unsupported("isolated private table")
	}
	if hasTable {
		t, _ := m.TableType(0)
		if !wasm.EqualValType(wasm.RefVal(t.Ref), wasm.FuncRef) || t.Limits.Addr64 && !admitStateOnlyTable64(m, production, importedTables) && !admitOwnedTwoTable64Size(m, production, importedTables) && !admitOwnedTwoTable64GetSet(m, production, importedTables) && !admitOwnedTwoTable64Grow(m, production, importedTables) && !admitOwnedTwoTable64Fill(m, production, importedTables) && !admitOwnedTwoTable64Copy(m, production, importedTables) && !admitOwnedTwoTable64CrossCopy(m, production, importedTables) && !admitOwnedTwoTable64InitDrop(m, production, importedTables) && !admitOwnedTwoTable64Indirect(m, production, importedTables) && !admitOwnedTable64Size(m, production, importedTables) && !admitOwnedTable64Grow(m, production, importedTables) && !admitOwnedTable64GetSet(m, production, importedTables) && !admitOwnedTable64Indirect(m, production, importedTables) && !admitImportedTable64Indirect(m, production, importedTables) && !admitOwnedTable64Fill(m, production, importedTables) && !admitOwnedTable64Copy(m, production, importedTables) && !admitOwnedTable64InitDrop(m, production, importedTables) && !admitImportedTable64Size(m, production, importedTables) && !admitImportedTable64GetSet(m, production, importedTables) && !admitImportedTable64Grow(m, production, importedTables) && !admitImportedTable64Fill(m, production, importedTables) && !admitImportedTable64Copy(m, production, importedTables) && !admitImportedTable64InitDrop(m, production, importedTables) {
			return false, unsupported("private nullable funcref table32 only")
		}
	}
	if len(m.Tables) != 0 && m.Tables[0].Init != nil {
		if !importedTables {
			return false, unsupported("table initializer requires retained public table ABI")
		}
		if _, err := admitTableInitializer(*m.Tables[0].Init, m); err != nil {
			return false, err
		}
	}
	imported := m.ImportedFuncCount()
	for _, e := range m.Elements {
		if !hasTable && e.Mode.Kind != wasm.ElemDeclarative && e.Mode.Kind != wasm.ElemPassive {
			return false, unsupported("table-free elements must be passive or declarative")
		}
		if (e.Mode.Kind != wasm.ElemActive && e.Mode.Kind != wasm.ElemPassive && e.Mode.Kind != wasm.ElemDeclarative) || e.Mode.Table != 0 {
			return false, unsupported("element mode/table")
		}
		switch e.Kind.Kind {
		case wasm.ElemFuncs:
			if len(e.Kind.Exprs) != 0 {
				return false, unsupported("legacy element expression payload")
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			// Flag4 has an implicit funcref type; its decoder leaves Ref unset.
			if !importedTables || len(e.Kind.Funcs) != 0 || e.Kind.Kind == wasm.ElemTypedExprs && !wasm.EqualValType(wasm.RefVal(e.Kind.Ref), wasm.FuncRef) {
				return false, unsupported("public nullable funcref element expressions required")
			}
			for _, expr := range e.Kind.Exprs {
				value, err := elementInitializerExpression(expr, m, e.Mode.Kind, production && importedTables)
				if err != nil {
					return false, err
				}
				if !value.Null && !value.HasGlobal && uint64(value.FuncIndex) >= uint64(imported+len(m.Code)) {
					return false, unsupported("element expression function index")
				}
			}
		default:
			return false, unsupported("element payload kind")
		}
		if e.Mode.Kind == wasm.ElemActive {
			if err := admitI32Offset(e.Mode.Offset, m, importedTables); err != nil {
				return false, err
			}
		} else if len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 {
			return false, unsupported("inactive element offset")
		}
		for _, f := range e.Kind.Funcs {
			if uint64(f) < uint64(imported) && !importedTables || uint64(f) >= uint64(imported+len(m.Code)) {
				return false, unsupported("element function index/profile")
			}
		}
	}
	return hasTable, nil
}

func (f *integerEmitter) indirectCall(sig functionSpec, base, indexSlot int, indexWide bool, tableIndex uint32) {
	f.Load(5, f.SpillOffset(indexSlot), indexWide) // table32 zero-normalizes dirty incoming i32; table64 preserves all bits
	f.ownedIndirectTargetAt(sig.key, tableIndex)
	// All dynamic guards precede writes to potentially smaller shared call arenas.
	f.restoreCallTransport()
	for j, wide := range sig.params {
		f.Load(5, f.SpillOffset(base+j), wide)
		f.a.Sd(enc.X5, enc.X10, int32(j*8))
	}
	if f.mode.foreignTableCalls {
		f.nativeTableCall(sig.key, len(sig.params), len(sig.results))
	} else {
		f.a.Jalr(enc.RA, enc.X28, 0)
	}
	f.restoreCallTransport()
	for j, wide := range sig.results {
		if wide {
			f.a.Ld(enc.X5, enc.X13, int32(j*8))
		} else {
			f.a.Lwu(enc.X5, enc.X13, int32(j*8))
		}
		f.Store(f.SpillOffset(base+j), 5, wide)
	}
}

// X5 is the normalized table index on entry; X28 is the guarded framed
// target on success. All guards finish before either call arena is written.
func (f *integerEmitter) ownedIndirectTarget(key uint64) {
	f.ownedIndirectTargetAt(key, 0)
}

func (f *integerEmitter) ownedIndirectTargetAt(key uint64, tableIndex uint32) {
	if tableIndex == 0 {
		f.a.Ld(enc.X6, enc.X25, -80)
	} else {
		f.a.Ld(enc.X6, enc.X25, -abi.TableDirPtrOffset)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
		f.a.Ld(enc.X6, enc.X6, int32(tableIndex*8))
	}
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Lwu(enc.X7, enc.X6, 0)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.GEU, enc.X5, enc.X7))
	f.a.Slli(enc.X5, enc.X5, 5)
	f.a.Add(enc.X6, enc.X6, enc.X5)
	// The descriptor's eight-byte header precedes 32-byte entries.
	f.a.Ld(enc.X28, enc.X6, 8)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	f.a.Ld(enc.X7, enc.X6, 16)
	f.a.Constant64(enc.X5, int64(key))
	f.trap(trapIndirectSignature, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	if f.mode.foreignTableCalls {
		// The owner supplies retained immutable canonical descriptors. Verify the
		// copied table row still names that exact entry/key/home before root writes.
		f.a.Ld(enc.X14, enc.X6, 32)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X14, enc.Zero))
		f.a.Constant64(enc.X5, 7)
		f.a.And(enc.X5, enc.X14, enc.X5)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X5, enc.Zero))
		f.a.Ld(enc.X7, enc.X14, 0)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X28))
		f.a.Ld(enc.X7, enc.X14, 8)
		f.a.Constant64(enc.X5, int64(key))
		f.trap(trapIndirectSignature, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
		f.a.Ld(enc.X7, enc.X14, 16)
		f.a.Ld(enc.X5, enc.X6, 24)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
		return
	}
	// Preserve the framed local-wrapper and exact instance identity contracts.
	// Sharing linear memory does not make two consumers the same instance.
	f.a.Ld(enc.X7, enc.X6, 24)
	f.a.Constant64(enc.X5, int64(abi.FuncRefLocalWrapperHomeTag))
	f.a.Or(enc.X5, enc.X5, enc.X25)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	f.a.Ld(enc.X7, enc.X6, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X7, enc.Zero))
	f.a.Ld(enc.X7, enc.X7, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X7, enc.Zero))
	f.a.Ld(enc.X6, enc.X25, -abi.FuncRefDescPtrOffset)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Ld(enc.X6, enc.X6, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X6))
}

// Private references are immutable canonical descriptor handles, never numeric
// carriers. Public scopes also admit retained foreign local-wrapper descriptors.
func (f *integerEmitter) functionReference(index uint32) {
	f.a.Ld(enc.X5, enc.X25, -abi.FuncRefDescPtrOffset)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
	f.a.Constant64(enc.X6, int64(index+1)*40)
	f.a.Add(enc.X5, enc.X5, enc.X6)
}

func (f *integerEmitter) privateTableDescriptor(dst enc.Reg, index uint32) {
	if index == 0 {
		f.a.Ld(dst, enc.X25, -80)
		return
	}
	f.a.Ld(dst, enc.X25, -abi.TableDirPtrOffset)
	f.trap(trapTableBounds, f.a.BranchLong(enc.EQ, dst, enc.Zero))
	f.a.Ld(dst, dst, int32(index*8))
	f.trap(trapTableBounds, f.a.BranchLong(enc.EQ, dst, enc.Zero))
}

// Returns the entry address in X6. X28 may hold a full-width reference.
func (f *integerEmitter) privateTableEntry(index uint32) {
	f.privateTableDescriptor(enc.X6, index)
	f.trap(trapTableBounds, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Lwu(enc.X7, enc.X6, 0)
	f.trap(trapTableBounds, f.a.BranchLong(enc.GEU, enc.X5, enc.X7))
	f.a.Slli(enc.X5, enc.X5, 5)
	f.a.Add(enc.X6, enc.X6, enc.X5)
	f.a.Addi(enc.X6, enc.X6, 8)
}

func (f *integerEmitter) storePrivateReference() {
	null := f.a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	if f.mode.foreignTableCalls {
		f.canonicalReferenceGuard(enc.X28, enc.X5, enc.X7)
	} else {
		// The table can only hold the current instance's local wrapper descriptors.
		// Validate the complete tag/context before the first write.
		f.a.Ld(enc.X7, enc.X28, 16)
		f.a.Constant64(enc.X5, int64(abi.FuncRefLocalWrapperHomeTag))
		f.a.Or(enc.X5, enc.X5, enc.X25)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
		f.a.Ld(enc.X7, enc.X28, 32)
		f.a.Ld(enc.X5, enc.X25, -abi.FuncRefDescPtrOffset)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
		f.a.Ld(enc.X5, enc.X5, 32)
		f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X7, enc.X5))
	}
	// No interrupt poll splits an entry: reuse never observes a partial copy.
	for offset := int32(0); offset < 32; offset += 8 {
		f.a.Ld(enc.X7, enc.X28, offset)
		f.a.Sd(enc.X7, enc.X6, offset)
	}
	done := f.Jump()
	if err := f.Patch(null, f.Position()); err != nil {
		f.err = err
	}
	for offset := int32(0); offset < 32; offset += 8 {
		f.a.Sd(enc.Zero, enc.X6, offset)
	}
	if err := f.Patch(done, f.Position()); err != nil {
		f.err = err
	}
}

// Public reference carriers can only originate from ref.func, authenticated
// table.get, null and their full-width local/control copies. Runtime scopes
// retain every possible descriptor owner through entry/resume/cold unwinding.
// A copied carrier must still be the exact immutable local-wrapper descriptor.
// No signature equality is required for table storage; call_indirect checks it.
func (f *integerEmitter) canonicalReferenceGuard(reference, first, second enc.Reg) {
	f.a.Constant64(first, 7)
	f.a.And(first, reference, first)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, first, enc.Zero))
	f.a.Ld(first, reference, 24)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, first, reference))
	f.a.Ld(first, reference, 16)
	f.a.Srli(first, first, abi.FuncRefEntryTagShift)
	f.a.Constant64(second, int64(abi.FuncRefLocalWrapperTagValue))
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, first, second))
	f.a.Ld(first, reference, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, first, enc.Zero))
	f.a.Ld(first, reference, 0)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, first, enc.Zero))
}
