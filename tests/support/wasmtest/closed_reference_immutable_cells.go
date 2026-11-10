package wasmtest

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ClosedReferenceImmutableCells dispatches through a full descriptor loaded from
// an own immutable cell, composing private carriers and literal expression tables.
func ClosedReferenceImmutableCells(vector, wide bool, start, target int) []byte {
	return closedReferenceLocalCells(vector, true, wide, start, 3, target, true, true, false, true, true, true)
}
func ClosedReferenceImmutableCellsImportedInitializer(vector, wide bool, start int) []byte {
	return closedReferenceLocalCells(vector, true, wide, start, 0, 0, true, true, false, true, true, true)
}

// ClosedReferenceImmutableCellsBare reads only own immutable null/numeric cells;
// no table, imports, typed operation, control witness or proper tail is needed.
func ClosedReferenceImmutableCellsBare(vector, null bool) []byte {
	types := [][]byte{FuncType(nil, []wasm.ValType{wasm.I32}), FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I32})}
	functions := [][]byte{{0}, {1}, {0}}
	codes := [][]byte{Code([]byte{0x23, 0, 0x10, 1, 0x0b}), Code([]byte{0x20, 0, 0xd1, 0x23, 0, 0xd1, 0x73, 0x04, 0x40, 0, 0x0b, 0x23, 0, 0xd1, 0x0b}), Code([]byte{0x41, 43, 0x0b})}
	if vector {
		types = append(types, FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
		functions = append(functions, []byte{2})
		codes = append(codes, Code([]byte{0x20, 0, 0x0b}))
	}
	init := []byte{0xd2, 2, 0x0b}
	if null {
		init = []byte{0xd0, 0x70, 0x0b}
	}
	return Module(Section(1, Vec(types...)), Section(3, Vec(functions...)), Section(6, Vec(GlobalEntry(wasm.FuncRef, false, init))), Section(7, Vec(ExportEntry("run", 0, 0))), Section(10, Vec(codes...)))
}
