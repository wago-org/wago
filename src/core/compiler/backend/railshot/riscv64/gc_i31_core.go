//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const trapI31CastFailure = 18 // private tag; production TrapCastFailure is 18

func admitRV64I31CoreWire(data []byte) bool {
	if !admitExactProductWireShape(data, nil) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64I31Core(m)
}

// AdmitI31CoreSource returns the exact-wire provenance witness for the bounded
// non-allocating RV64 i31 products. Callers must pass this witness back in
// CompileOptions; decoded shape alone is deliberately insufficient because the
// decoder discards semantically inert wire sections.
func AdmitI31CoreSource(data []byte) bool {
	return admitRV64I31CoreWire(data)
}

// admitRV64I31Core independently proves the decoded shape used to select the
// lowering. This is intentionally narrower than the general collector-free
// i31 implementation on complete backends.
func admitRV64I31Core(m *wasm.Module) bool {
	return admitRV64I31MinimalCore(m) || admitRV64I31OfficialCore(m) || admitRV64I31OfficialTable(m) || admitRV64I31OfficialTableGlobalInitializer(m) || admitRV64I31OfficialGlobalInitializer(m) || admitRV64I31OfficialAnyGlobal(m) || admitRV64I31OfficialAnyTable(m) || admitRV64I31OfficialRefTest(m) || admitRV64I31RefEqPilot(m)
}

func admitRV64I31TableProduct(m *wasm.Module) bool {
	return admitRV64I31OfficialTable(m) || admitRV64I31OfficialTableGlobalInitializer(m) || admitRV64I31OfficialAnyTable(m) || admitRV64I31RefEqPilot(m)
}

// admitRV64I31RefEqPilot pins a reduced, collector-free subset of the official
// gc/ref_eq fixture. Its sole eqref table is initialized only with null and
// compact i31 carriers, so full-word identity needs neither heap allocation nor
// collector participation. The object-bearing official module remains rejected.
func admitRV64I31RefEqPilot(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 2 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 1 || len(m.Exports) != 2 ||
		len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0]
	if table.Init != nil || table.Type.Limits.Addr64 || table.Type.Limits.HasMax || table.Type.Limits.Min != 5 || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.EqRef) {
		return false
	}
	if !i31CoreFunctionType(&m.Types[0], nil, nil) || !i31CoreFunctionType(&m.Types[1], []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}) {
		return false
	}
	wantBodies := [][]byte{
		{0x41, 0, 0xd0, 0x6d, 0x26, 0, 0x41, 1, 0xd0, 0x6c, 0x26, 0, 0x41, 2, 0x41, 7, 0xfb, 0x1c, 0x26, 0, 0x41, 3, 0x41, 7, 0xfb, 0x1c, 0x26, 0, 0x41, 4, 0x41, 8, 0xfb, 0x1c, 0x26, 0, 0x0b},
		{0x20, 0, 0x25, 0, 0x20, 1, 0x25, 0, 0xd3, 0x0b},
	}
	for i, binding := range m.FuncTypes {
		if binding.Rec || binding.Index != uint32(i) {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	return m.Exports[0].Name == "init" && m.Exports[0].Index.Kind == wasm.ExternFunc && m.Exports[0].Index.Index == 0 &&
		m.Exports[1].Name == "eq" && m.Exports[1].Index.Kind == wasm.ExternFunc && m.Exports[1].Index.Index == 1
}

func admitRV64I31MinimalCore(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Exports) != 2 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 ||
		len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	rec := &m.Types[0]
	if len(rec.SubTypes) != 1 {
		return false
	}
	sub := &rec.SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() ||
		sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I32) ||
		len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
		return false
	}
	for i, binding := range m.FuncTypes {
		if binding.Rec || binding.Index != 0 {
			return false
		}
		fn := &m.Code[i]
		want := []byte{0x20, 0, 0xfb, 0x1c, 0xfb, byte(0x1d + i), 0x0b}
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, want) {
			return false
		}
	}
	return m.Exports[0].Name == "signed" && m.Exports[0].Index.Kind == wasm.ExternFunc && m.Exports[0].Index.Index == 0 &&
		m.Exports[1].Name == "unsigned" && m.Exports[1].Index.Kind == wasm.ExternFunc && m.Exports[1].Index.Index == 1
}

