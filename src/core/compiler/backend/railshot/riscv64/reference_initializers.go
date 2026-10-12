//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// admitStateOnlyMutableFunctionGlobalImport is the source-side counterpart of
// the runtime artifact witness. It admits one mutable nullable funcref import
// only when the module does nothing except re-export that exact cell.
func admitStateOnlyMutableFunctionGlobalImport(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 0 || len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Imports) != 1 || len(m.Globals) != 0 || len(m.Exports) != 1 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	imp := m.Imports[0]
	if imp.Type.Kind != wasm.ExternGlobal {
		return false
	}
	typ := imp.Type.GlobalType()
	ex := m.Exports[0]
	return typ.Mutable && wasm.EqualValType(typ.Type, wasm.FuncRef) && ex.Index.Kind == wasm.ExternGlobal && ex.Index.Index == 0
}

// admitMutableFunctionGlobalGet is the first executable mutable-import shape.
// Its sole function can only observe whether the imported cell is null; no raw
// descriptor escapes and no guest write bypasses the container ownership path.
func admitMutableFunctionGlobalGet(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Imports) != 1 || len(m.Globals) != 0 || len(m.Exports) != 2 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	imp := m.Imports[0]
	if imp.Type.Kind != wasm.ExternGlobal {
		return false
	}
	typ := imp.Type.GlobalType()
	sig, ok := m.FuncSignature(0)
	if !ok || sig.Kind != wasm.CompFunc || len(sig.Params) != 0 || len(sig.Results) != 1 || !wasm.EqualValType(sig.Results[0], wasm.I32) || m.Code[0].LocalDeclBytes != 1 || len(m.Code[0].Locals.Runs) != 0 || !bytes.Equal(m.Code[0].BodyBytes, []byte{0x23, 0, 0xd1, 0x0b}) {
		return false
	}
	functionExport, globalExport := false, false
	for _, ex := range m.Exports {
		functionExport = functionExport || ex.Name == "is_null" && ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == 0
		globalExport = globalExport || ex.Name == "cell" && ex.Index.Kind == wasm.ExternGlobal && ex.Index.Index == 0
	}
	return typ.Mutable && wasm.EqualValType(typ.Type, wasm.FuncRef) && functionExport && globalExport
}

// admitMutableFunctionGlobalClear is the first guest write boundary for an
// imported reference container. The exact body can only replace the sole
// nullable funcref cell with null; lowering routes the mutation through the
// parked runtime ownership transaction instead of storing a raw descriptor.
func admitMutableFunctionGlobalClear(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Imports) != 1 || len(m.Globals) != 0 || len(m.Exports) != 2 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	imp := m.Imports[0]
	if imp.Type.Kind != wasm.ExternGlobal {
		return false
	}
	typ := imp.Type.GlobalType()
	sig, ok := m.FuncSignature(0)
	if !ok || sig.Kind != wasm.CompFunc || len(sig.Params) != 0 || len(sig.Results) != 0 || m.Code[0].LocalDeclBytes != 1 || len(m.Code[0].Locals.Runs) != 0 || !bytes.Equal(m.Code[0].BodyBytes, []byte{0xd0, 0x70, 0x24, 0, 0x0b}) {
		return false
	}
	functionExport, globalExport := false, false
	for _, ex := range m.Exports {
		functionExport = functionExport || ex.Name == "clear" && ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == 0
		globalExport = globalExport || ex.Name == "cell" && ex.Index.Kind == wasm.ExternGlobal && ex.Index.Index == 0
	}
	return typ.Mutable && wasm.EqualValType(typ.Type, wasm.FuncRef) && functionExport && globalExport
}

