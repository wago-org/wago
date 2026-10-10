package wago

import (
	"bytes"
	"fmt"
	goruntime "runtime"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	nativeTable64NoABI uint8 = iota
	nativeTable64SizeABI
	nativeTable64GrowABI
	nativeTable64GetSetABI
	nativeTable64IndirectABI
	nativeTable64FillABI
	nativeTable64CopyABI
	nativeTable64InitDropABI
	nativeTable64ImportedSizeABI
	nativeTable64ImportedGetSetABI
	nativeTable64ImportedGrowABI
	nativeTable64ImportedFillABI
	nativeTable64ImportedCopyABI
	nativeTable64ImportedInitDropABI
)

func stagedNativeTable64ABI(m *wasm.Module) uint8 {
	if goruntime.GOOS != "linux" || goruntime.GOARCH != "riscv64" {
		return nativeTable64NoABI
	}
	if stagedOwnedTwoTable32SizeShape(m) {
		return nativeTable64SizeABI
	}
	if stagedOwnedTwoTable32GetSetShape(m) {
		return nativeTable64GetSetABI
	}
	if stagedOwnedTwoTable32GrowShape(m) {
		return nativeTable64GrowABI
	}
	if stagedOwnedTwoTable32FillShape(m) {
		return nativeTable64FillABI
	}
	if stagedOwnedTwoTable64SizeShape(m) {
		return nativeTable64SizeABI
	}
	if stagedOwnedTable64SizeShape(m) {
		return nativeTable64SizeABI
	}
	if stagedOwnedTable64GrowShape(m) {
		return nativeTable64GrowABI
	}
	if stagedOwnedTwoTable64GrowShape(m) {
		return nativeTable64GrowABI
	}
	if stagedOwnedTable64GetSetShape(m) {
		return nativeTable64GetSetABI
	}
	if stagedOwnedTwoTable64GetSetShape(m) {
		return nativeTable64GetSetABI
	}
	if stagedOwnedTwoTable64FillShape(m) {
		return nativeTable64FillABI
	}
	if stagedOwnedTwoTable64CopyShape(m) {
		return nativeTable64CopyABI
	}
	if stagedOwnedTwoTable64CrossCopyShape(m) {
		return nativeTable64CopyABI
	}
	if stagedOwnedTable64IndirectShape(m) {
		return nativeTable64IndirectABI
	}
	if stagedOwnedTwoTable64IndirectShape(m) {
		return nativeTable64IndirectABI
	}
	if stagedImportedTable64IndirectShape(m) {
		return nativeTable64IndirectABI
	}
	if stagedOwnedTable64FillShape(m) {
		return nativeTable64FillABI
	}
	if stagedOwnedTable64CopyShape(m) {
		return nativeTable64CopyABI
	}
	if stagedOwnedTable64InitDropShape(m) {
		return nativeTable64InitDropABI
	}
	if stagedOwnedTwoTable64InitDropShape(m) {
		return nativeTable64InitDropABI
	}
	if stagedImportedTable64SizeShape(m) {
		return nativeTable64ImportedSizeABI
	}
	if stagedImportedTable64GetSetShape(m) {
		return nativeTable64ImportedGetSetABI
	}
	if stagedImportedTable64GrowShape(m) {
		return nativeTable64ImportedGrowABI
	}
	if stagedImportedTable64FillShape(m) {
		return nativeTable64ImportedFillABI
	}
	if stagedImportedTable64CopyShape(m) {
		return nativeTable64ImportedCopyABI
	}
	if stagedImportedTable64InitDropShape(m) {
		return nativeTable64ImportedInitDropABI
	}
	return nativeTable64NoABI
}