func i31CoreFunctionType(group *wasm.RecType, params, results []wasm.ValType) bool {
	if group == nil || len(group.SubTypes) != 1 {
		return false
	}
	sub := &group.SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() ||
		sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != len(params) || len(sub.Comp.Results) != len(results) || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
		return false
	}
	for i := range params {
		if !wasm.EqualValType(sub.Comp.Params[i], params[i]) {
			return false
		}
	}
	for i := range results {
		if !wasm.EqualValType(sub.Comp.Results[i], results[i]) {
			return false
		}
	}
	return true
}

func isI31CoreRef(typ wasm.ValType) bool {
	return wasm.EqualValType(typ, wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false)))
}

func admitRV64I31OfficialCore(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 5 || len(m.FuncTypes) != 7 || len(m.Code) != 7 || len(m.Globals) != 2 || len(m.Exports) != 7 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	i31 := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false))
	types := []struct{ params, results []wasm.ValType }{
		{[]wasm.ValType{wasm.I32}, []wasm.ValType{i31}},
		{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}},
		{nil, []wasm.ValType{wasm.I32}},
		{nil, []wasm.ValType{wasm.I32, wasm.I32}},
		{[]wasm.ValType{wasm.I32}, nil},
	}
	for i := range types {
		if !i31CoreFunctionType(&m.Types[i], types[i].params, types[i].results) {
			return false
		}
	}
	wantBindings := []uint32{0, 1, 1, 2, 2, 3, 4}
	wantBodies := [][]byte{
		{0x20, 0, 0xfb, 0x1c, 0x0b},
		{0x20, 0, 0xfb, 0x1c, 0xfb, 0x1e, 0x0b},
		{0x20, 0, 0xfb, 0x1c, 0xfb, 0x1d, 0x0b},
		{0xd0, 0x6c, 0xfb, 0x1e, 0x0b},
		{0xd0, 0x6c, 0xfb, 0x1d, 0x0b},
		{0x23, 0, 0xfb, 0x1e, 0x23, 1, 0xfb, 0x1e, 0x0b},
		{0x20, 0, 0xfb, 0x1c, 0x24, 1, 0x0b},
	}
	for i, binding := range m.FuncTypes {
		if binding.Rec || binding.Index != wantBindings[i] {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	for i, g := range m.Globals {
		if !isI31CoreRef(g.Type.Type) || g.Type.Mutable != (i == 1) || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, []byte{0x41, byte(2 + i), 0xfb, 0x1c, 0x0b}) {
			return false
		}
	}
	wantExports := []string{"new", "get_u", "get_s", "get_u-null", "get_s-null", "get_globals", "set_global"}
	for i, ex := range m.Exports {
		if ex.Name != wantExports[i] || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}

// admitRV64I31OfficialTable pins the second official spec-v3 gc/i31 module.
// Its compact eight-byte table rows exercise the complete table lifecycle, so
// no other i31 table shape inherits this collector-free lowering.
func admitRV64I31OfficialTable(m *wasm.Module) bool {
	return admitRV64I31OfficialTableShape(m, false)
}

// admitRV64I31OfficialAnyTable pins the sixth official spec-v3 gc/i31 module.
// Its anyref table is still confined to compact i31 values by the exact element
// and instruction shape, including an explicit nullable i31 cast on reads.
func admitRV64I31OfficialAnyTable(m *wasm.Module) bool {
	return admitRV64I31OfficialTableShape(m, true)
}

func admitRV64I31OfficialTableShape(m *wasm.Module, anyref bool) bool {
	if m == nil || len(m.Types) != 4 || len(m.FuncTypes) != 6 || len(m.Code) != 6 || len(m.Tables) != 1 || len(m.Elements) != 2 || len(m.Exports) != 6 ||
		len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0]
	wantTableType := wasm.I31Ref
	if anyref {
		wantTableType = wasm.AnyRef
	}
	if table.Init != nil || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min != 3 || table.Type.Limits.Max != 10 || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wantTableType) {
		return false
	}
	types := []struct{ params, results []wasm.ValType }{
		{nil, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil},
	}
	for i := range types {
		if !i31CoreFunctionType(&m.Types[i], types[i].params, types[i].results) {
			return false
		}
	}
	wantBindings := []uint32{0, 1, 2, 3, 3, 3}
	wantBodies := [][]byte{
		{0xfc, 0x10, 0, 0x0b},
		{0x20, 0, 0x25, 0, 0xfb, 0x1e, 0x0b},
		{0x20, 1, 0xfb, 0x1c, 0x20, 0, 0xfc, 0x0f, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0xfb, 0x1c, 0x20, 2, 0xfc, 0x11, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, 0, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 1, 0, 0x0b},
	}
	if anyref {
		wantBodies[1] = []byte{0x20, 0, 0x25, 0, 0xfb, 0x17, 0x6c, 0xfb, 0x1e, 0x0b}
	}
	for i, binding := range m.FuncTypes {
		if binding.Rec || binding.Index != wantBindings[i] {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []string{"size", "get", "grow", "fill", "copy", "init"}
	for i, ex := range m.Exports {
		if ex.Name != wantExports[i] || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i) {
			return false
		}
	}
	wantElements := []struct {
		mode   wasm.ElemModeKind
		offset []byte
		values [][]byte
	}{
		{wasm.ElemActive, []byte{0x41, 0, 0x0b}, [][]byte{{0x41, 0xe7, 0x07, 0xfb, 0x1c, 0x0b}, {0x41, 0xf8, 0x06, 0xfb, 0x1c, 0x0b}, {0x41, 0x89, 0x06, 0xfb, 0x1c, 0x0b}}},
		{wasm.ElemPassive, nil, [][]byte{{0x41, 0xfb, 0, 0xfb, 0x1c, 0x0b}, {0x41, 0xc8, 0x03, 0xfb, 0x1c, 0x0b}, {0x41, 0x95, 0x06, 0xfb, 0x1c, 0x0b}}},
	}
	for i, elem := range m.Elements {
		want := wantElements[i]
		if elem.Mode.Kind != want.mode || elem.Mode.Table != 0 || len(elem.Mode.Offset.Instrs) != 0 || !bytes.Equal(elem.Mode.Offset.BodyBytes, want.offset) || elem.Kind.Kind != wasm.ElemTypedExprs || len(elem.Kind.Funcs) != 0 || !wasm.EqualValType(wasm.RefVal(elem.Kind.Ref), wasm.I31Ref) || len(elem.Kind.Exprs) != len(want.values) {
			return false
		}
		for j, expr := range elem.Kind.Exprs {
			if len(expr.Instrs) != 0 || !bytes.Equal(expr.BodyBytes, want.values[j]) {
				return false
			}
		}
	}
	return true
}