// admitMutableFunctionGlobalSetSelf is the first non-null guest write boundary.
// The exact body can only publish its own sole exported function into imported
// mutable global zero. Runtime lowering authenticates and transfers the raw
// descriptor through the container ownership transaction.
func admitMutableFunctionGlobalSetSelf(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Imports) != 1 || len(m.Globals) != 0 || len(m.Exports) != 2 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	imp := m.Imports[0]
	if imp.Type.Kind != wasm.ExternGlobal {
		return false
	}
	typ := imp.Type.GlobalType()
	sig, ok := m.FuncSignature(0)
	if !ok || sig.Kind != wasm.CompFunc || len(sig.Params) != 0 || len(sig.Results) != 0 || m.Code[0].LocalDeclBytes != 1 || len(m.Code[0].Locals.Runs) != 0 || !bytes.Equal(m.Code[0].BodyBytes, []byte{0xd2, 0, 0x24, 0, 0x0b}) {
		return false
	}
	functionExport, globalExport := false, false
	for _, ex := range m.Exports {
		functionExport = functionExport || ex.Name == "store" && ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == 0
		globalExport = globalExport || ex.Name == "cell" && ex.Index.Kind == wasm.ExternGlobal && ex.Index.Index == 0
	}
	return typ.Mutable && wasm.EqualValType(typ.Type, wasm.FuncRef) && functionExport && globalExport
}

// admitMutableFunctionGlobalCopy is the first cross-container guest reference
// write boundary. The exact body copies imported mutable funcref global zero
// into imported mutable funcref global one. Runtime lowering authenticates the
// full-width descriptor and transfers its canonical owner root into the target.
func admitMutableFunctionGlobalCopy(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Imports) != 2 || len(m.Globals) != 0 || len(m.Exports) != 1 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	for i, name := range []string{"source", "target"} {
		imp := m.Imports[i]
		if imp.Module != "env" || imp.Name != name || imp.Type.Kind != wasm.ExternGlobal {
			return false
		}
		typ := imp.Type.GlobalType()
		if !typ.Mutable || !wasm.EqualValType(typ.Type, wasm.FuncRef) {
			return false
		}
	}
	sig, ok := m.FuncSignature(0)
	if !ok || sig.Kind != wasm.CompFunc || len(sig.Params) != 0 || len(sig.Results) != 0 || m.Code[0].LocalDeclBytes != 1 || len(m.Code[0].Locals.Runs) != 0 || !bytes.Equal(m.Code[0].BodyBytes, []byte{0x23, 0, 0x24, 1, 0x0b}) {
		return false
	}
	ex := m.Exports[0]
	return ex.Name == "copy" && ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == 0
}

// admitMutableFunctionGlobalMove admits the first exact multi-write reference
// body. It copies imported mutable funcref global zero into global one, then
// clears global zero. Each write still crosses the parked ownership bridge.
func admitMutableFunctionGlobalMove(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Imports) != 2 || len(m.Globals) != 0 || len(m.Exports) != 1 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	for i, name := range []string{"source", "target"} {
		imp := m.Imports[i]
		if imp.Module != "env" || imp.Name != name || imp.Type.Kind != wasm.ExternGlobal {
			return false
		}
		typ := imp.Type.GlobalType()
		if !typ.Mutable || !wasm.EqualValType(typ.Type, wasm.FuncRef) {
			return false
		}
	}
	sig, ok := m.FuncSignature(0)
	if !ok || sig.Kind != wasm.CompFunc || len(sig.Params) != 0 || len(sig.Results) != 0 || m.Code[0].LocalDeclBytes != 1 || len(m.Code[0].Locals.Runs) != 0 || !bytes.Equal(m.Code[0].BodyBytes, []byte{0x23, 0, 0x24, 1, 0xd0, 0x70, 0x24, 0, 0x0b}) {
		return false
	}
	ex := m.Exports[0]
	return ex.Name == "move" && ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == 0
}

