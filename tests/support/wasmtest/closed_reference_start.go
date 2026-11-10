package wasmtest

import (
	"encoding/binary"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// ClosedReferenceDefinedStart has an unexported void start at absolute index3,
// private reference functions5/6/9, and only numeric resource exports by default.
// It observes initialized imported state, returns private reference results,
// then enters a numeric imported proper tail from a separate private frame.
// choice0/1/2 selects native/shared/owned targets; fault executes in private6.
func ClosedReferenceDefinedStart(vector, tails, wide bool, choice byte, probe bool, fault []byte) []byte {
	return closedReferenceDefinedStart(vector, tails, wide, choice, probe, fault, 48, 3)
}

// ClosedReferenceDefinedStartBadData fails initialization before start entry.
func ClosedReferenceDefinedStartBadData(vector, wide bool) []byte {
	return closedReferenceDefinedStart(vector, true, wide, 2, false, nil, 65536, 3)
}

// ClosedReferenceDefinedStartSelected exercises a nonzero or final void ordinal.
func ClosedReferenceDefinedStartSelected(vector bool, startIndex uint32) []byte {
	return closedReferenceDefinedStart(vector, true, true, 2, false, nil, 48, startIndex)
}

// ClosedReferenceImportedVoidStart isolates the selected ABI's imported-start
// veto from ordinary signature validation. The same module without start is valid.
func ClosedReferenceImportedVoidStart(start bool) []byte {
	sections := [][]byte{
		Section(1, Vec(FuncType(nil, nil), FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))),
		Section(2, Vec(append(append(Name("env"), Name("start")...), 0, 0))),
		Section(3, []byte{2, 0, 1}),
	}
	if start {
		sections = append(sections, Section(8, []byte{0}))
	}
	sections = append(sections, Section(10, Vec(Code([]byte{0xd0, 0x70, 0x10, 2, 0x1a, 0x0b}), Code([]byte{0x20, 0, 0x0b}))))
	return Module(sections...)
}