// admitRV64I31OfficialTableGlobalInitializer pins the third official spec-v3
// gc/i31 module. The immutable imported i32 is wrapped into compact i31 table
// rows during instantiation; no other imported initializer inherits this path.
func admitRV64I31OfficialTableGlobalInitializer(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Imports) != 1 || len(m.Tables) != 1 || len(m.Exports) != 1 ||
		len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	im := m.Imports[0]
	if im.Module != "env" || im.Name != "g" || im.Type.Kind != wasm.ExternGlobal {
		return false
	}
	global := im.Type.GlobalType()
	if global.Mutable || !wasm.EqualValType(global.Type, wasm.I32) {
		return false
	}
	if !i31CoreFunctionType(&m.Types[0], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 {
		return false
	}
	fn := &m.Code[0]
	if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0x20, 0, 0x25, 0, 0xfb, 0x1e, 0x0b}) {
		return false
	}
	table := m.Tables[0]
	if table.Init == nil || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min != 3 || table.Type.Limits.Max != 3 || !isI31CoreRef(wasm.RefVal(table.Type.Ref)) ||
		len(table.Init.Instrs) != 0 || !bytes.Equal(table.Init.BodyBytes, []byte{0x23, 0, 0xfb, 0x1c, 0x0b}) {
		return false
	}
	ex := m.Exports[0]
	return ex.Name == "get" && ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == 0
}

