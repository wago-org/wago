package wasmtest

import (
	"encoding/binary"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// ClosedReferenceLocalCells keeps every cell private. The collector stores the
// exact lower operand and saved local after a callback clears the live cells.
// Start0 is absent, start1 defined, start2 imported; imports are interleaved.
func ClosedReferenceLocalCells(vector, tails, wide bool, start int) []byte {
	return closedReferenceLocalCells(vector, tails, wide, start, 3, -1)
}

func ClosedReferenceLocalCellsImportedInitializer(vector, tails, wide bool, start int) []byte {
	return closedReferenceLocalCells(vector, tails, wide, start, 0, -1)
}

// ClosedReferenceNumericCalls preserves unrelated full-width cell carriers while
// calling a numeric target through an indexed private parameter. Target0 names
// an imported native wrapper, target1 an own identity, target2 a Go wrapper.
func ClosedReferenceNumericCalls(vector, tails, wide bool, start, target int) []byte {
	return closedReferenceLocalCells(vector, tails, wide, start, 3, target)
}
func ClosedReferenceNumericCallsImportedInitializer(vector, tails, wide bool, start int) []byte {
	return closedReferenceLocalCells(vector, tails, wide, start, 0, 0)
}

func closedReferenceLocalCells(vector, tails, wide bool, start int, initializer byte, target int) []byte {
	typ, typeByte := wasm.F64, byte(0x7c)
	if vector {
		typ, typeByte = wasm.V128, 0x7b
	}
	types := [][]byte{
		FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		FuncType(nil, nil),
		FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.FuncRef}),
		FuncType(nil, []wasm.ValType{typ, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
		FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32}, []wasm.ValType{wasm.I32}),
		FuncType([]wasm.ValType{wasm.FuncRef, wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}),
	}
	flag0, flag1 := byte(1), byte(5)
	if wide {
		flag0, flag1 = 5, 1
	}
	fn := func(name string, index byte) []byte {
		return append(append(Name("env.calls"), Name(name)...), 0, index)
	}
	mem := func(name string, flag byte) []byte {
		return append(append(Name("env.resources"), Name(name)...), 2, flag, 1, 3)
	}
	imports := [][]byte{mem("memory.0", flag0), fn("unused.fn", 0), GlobalImportEntry("env.resources", "counter", wasm.I32, true), fn("start.fn", 1), GlobalImportEntry("env.resources", "payload", typ, true), mem("memory.1", flag1), fn("finish.fn", 0), mem("alias.1", flag1)}
	null := []byte{0xd0, 0x70, 0x0b}
	zero := []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}
	if vector {
		zero = append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
	}
	globals := [][]byte{GlobalEntry(wasm.FuncRef, true, null), GlobalEntry(wasm.FuncRef, true, []byte{0xd2, initializer, 0x0b}), GlobalEntry(wasm.FuncRef, true, null), GlobalEntry(wasm.FuncRef, true, null), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(typ, true, zero), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b}), GlobalEntry(wasm.F32, true, []byte{0x43, 0, 0, 0, 0, 0x0b}), GlobalEntry(wasm.F64, true, []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b})}
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, 0x7ff0000000000042)
	literal := append([]byte{0x44}, payload...)
	if vector {
		payload = append(payload, make([]byte, 8)...)
		binary.LittleEndian.PutUint64(payload[8:], 0x80000000deadbeef)
		literal = append([]byte{0xfd, 12}, payload...)
	}
	run := []byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x23, 6, 0x41, 1, 0x6a, 0x24, 6, 0xd2, 0, 0x24, 2, 0x23, 3, 0x41, 0x7d}
	run = append(run, literal...)
	run = append(run, 0x10, 4, 0xd1, 0x24, 9, 0x24, 7, 0x24, 8, 0x23, 8, 0x24, 1, 0x23, 5, 0x41, 0x7d, 0x10, 8, 0x24, 7)
	addr := byte(0x41)
	if wide {
		addr = 0x42
	}
	run = append(run, addr, 49, 0x41, 42, 0x3a, 0, 0, 0x0b)
	leaf := []byte{3, 1, 0x70, 1, 0x7f, 1, typeByte, 0x20, 0, 0x21, 3, 0x23, 2, 0x20, 1, 0x10, 0, 0x20, 2, 0x10, 1, 0x21, 5, 0x21, 4, 0x20, 3, 0x10, 9, 0x21, 3, 0x23, 4, 0xd1, 0x24, 10, 0x20, 5, 0x20, 4, 0x20, 3, 0x0b}
	leaf = append(ULEB(uint32(len(leaf))), leaf...)
	call := byte(0x10)
	if tails {
		call = 0x12
	}
	tail := []byte{0x20, 1, call, 2, 0x0b}
	if tails {
		tail = []byte{0x20, 0, 0x20, 1, call, 2, 0x0b}
	}
	selectCell := []byte{0x20, 0, 0x45, 0x04, 0x70, 0xd2, 0, 0x05, 0x20, 0, 0x41, 1, 0x46, 0x04, 0x70, 0xd2, 3, 0x05, 0xd0, 0x70, 0x0b, 0x0b, 0x24, 3, 0x41, 0, 0x0b}
	exports := [][]byte{ExportEntry("initialize", 0, 3), ExportEntry("get", 0, 5), ExportEntry("clear", 0, 6), ExportEntry("run", 0, 7), ExportEntry("select", 0, 10), ExportEntry("count", 3, 6)}
	functionTypes := []byte{8, 1, 2, 3, 1, 1, 4, 5, 0}
	elements := []byte{1, 3, 0, 2, 0, 3}
	initialize := []byte{0x10, 7, 0x0b}
	var extra [][]byte
	if target >= 0 {
		types = append(types, []byte{0x60, 2, 0x63, 0, 0x7f, 1, 0x7f})
		globals = append(globals, GlobalEntry(wasm.FuncRef, true, null)) // target snapshot, index14
		functionTypes = []byte{10, 1, 2, 3, 1, 1, 4, 5, 0, 0, 6}
		index := byte(0)
		if target == 1 {
			index = 11
		} else if target == 2 {
			index = 2
		}
		leaf = []byte{3, 1, 0x70, 1, 0x7f, 1, typeByte, 0x20, 0, 0x21, 3, 0x23, 2, 0xd2, index, 0x20, 1, 0x10, 12,
			0x22, 4, 0xad, 0x24, 11, 0x20, 4, 0x20, 2, 0xd2, 1, 0x14, 1, 0x21, 5, 0x21, 4, 0x20, 3, 0x10, 9,
			0x21, 3, 0x23, 4, 0xd1, 0x24, 10, 0x20, 5, 0x20, 4, 0x20, 3, 0x0b}
		leaf = append(ULEB(uint32(len(leaf))), leaf...)
		initialize = []byte{0xd2, 7, 0x14, 1, 0x0b} // numeric wrapper also emits call_ref
		elements = []byte{1, 3, 0, 6, 0, 1, 2, 3, 7, 11}
		extra = [][]byte{Code([]byte{0x20, 0, 0x41, 2, 0x6a, 0x0b}), Code([]byte{0x20, 0, 0x24, 14, 0x20, 1, 0x20, 0, 0x14, 0, 0x0b})}
	}
	sections := [][]byte{Section(1, Vec(types...)), Section(2, Vec(imports...)), Section(3, functionTypes), Section(6, Vec(globals...)), Section(7, Vec(exports...))}
	if start != 0 {
		index := byte(3)
		if start == 2 {
			index = 1
		}
		sections = append(sections, Section(8, []byte{index}))
	}
	sections = append(sections, Section(9, elements), Section(10, Vec(append([][]byte{Code(initialize), leaf, Code([]byte{0x23, 8, 0x23, 7, 0x23, 9, 0x23, 10, 0x23, 2, 0xd1, 0x23, 3, 0xd1, 0x0b}), Code([]byte{0xd0, 0x70, 0x24, 2, 0xd0, 0x70, 0x24, 3, 0x0b}), Code(run), Code(tail), Code([]byte{0x20, 0, 0x24, 4, 0x20, 1, 0x24, 5, 0x20, 1, 0x0b}), Code(selectCell)}, extra...)...)), Section(11, append(append([]byte{1, 0, addr}, SLEB32(48)...), 0x0b, 1, 7)))
	return Module(sections...)
}