// admitMutableFunctionGlobalBidirectionalCopy admits two independently safe
// cross-container writes. Each exported function copies one imported mutable
// funcref cell into the other; no guest temporary can outlive a container root.
// The indexed parked bridge is therefore exercised for both destination cells
// without opening arbitrary multi-write bodies.
func admitMutableFunctionGlobalBidirectionalCopy(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Imports) != 2 || len(m.Globals) != 0 || len(m.Exports) != 2 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	for i, name := range []string{"source", "target"} {
		imp := m.Imports[i]
		if imp.Module != "env" || imp.Name != name || imp.Type.Kind != wasm.ExternGlobal {
			return false
		}
		typ := imp.Type.GlobalType()
		if !typ.Mutable || !wasm.EqualValType(typ.Type, wasm.FuncRef) {
			return false
		}
	}
	wantBodies := [][]byte{{0x23, 0, 0x24, 1, 0x0b}, {0x23, 1, 0x24, 0, 0x0b}}
	for i := range m.Code {
		sig, ok := m.FuncSignature(uint32(i))
		if !ok || sig.Kind != wasm.CompFunc || len(sig.Params) != 0 || len(sig.Results) != 0 || m.Code[i].LocalDeclBytes != 1 || len(m.Code[i].Locals.Runs) != 0 || !bytes.Equal(m.Code[i].BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []string{"copy_to_target", "copy_to_source"}
	for i, ex := range m.Exports {
		if ex.Name != wantExports[i] || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}

// admitMutableFunctionGlobalLocalCopy is the first exact mixed-state reference
// product. One private mutable funcref cell is initialized with the sole local
// function and copied into one imported mutable cell through the parked owner
// transaction. The private cell cannot be exported or receive foreign values.
func admitMutableFunctionGlobalLocalCopy(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Imports) != 1 || len(m.Globals) != 1 || len(m.Exports) != 1 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	imp := m.Imports[0]
	if imp.Module != "env" || imp.Name != "target" || imp.Type.Kind != wasm.ExternGlobal {
		return false
	}
	importType := imp.Type.GlobalType()
	if !importType.Mutable || !wasm.EqualValType(importType.Type, wasm.FuncRef) {
		return false
	}
	local := m.Globals[0]
	if !local.Type.Mutable || !wasm.EqualValType(local.Type.Type, wasm.FuncRef) || len(local.Init.Instrs) != 0 || !bytes.Equal(local.Init.BodyBytes, []byte{0xd2, 0, 0x0b}) {
		return false
	}
	sig, ok := m.FuncSignature(0)
	if !ok || sig.Kind != wasm.CompFunc || len(sig.Params) != 0 || len(sig.Results) != 0 || m.Code[0].LocalDeclBytes != 1 || len(m.Code[0].Locals.Runs) != 0 || !bytes.Equal(m.Code[0].BodyBytes, []byte{0x23, 1, 0x24, 0, 0x0b}) {
		return false
	}
	ex := m.Exports[0]
	return ex.Name == "copy_local" && ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == 0
}

// admitMutableFunctionGlobalLocalPublish is the first exact mixed-state
// reference product whose private cell changes at runtime. The only values
// that can enter that cell are null and this instance's function zero, and the
// only escape copies the cell into imported mutable target zero through the
// parked owner transaction.
func admitMutableFunctionGlobalLocalPublish(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 4 || len(m.Code) != 4 || len(m.Imports) != 1 || len(m.Globals) != 1 || len(m.Exports) != 4 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	imp := m.Imports[0]
	if imp.Module != "env" || imp.Name != "target" || imp.Type.Kind != wasm.ExternGlobal {
		return false
	}
	importType := imp.Type.GlobalType()
	if !importType.Mutable || !wasm.EqualValType(importType.Type, wasm.FuncRef) {
		return false
	}
	local := m.Globals[0]
	if !local.Type.Mutable || !wasm.EqualValType(local.Type.Type, wasm.FuncRef) || len(local.Init.Instrs) != 0 || !bytes.Equal(local.Init.BodyBytes, []byte{0xd0, 0x70, 0x0b}) {
		return false
	}
	wantBodies := [][]byte{
		{0x0b},
		{0xd2, 0, 0x24, 1, 0x0b},
		{0xd0, 0x70, 0x24, 1, 0x0b},
		{0x23, 1, 0x24, 0, 0x0b},
	}
	for i := range m.Code {
		sig, ok := m.FuncSignature(uint32(i))
		if !ok || sig.Kind != wasm.CompFunc || len(sig.Params) != 0 || len(sig.Results) != 0 || m.Code[i].LocalDeclBytes != 1 || len(m.Code[i].Locals.Runs) != 0 || !bytes.Equal(m.Code[i].BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []string{"target", "set_local", "clear_local", "publish"}
	for i, ex := range m.Exports {
		if ex.Name != wantExports[i] || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}

// privateIndexedFunctionGlobal admits the table-free typed cells used to hold
// same-instance recursive call_ref targets. Keep this separate from public
// funcref globals: the carrier is immutable, non-null, non-exact, initialized
// directly by ref.func, and its target must have the indexed function type
// named by the cell. No imported target or subtype conversion is accepted.
func privateIndexedFunctionGlobal(global wasm.Global, m *wasm.Module) (uint32, bool) {
	if m == nil || global.Type.Mutable || len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || global.Type.Type.Kind() != wasm.ValRef {
		return 0, false
	}
	ref := global.Type.Type.Ref()
	heap := ref.Heap()
	if ref.Nullable() || ref.Exact() || heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec || !internalFunctionReferenceWithTypes(global.Type.Type, true, internalReferenceTypes{decoded: m.Types}) {
		return 0, false
	}
	value, err := tableElementExpression(global.Init)
	if err != nil || value.Null || uint64(value.FuncIndex) >= uint64(len(m.Code)) {
		return 0, false
	}
	target := m.FuncTypes[value.FuncIndex]
	if target.Rec || target.Index != heap.Type().Index {
		return 0, false
	}
	return value.FuncIndex, true
}

// Public immutable cells use retained canonical descriptors. Imported cells
// require the retained-reference profile and frozen Runtime descriptor-owner roots.
func admitImmutableFunctionGlobal(global wasm.Global, m *wasm.Module, limit int) error {
	if global.Type.Mutable || !wasm.EqualValType(global.Type.Type, wasm.FuncRef) || len(m.Tables)+m.ImportedTableCount() > 1 {
		return unsupported("reference global requires immutable local funcref and retained references")
	}
	if len(global.Init.BodyBytes) > 0 && global.Init.BodyBytes[0] == 0x23 {
		if err := admitGlobalExpression(global.Init, m, wasm.FuncRef, limit); err != nil {
			return err
		}
		return nil
	}
	value, err := tableElementExpression(global.Init)
	if err != nil {
		return err
	}
	if !value.Null && uint64(value.FuncIndex) >= uint64(m.ImportedFuncCount()+len(m.Code)) {
		return unsupported("reference global function index")
	}
	return nil
}

// Export chains are validated before folding. Imported-function targets need
// same-store native binding validation at public instantiation.
func admitReferenceGlobalExport(m *wasm.Module, index uint32) error {
	imported := m.ImportedGlobalCount()
	// Each copy reduces the global index. The public profile has at most 256
	// globals; no recursion or allocation is needed to find the literal target.
	for remaining := 256; remaining > 0; remaining-- {
		if uint64(index) < uint64(imported) {
			typ, ok := m.GlobalTypeByIndex(index)
			if !ok || !admitImportedFunctionGlobal(m, typ) {
				return unsupported("reference global import requires immutable funcref and retained references")
			}
			return nil
		}
		if uint64(index) >= uint64(imported+len(m.Globals)) {
			return unsupported("reference global export index")
		}
		global := m.Globals[int(index)-imported]
		if err := admitImmutableFunctionGlobal(global, m, int(index)); err != nil {
			return err
		}
		if global.Init.BodyBytes[0] == 0x23 {
			r := wasm.ReaderFrom(global.Init.BodyBytes[1:])
			index, _ = r.U32()
			continue
		}
		return nil
	}
	return unsupported("reference global initializer chain bound")
}

func admitImportedFunctionGlobal(m *wasm.Module, typ wasm.GlobalType) bool {
	return wasm.EqualValType(typ.Type, wasm.FuncRef) && (!typ.Mutable && len(m.Tables)+m.ImportedTableCount() <= 1 || typ.Mutable && (admitStateOnlyMutableFunctionGlobalImport(m) || admitMutableFunctionGlobalGet(m) || admitMutableFunctionGlobalClear(m) || admitMutableFunctionGlobalSetSelf(m) || admitMutableFunctionGlobalCopy(m) || admitMutableFunctionGlobalMove(m) || admitMutableFunctionGlobalBidirectionalCopy(m) || admitMutableFunctionGlobalLocalCopy(m) || admitMutableFunctionGlobalLocalPublish(m)))
}