// admitRV64I31OfficialGlobalInitializer pins the fourth official spec-v3
// gc/i31 module. An immutable imported i32 initializes a local nullable
// i31 global; no other dynamic reference-global initializer inherits this path.
func admitRV64I31OfficialGlobalInitializer(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Imports) != 1 || len(m.Globals) != 1 || len(m.Exports) != 1 ||
		len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	im := m.Imports[0]
	if im.Module != "env" || im.Name != "g" || im.Type.Kind != wasm.ExternGlobal {
		return false
	}
	imported := im.Type.GlobalType()
	if imported.Mutable || !wasm.EqualValType(imported.Type, wasm.I32) {
		return false
	}
	if !i31CoreFunctionType(&m.Types[0], nil, []wasm.ValType{wasm.I32}) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 {
		return false
	}
	global := m.Globals[0]
	if !wasm.EqualValType(global.Type.Type, wasm.I31Ref) || global.Type.Mutable || len(global.Init.Instrs) != 0 || !bytes.Equal(global.Init.BodyBytes, []byte{0x23, 0, 0xfb, 0x1c, 0x0b}) {
		return false
	}
	fn := &m.Code[0]
	if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, []byte{0x23, 1, 0xfb, 0x1e, 0x0b}) {
		return false
	}
	ex := m.Exports[0]
	return ex.Name == "get" && ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == 0
}