// Keep table64 admission separate from ordinary compilation. This cold path
// carries many shape checks and error exits that otherwise enlarge its frame.
//
//go:noinline
func validateCompileTable64(m *wasm.Module) error {
	rv64Qualified := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64" && (stagedStateOnlyOwnedTable64DeclarationShape(m) || stagedOwnedTwoTable32SizeShape(m) || stagedOwnedTwoTable32GetSetShape(m) || stagedOwnedTwoTable32GrowShape(m) || stagedOwnedTwoTable32FillShape(m) || stagedOwnedTwoTable64SizeShape(m) || stagedOwnedTwoTable64GetSetShape(m) || stagedOwnedTwoTable64GrowShape(m) || stagedOwnedTwoTable64FillShape(m) || stagedOwnedTwoTable64CopyShape(m) || stagedOwnedTwoTable64CrossCopyShape(m) || stagedOwnedTwoTable64InitDropShape(m) || stagedOwnedTwoTable64IndirectShape(m) || stagedOwnedTable64SizeShape(m) || stagedOwnedTable64GrowShape(m) || stagedOwnedTable64GetSetShape(m) || stagedOwnedTable64IndirectShape(m) || stagedImportedTable64IndirectShape(m) || stagedOwnedTable64FillShape(m) || stagedOwnedTable64CopyShape(m) || stagedOwnedTable64InitDropShape(m) || stagedImportedTable64SizeShape(m) || stagedImportedTable64GetSetShape(m) || stagedImportedTable64GrowShape(m) || stagedImportedTable64FillShape(m) || stagedImportedTable64CopyShape(m) || stagedImportedTable64InitDropShape(m))
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) && !rv64Qualified {
		return fmt.Errorf("compile: unsupported table table64 staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	twoLocal := m.TableCount() == 2 && m.ImportedTableCount() == 0 && len(m.Tables) == 2
	twoLocalDeclaration := stagedTwoLocalNoMaxTable64DeclarationShape(m)
	importedLocalDeclaration := stagedImportedLocalNoMaxTable64DeclarationShape(m)
	threeLocalTableInit64 := m.TableCount() == 3 && m.ImportedTableCount() == 0 && len(m.Tables) == 3
	soleExternrefGrow := m.TableCount() == 1 && stagedSoleExternrefGrowShape(m)
	fourLocalExternrefSizeGrow := m.TableCount() == 4 && stagedFourLocalExternrefSizeGrowShape(m)
	if m.TableCount() != 1 && !twoLocal && !importedLocalDeclaration && !threeLocalTableInit64 && !fourLocalExternrefSizeGrow {
		return fmt.Errorf("compile: staged table64 requires exactly one local/imported table or an exact bounded multi-table slice")
	}
	if m.TableCount() == 1 && m.ImportedTableCount() != 0 && len(m.Tables) != 0 {
		return fmt.Errorf("compile: staged table64 rejects mixed imported/local table shapes")
	}
	if twoLocal && !twoLocalDeclaration {
		if err := stagedTwoLocalTableShape(m); err != nil {
			return fmt.Errorf("compile: staged table64 %w", err)
		}
	}
	if threeLocalTableInit64 {
		if err := stagedThreeLocalTableInit64Shape(m); err != nil {
			return fmt.Errorf("compile: staged table64 %w", err)
		}
	}
	if soleExternrefGrow || fourLocalExternrefSizeGrow {
		for i := range m.Tables {
			if m.Tables[i].Init != nil {
				return fmt.Errorf("compile: staged table64 table %d initializer expression is outside the exact local externref size/grow slice", i)
			}
		}
		if len(m.Elements) != 0 {
			return fmt.Errorf("compile: staged table64 element segments are outside the exact local externref size/grow slice")
		}
		allowed := func(k wasm.InstrKind) bool {
			if fourLocalExternrefSizeGrow {
				return k == wasm.InstrTableSize || k == wasm.InstrTableGrow
			}
			return k == wasm.InstrTableGet || k == wasm.InstrTableSet || k == wasm.InstrTableSize || k == wasm.InstrTableGrow
		}
		if err := stagedExactTableOperationShape(m, "exact local externref size/grow slice", allowed); err != nil {
			return fmt.Errorf("compile: staged table64 %w", err)
		}
	}
	externrefLocal := (twoLocal && (stagedTwoLocalExternrefReadWriteShape(m) || stagedTwoLocalExternrefFillShape(m))) || soleExternrefGrow || fourLocalExternrefSizeGrow
	inertOversized := stagedInertOversizedTable64Shape(m)
	for tableIndex := 0; tableIndex < m.TableCount(); tableIndex++ {
		tt, ok := m.TableType(uint32(tableIndex))
		if !ok {
			return fmt.Errorf("compile: staged table64 table %d type is unavailable", tableIndex)
		}
		if !wasm.EqualValType(wasm.RefVal(tt.Ref), wasm.FuncRef) && !(externrefLocal && wasm.EqualValType(wasm.RefVal(tt.Ref), wasm.ExternRef)) {
			return fmt.Errorf("compile: staged table64 requires funcref table %d outside an exact local externref slice", tableIndex)
		}
		if tt.Limits.Min > frontend.StagedTable64Max() || (tt.Limits.HasMax && tt.Limits.Max > frontend.StagedTable64Max() && !inertOversized) {
			return fmt.Errorf("compile: staged table64 table %d requires an executable runtime bound no greater than %d entries", tableIndex, frontend.StagedTable64Max())
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind == wasm.ElemActive && (int(e.Mode.Table) < 0 || int(e.Mode.Table) >= m.TableCount()) {
			return fmt.Errorf("compile: staged table64 active element segment targets unavailable table %d", e.Mode.Table)
		}
		if !twoLocal && !importedLocalDeclaration && !threeLocalTableInit64 && e.Mode.Kind == wasm.ElemActive && e.Mode.Table != 0 {
			return fmt.Errorf("compile: staged table64 active element segment targets table %d, want the sole table 0", e.Mode.Table)
		}
		if !twoLocal && !importedLocalDeclaration && !threeLocalTableInit64 && m.ImportedTableCount() != 0 && e.Mode.Kind != wasm.ElemActive && !(rv64Qualified && stagedImportedTable64InitDropShape(m)) {
			return fmt.Errorf("compile: imported table64 passive/declarative lifecycle remains outside the sole-local staged boundary")
		}
	}
	return nil
}

func stagedOwnedTwoTable32SizeShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		if table.Init != nil || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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

func stagedOwnedTwoTable32GetSetShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0xd0, 0x70, 0x26, byte(i), 0x20, 0, 0x25, byte(i), 0xd1, 0x0b}
		if table.Init != nil || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I32) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
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