func closedReferenceDefinedStart(vector, tails, wide bool, choice byte, probe bool, fault []byte, dataOffset int32, startIndex uint32) []byte {
	typ := wasm.F64
	typeByte := byte(0x7c)
	if vector {
		typ = wasm.V128
		typeByte = 0x7b
	}
	types := [][]byte{
		FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		FuncType(nil, nil),
		FuncType(nil, []wasm.ValType{typ, wasm.I32, wasm.I32}),
		FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32, typ, wasm.I32}, []wasm.ValType{typ, wasm.I32, wasm.FuncRef}),
		FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	flag0, flag1 := byte(1), byte(5)
	if wide {
		flag0, flag1 = 5, 1
	}
	fn := func(name string) []byte { return append(append(Name("env.calls"), Name(name)...), 0, 0) }
	mem := func(name string, flag byte) []byte {
		return append(append(Name("env.resources"), Name(name)...), 2, flag, 1, 3)
	}
	imports := [][]byte{mem("memory.0", flag0), fn("native.fn"), GlobalImportEntry("env.resources", "counter", wasm.I32, true), GlobalImportEntry("env.resources", "payload", typ, true), fn("shared.fn"), mem("memory.1", flag1), fn("owned.fn"), mem("alias.1", flag1)}
	zero := []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}
	if vector {
		zero = append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
	}
	globals := [][]byte{GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(typ, true, zero), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b}), GlobalEntry(typ, true, zero), GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b})}
	globals = append(globals, GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))
	var exports [][]byte
	for _, ex := range []struct {
		name  string
		kind  byte
		index uint32
	}{{"memory0", 2, 0}, {"memory1", 2, 1}, {"alias1", 2, 2}, {"counter", 3, 0}, {"payload", 3, 1}, {"start-count", 3, 2}, {"start-result", 3, 3}, {"start-payload", 3, 4}, {"continued", 3, 5}, {"resumed-size", 3, 6}, {"resumed-payload", 3, 7}, {"null", 3, 8}, {"lower-null", 3, 9}} {
		exports = append(exports, ExportEntry(ex.name, ex.kind, ex.index))
	}
	if probe {
		exports = append(exports, ExportEntry("probe", 0, 4))
	}
	i32 := func(v int32) []byte { return append([]byte{0x41}, SLEB32(v)...) }
	address := func(secondary bool, value int32) []byte {
		op := byte(0x41)
		if wide != secondary {
			op = 0x42
		}
		return append([]byte{op}, SLEB32(value)...)
	}
	ref := []byte{0xd2, 7}
	if choice&1 != 0 {
		ref = []byte{0xd0, 0x70}
	}
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, 0x7ff0000000000042)
	literal := append([]byte{0x44}, payload...)
	if vector {
		payload = append(payload, make([]byte, 8)...)
		binary.LittleEndian.PutUint64(payload[8:], 0x80000000deadbeef)
		literal = append([]byte{0xfd, 12}, payload...)
	}
	start := []byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x23, 2, 0x41, 1, 0x6a, 0x24, 2}
	start = append(start, address(false, 49)...)
	start = append(start, 0x41, 42, 0x3a, 0, 0)
	start = append(start, ref...)
	start = append(start, i32(-3)...)
	start = append(start, literal...)
	start = append(start, i32(int32(choice))...)
	start = append(start, 0x10, 5, 0xd1, 0x24, 8, 0x24, 3, 0x24, 4)
	// This alias observation precedes the second import, so it cannot be
	// rescued by another callback's refresh after the first host resumes.
	start = append(start, 0x3f, 2)
	if wide {
		start = append(start, 0xad)
	}
	start = append(start, 0x24, 6)
	start = append(start, address(true, 65536+64)...)
	if vector {
		start = append(start, 0xfd, 0, 0x44, 2, 0)
	} else {
		start = append(start, 0x2b, 0x43, 2, 0)
	}
	start = append(start, 0x24, 7, 0x23, 7, 0x24, 1)
	call := byte(0x10)
	if tails {
		call = 0x12
	}
	start = append(start, call, 8, 0x0b)
	forward := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, call, 6, 0x0b}
	leaf := []byte{0x20, 0, 0x22, 4}
	leaf = append(leaf, fault...)
	leaf = append(leaf, 0x20, 2, 0x20, 3, 0x45, 4, 0x7f, 0x20, 1, 0x10, 0, 5, 0x20, 3, 0x41, 1, 0x46, 4, 0x7f, 0x20, 1, 0x10, 1, 5, 0x20, 1, 0x10, 2, 0x0b, 0x0b, 0x21, 5, 0x21, 6, 0xd1, 0x24, 9, 0x20, 6, 0x20, 5, 0x20, 4, 0x0f, 0x0b)
	leafCode := append([]byte{3, 1, 0x70, 1, 0x7f, 1, typeByte}, leaf...)
	leafCode = append(ULEB(uint32(len(leafCode))), leafCode...)
	continuation := append([]byte(nil), ref...)
	continuation = append(continuation, i32(-3)...)
	continuation = append(continuation, i32(int32(choice))...)
	continuation = append(continuation, 0x10, 9, 0x24, 3, 0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x41)
	continuation = append(continuation, SLEB32(113)...)
	continuation = append(continuation, 0x24, 5)
	continuation = append(continuation, address(false, 50)...)
	continuation = append(continuation, i32(99)...)
	continuation = append(continuation, 0x3a, 0, 0, 0x0b)
	numeric := []byte{0x20, 0, 0x20, 2, 0x45, 4, 0x40, 0x20, 1, 0x12, 0, 5, 0x20, 2, 0x41, 1, 0x46, 4, 0x40, 0x20, 1, 0x12, 1, 5, 0x20, 1, 0x12, 2, 0x0b, 0x0b, 0, 0x0b}
	offset := append(address(false, dataOffset), 0x0b)
	data := append(append([]byte{1, 0}, offset...), 1, 7)
	functions := []byte{7, 1, 5, 3, 3, 1, 1, 4}
	code := [][]byte{Code(start), Code([]byte{0x23, 2, 0x0b}), Code(forward), leafCode, Code([]byte{0x0b}), Code(continuation), Code(numeric)}
	if startIndex == 7 {
		code[4] = Code([]byte{0x41, 0xc9, 0, 0x24, 5, 0x0b})
	}
	if startIndex == 10 {
		functions[0]++
		functions = append(functions, 1)
		code = append(code, Code([]byte{0x41, 0xc9, 0, 0x24, 5, 0x0b}))
	}
	return Module(Section(1, Vec(types...)), Section(2, Vec(imports...)), Section(3, functions), Section(6, Vec(globals...)), Section(7, Vec(exports...)), Section(8, ULEB(startIndex)), Section(9, []byte{1, 3, 0, 1, 7}), Section(10, Vec(code...)), Section(11, data))
}