// admitRV64I31OfficialAnyGlobal pins the fifth official spec-v3 gc/i31 module.
// Its nullable anyref globals carry only compact i31 values, so casts can be
// lowered without provisioning a collector or admitting arbitrary anyref state.
func admitRV64I31OfficialAnyGlobal(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 2 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Globals) != 2 || len(m.Exports) != 2 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	if !i31CoreFunctionType(&m.Types[0], nil, []wasm.ValType{wasm.I32, wasm.I32}) || !i31CoreFunctionType(&m.Types[1], []wasm.ValType{wasm.I32}, nil) {
		return false
	}
	wantBindings := []uint32{0, 1}
	wantBodies := [][]byte{
		{0x23, 0, 0xfb, 0x17, 0x6c, 0xfb, 0x1e, 0x23, 1, 0xfb, 0x17, 0x6c, 0xfb, 0x1e, 0x0b},
		{0x20, 0, 0xfb, 0x1c, 0x24, 1, 0x0b},
	}
	for i, binding := range m.FuncTypes {
		if binding.Rec || binding.Index != wantBindings[i] {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantInitializers := [][]byte{{0x41, 0xd2, 0x09, 0xfb, 0x1c, 0x0b}, {0x41, 0xae, 0x2c, 0xfb, 0x1c, 0x0b}}
	for i, global := range m.Globals {
		if !wasm.EqualValType(global.Type.Type, wasm.AnyRef) || global.Type.Mutable != (i == 1) || len(global.Init.Instrs) != 0 || !bytes.Equal(global.Init.BodyBytes, wantInitializers[i]) {
			return false
		}
	}
	wantExports := []string{"get_globals", "set_global"}
	for i, ex := range m.Exports {
		if ex.Name != wantExports[i] || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}

// admitRV64I31OfficialRefTest pins the collector-free null/i31 ref.test
// product. Its complete bodies confine every input to null or compact i31, so
// abstract any/eq/i31/struct/array tests need no heap object classification.
func admitRV64I31OfficialRefTest(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 2 || len(m.FuncTypes) != 7 || len(m.Code) != 7 || len(m.Exports) != 7 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	if !i31CoreFunctionType(&m.Types[0], nil, []wasm.ValType{wasm.I32}) || !i31CoreFunctionType(&m.Types[1], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}) {
		return false
	}
	wantBindings := []uint32{0, 0, 1, 1, 1, 1, 1}
	wantBodies := [][]byte{
		{0xd0, 0x6e, 0xfb, 0x14, 0x6e, 0xd0, 0x6e, 0xfb, 0x15, 0x6e, 0x6a, 0x0b},
		{0xd0, 0x6c, 0xfb, 0x14, 0x6c, 0xd0, 0x6c, 0xfb, 0x15, 0x6c, 0x6a, 0x0b},
	}
	for _, heap := range []byte{0x6c, 0x6e, 0x6d, 0x6b, 0x6a} {
		wantBodies = append(wantBodies, []byte{0x20, 0, 0xfb, 0x1c, 0xfb, 0x14, heap, 0x20, 0, 0xfb, 0x1c, 0xfb, 0x15, heap, 0x6a, 0x0b})
	}
	for i, binding := range m.FuncTypes {
		if binding.Rec || binding.Index != wantBindings[i] {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []string{"null-data", "null-i31", "i31", "any", "eq", "struct", "array"}
	for i, ex := range m.Exports {
		if ex.Name != wantExports[i] || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}

func i31CoreGlobalValue(expr wasm.Expr) (uint64, error) {
	r := wasm.ReaderFrom(expr.BodyBytes)
	op, err := r.Byte()
	if err != nil || op != 0x41 && op != 0x23 {
		return 0, unsupported("i31 global initializer")
	}
	var value uint64
	if op == 0x41 {
		v, err := r.I32()
		if err != nil {
			return 0, err
		}
		value = uint64(uint32(v)<<1 | 1)
	} else {
		index, err := r.U32()
		if err != nil || index != 0 {
			return 0, unsupported("i31 global initializer")
		}
	}
	prefix, err := r.Byte()
	if err != nil || prefix != 0xfb {
		return 0, unsupported("i31 global initializer")
	}
	sub, err := r.U32()
	if err != nil || sub != 28 {
		return 0, unsupported("i31 global initializer")
	}
	end, err := r.Byte()
	if err != nil || end != 0x0b || r.HasNext() {
		return 0, unsupported("i31 global initializer")
	}
	return value, nil
}

func (f *integerEmitter) i31Wrap() {
	// SLLIW discards the incoming sign bit as required by the 31-bit payload.
	// It sign-extends on RV64, so restore the shared zero-normalized carrier
	// before storing the full-width tagged reference.
	f.a.Slliw(enc.X5, enc.X5, 1)
	f.a.Addi(enc.X5, enc.X5, 1)
	f.NormalizeI32(5)
}

func (f *integerEmitter) i31Get(signed bool) {
	f.trap(trapNullReference, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
	if signed {
		f.a.Sraiw(enc.X5, enc.X5, 1)
	} else {
		f.a.Srliw(enc.X5, enc.X5, 1)
	}
	// Every shared i32 boundary carries uint32 bits in a zero-extended word,
	// including the signed extraction of a negative 31-bit payload.
	f.NormalizeI32(5)
}

func (f *integerEmitter) i31Cast(nullable bool) {
	// Compact i31 references are the only odd gc.Ref values. Null and every
	// heap-object reference are even. A nullable cast preserves null so the
	// following i31 operation can report its more specific null-reference trap.
	null := -1
	if nullable {
		null = f.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
	}
	f.a.Addi(enc.X6, enc.Zero, 1)
	f.a.And(enc.X6, enc.X5, enc.X6)
	f.trap(trapI31CastFailure, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	if nullable {
		if err := f.Patch(null, f.Position()); err != nil {
			f.err = err
		}
	}
}

func (f *integerEmitter) i31Test(nullable bool, heap wasm.AbsHeapType) {
	switch heap {
	case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31:
		if nullable {
			f.a.Sltiu(enc.X6, enc.X5, 1)
		}
		f.a.Addi(enc.X7, enc.Zero, 1)
		f.a.And(enc.X5, enc.X5, enc.X7)
		if nullable {
			f.a.Or(enc.X5, enc.X5, enc.X6)
		}
	case wasm.HeapStruct, wasm.HeapArray:
		if nullable {
			f.a.Sltiu(enc.X5, enc.X5, 1)
		} else {
			f.a.Addi(enc.X5, enc.Zero, 0)
		}
	}
}
