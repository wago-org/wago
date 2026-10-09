package wasmtest

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// ClosedReferenceState returns a module whose internal reference-valued calls
// operate on owned numeric globals, two mixed-width memories and bulk data.
// Only numeric functions are exported. Function ordinals 2 and 3 are internal.
func ClosedReferenceState(vector, tails, wideFirst bool) []byte {
	return closedReferenceState(vector, tails, wideFirst, 32)
}

// ClosedReferenceStateOffset varies the full memory64 active initializer address.
func ClosedReferenceStateOffset(vector bool, offset uint64) []byte {
	return closedReferenceState(vector, false, true, offset)
}

func closedReferenceState(vector, tails, wideFirst bool, wideOffset uint64) []byte {
	typ := wasm.F64
	load, store := []byte{0x2b}, []byte{0x39}
	zero := []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}
	if vector {
		typ = wasm.V128
		load = []byte{0xfd, 0}
		store = []byte{0xfd, 11}
		zero = append([]byte{0xfd, 12}, make([]byte, 16)...)
		zero = append(zero, 0x0b)
	}
	widths := []bool{wideFirst, !wideFirst}
	addrType := func(index int) wasm.ValType {
		if widths[index] {
			return wasm.I64
		}
		return wasm.I32
	}
	constant := func(index int, value int64) []byte {
		op := byte(0x41)
		if widths[index] {
			op = 0x42
		}
		return append([]byte{op}, SLEB64(value)...)
	}
	memarg := func(op []byte, index int) []byte {
		out := append([]byte(nil), op...)
		return append(out, 0x40, byte(index), 0)
	}
	var types, functions, codes, exports [][]byte
	add := func(params, results []wasm.ValType, body []byte, name string, local bool) {
		index := uint32(len(functions))
		functions = append(functions, ULEB(uint32(len(types))))
		types = append(types, FuncType(params, results))
		code := Code(body)
		if local {
			raw := append([]byte{1, 1, 0x70}, body...)
			code = append(ULEB(uint32(len(raw))), raw...)
		}
		codes = append(codes, code)
		if name != "" {
			exports = append(exports, ExportEntry(name, 0, index))
		}
	}
	add([]wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.I32}, []byte{0xd2, 1, 0x20, 0, 0x20, 1, 0x10, 2, 0xd1, 0x0b}, "run", false)
	add(nil, nil, []byte{0x0b}, "", false)
	params, results := []wasm.ValType{wasm.FuncRef, wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.FuncRef}
	call := byte(0x10)
	if tails {
		call = 0x12
	}
	add(params, results, []byte{0x20, 0, 0x20, 1, 0x20, 2, call, 3, 0x0b}, "", false)
	// Keep the reference as an operand across numeric side effects, then save
	// and reload it through a reference local after the last bulk operation.
	leaf := []byte{0x20, 0, 0x20, 1, 0x23, 0, 0x6a, 0x24, 1, 0x20, 2, 0x24, 2}
	for i := 0; i < 2; i++ {
		leaf = append(leaf, constant(i, 1)...)
		leaf = append(leaf, 0x40, byte(i), 0x1a)
		leaf = append(leaf, constant(i, 64)...)
		leaf = append(leaf, 0x23, 2)
		leaf = append(leaf, memarg(store, i)...)
	}
	leaf = append(leaf, constant(1, 48)...)
	leaf = append(leaf, 0x41, 0, 0x41, 4, 0xfc, 8, 2, 1)
	leaf = append(leaf, constant(0, 48)...)
	leaf = append(leaf, constant(1, 48)...)
	leaf = append(leaf, 0x41, 4, 0xfc, 10, 0, 1)
	leaf = append(leaf, 0x21, 3, 0x23, 2, 0x23, 1, 0x20, 3, 0x0b)
	add(params, results, leaf, "", true)
	for i := 0; i < 2; i++ {
		loadBody := append([]byte{0x20, 0}, memarg(load, i)...)
		loadBody = append(loadBody, 0x0b)
		storeBody := append([]byte{0x20, 0, 0x20, 1}, memarg(store, i)...)
		storeBody = append(storeBody, 0x0b)
		add([]wasm.ValType{addrType(i)}, []wasm.ValType{typ}, loadBody, fmt.Sprintf("load%d", i), false)
		add([]wasm.ValType{addrType(i), typ}, nil, storeBody, fmt.Sprintf("store%d", i), false)
		add([]wasm.ValType{addrType(i)}, []wasm.ValType{addrType(i)}, []byte{0x20, 0, 0x40, byte(i), 0x0b}, fmt.Sprintf("grow%d", i), false)
		add(nil, []wasm.ValType{addrType(i)}, []byte{0x3f, byte(i), 0x0b}, fmt.Sprintf("size%d", i), false)
	}
	var memories, data [][]byte
	for i, wide := range widths {
		flag := byte(1)
		if wide {
			flag = 5
		}
		memories = append(memories, []byte{flag, 1, 3})
		offset := append(constant(i, int64(wideOffset)), 0x0b)
		if !wide {
			offset = []byte{0x23, 0, 0x41, 23, 0x6a, 0x0b}
		}
		entry := append([]byte{2, byte(i)}, offset...)
		entry = append(entry, 2, byte(0x21+i), byte(0x41+i))
		data = append(data, entry)
	}
	data = append(data, []byte{1, 4, 0x12, 0x34, 0x56, 0x78})
	globals := [][]byte{GlobalEntry(wasm.I32, false, []byte{0x41, 7, 0x41, 2, 0x6a, 0x0b}), GlobalEntry(wasm.I32, true, []byte{0x23, 0, 0x41, 2, 0x6a, 0x0b}), GlobalEntry(typ, true, zero)}
	return Module(Section(1, Vec(types...)), Section(3, Vec(functions...)), Section(5, Vec(memories...)), Section(6, Vec(globals...)), Section(7, Vec(exports...)), Section(9, []byte{1, 3, 0, 1, 1}), Section(12, []byte{3}), Section(10, Vec(codes...)), Section(11, Vec(data...)))
}
