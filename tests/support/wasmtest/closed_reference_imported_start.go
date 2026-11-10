package wasmtest

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// ClosedReferenceImportedStart uses function import ordinal1 for start, despite
// interleaved resources. initialize observes private saved/lower references across
// the numeric finish import. All externally callable signatures remain numeric.
func ClosedReferenceImportedStart(vector, wide bool) []byte {
	return closedReferenceImportedStart(vector, wide, 48)
}

func ClosedReferenceImportedStartBadData(vector, wide bool) []byte {
	return closedReferenceImportedStart(vector, wide, 65536)
}

func closedReferenceImportedStart(vector, wide bool, dataOffset int32) []byte {
	typ, typeByte := wasm.F64, byte(0x7c)
	if vector {
		typ, typeByte = wasm.V128, 0x7b
	}
	types := [][]byte{FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), FuncType(nil, nil), FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.FuncRef}), FuncType(nil, []wasm.ValType{typ, wasm.I32, wasm.I32, wasm.I32})}
	types = append(types, FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32}, []wasm.ValType{wasm.I32}))
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
	zero := []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}
	if vector {
		zero = append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
	}
	globals := [][]byte{GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(typ, true, zero), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b})}
	exports := [][]byte{ExportEntry("initialize", 0, 3), ExportEntry("get", 0, 6), ExportEntry("count", 3, 2), ExportEntry("result", 3, 3), ExportEntry("payload", 3, 4), ExportEntry("saved-null", 3, 5), ExportEntry("lower-null", 3, 6)}
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, 0x7ff0000000000042)
	literal := append([]byte{0x44}, payload...)
	if vector {
		payload = append(payload, make([]byte, 8)...)
		binary.LittleEndian.PutUint64(payload[8:], 0x80000000deadbeef)
		literal = append([]byte{0xfd, 12}, payload...)
	}
	initialize := []byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x23, 2, 0x41, 1, 0x6a, 0x24, 2, 0xd2, 5, 0x41, 0x7d}
	initialize = append(initialize, literal...)
	initialize = append(initialize, 0x10, 4, 0xd1, 0x24, 5, 0x24, 3, 0x24, 4, 0x23, 4, 0x24, 1)
	addr := byte(0x41)
	if wide {
		addr = 0x42
	}
	initialize = append(initialize, addr, 49, 0x41, 42, 0x3a, 0, 0, 0xd2, 5, 0x41, 0x7d, 0x10, 7, 0x24, 3, 0x0b)
	leaf := []byte{3, 1, 0x70, 1, 0x7f, 1, typeByte, 0x20, 0, 0x22, 3, 0x20, 2, 0x20, 1, 0x10, 2, 0x21, 4, 0x21, 5, 0xd1, 0x24, 6, 0x20, 5, 0x20, 4, 0x20, 3, 0x0f, 0x0b}
	leaf = append(ULEB(uint32(len(leaf))), leaf...)
	return Module(Section(1, Vec(types...)), Section(2, Vec(imports...)), Section(3, []byte{5, 1, 2, 1, 3, 4}), Section(6, Vec(globals...)), Section(7, Vec(exports...)), Section(8, []byte{1}), Section(9, []byte{1, 3, 0, 1, 5}), Section(10, Vec(Code(initialize), leaf, Code([]byte{0x0b}), Code([]byte{0x23, 4, 0x23, 3, 0x23, 5, 0x23, 6, 0x0b}), Code([]byte{0x20, 0, 0x20, 1, 0x12, 2, 0x0b}))), Section(11, append(append([]byte{1, 0, addr}, SLEB32(dataOffset)...), 0x0b, 1, 7)))
}