func stagedOwnedTwoTable32GrowShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, byte(i), 0x0b}
		if table.Init != nil || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I32) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
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

func stagedOwnedTwoTable32FillShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, byte(i), 0x41, 1, 0x0b}
		if table.Init != nil || table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 2 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I32) || !wasm.EqualValType(sub.Comp.Params[1], wasm.I32) || len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) {
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

func stagedOwnedTwoTable64SizeShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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

func stagedOwnedTwoTable64GetSetShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0xd0, 0x70, 0x26, byte(i), 0x20, 0, 0x25, byte(i), 0xd1, 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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

func stagedOwnedTwoTable64GrowShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, byte(i), 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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

func stagedOwnedTwoTable64FillShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, byte(i), 0x41, 1, 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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

func stagedOwnedTwoTable64CopyShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, byte(i), byte(i), 0x41, 1, 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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

func stagedOwnedTwoTable64CrossCopyShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 2 || len(m.Elements) != 0 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, byte(i), byte(1 - i), 0x41, 1, 0x0b}
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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

func stagedOwnedTable64SizeShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0].Type
	if !table.Limits.Addr64 || !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || m.FuncTypes[0].Rec || m.FuncTypes[0].Index != 0 || len(m.Types[0].SubTypes) != 1 {
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

// stagedImportedTable64SizeShape is the first imported table64 slice. Keep the
// source contract exact while descriptor attachment, limit matching, retained
// ownership, and artifact admission cross the owned/imported runtime boundary.
func stagedImportedTable64SizeShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
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

func stagedImportedTable64GetSetShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
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

func stagedImportedTable64GrowShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
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

func stagedImportedTable64FillShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
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

func stagedImportedTable64CopyShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
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

func stagedImportedTable64InitDropShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Elements) != 1 || m.Start != nil {
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

func stagedOwnedTable64GrowShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
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

func stagedOwnedTable64GetSetShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
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

func stagedOwnedTable64IndirectShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 1 || len(m.Elements) != 1 || m.Tables[0].Init != nil || m.Start != nil {
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

func stagedOwnedTwoTable64IndirectShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Tables) != 2 || len(m.Elements) != 1 || m.Start != nil || len(m.Exports) != 4 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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

func stagedImportedTable64IndirectShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 1 || m.ImportedTableCount() != 1 || len(m.Tables) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Elements) != 0 || m.Start != nil {
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

func stagedOwnedTable64FillShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
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

func stagedOwnedTable64CopyShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
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

func stagedOwnedTable64InitDropShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 1 || len(m.Elements) != 1 || m.Tables[0].Init != nil || m.Start != nil {
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

func stagedOwnedTwoTable64InitDropShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Types) != 2 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Tables) != 2 || len(m.Elements) != 1 || m.Start != nil || len(m.Exports) != 5 {
		return false
	}
	if len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Tables {
		table := m.Tables[i]
		if table.Init != nil || !table.Type.Limits.Addr64 || !table.Type.Limits.HasMax || table.Type.Limits.Min > frontend.StagedTable64Max() || table.Type.Limits.Max > frontend.StagedTable64Max() || table.Type.Limits.Max < table.Type.Limits.Min || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.FuncRef) {
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

// stagedStateOnlyOwnedTable64DeclarationShape is the architecture-neutral
// compile gate for the bounded RV64 declaration profile. The backend and
// persisted-artifact admission independently repeat this exact witness.
func stagedStateOnlyOwnedTable64DeclarationShape(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Tables) != 1 || len(m.Elements) != 0 || m.Tables[0].Init != nil || m.Start != nil {
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
