package wasmtest

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ClosedReferenceImportTails discards a private reference parameter, local and
// lower operand on a genuine numeric imported tail. The numeric root keeps its
// payload across the foreign tail and visibly continues after the private call.
// Imports0/1/2 are (i32)->i32; run args are i32,payload,target,null choice.
func ClosedReferenceImportTails(vector, large bool) []byte {
	return closedReferenceImportTails(vector, large, false, false, false)
}

// ClosedReferenceResourceImportTails records the secondary imported alias's
// size and newly grown page immediately after the foreign tail returns.
func ClosedReferenceResourceImportTails(vector, large, wide bool) []byte {
	return closedReferenceImportTails(vector, large, true, wide, false)
}

func ClosedReferenceRecursiveImportTail(vector, large bool) []byte {
	return closedReferenceImportTails(vector, large, false, false, true)
}

func closedReferenceImportTails(vector, large, resources, wide, recursive bool) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	types := [][]byte{FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), FuncType([]wasm.ValType{wasm.I32, typ, wasm.I32, wasm.I32}, []wasm.ValType{typ, wasm.I32, wasm.I32}), FuncType(nil, nil), FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32, typ, wasm.I32}, []wasm.ValType{wasm.I32})}
	var imports [][]byte
	for _, name := range []string{"native.fn", "shared.fn", "owned.fn"} {
		imports = append(imports, append(append(Name("env.calls"), Name(name)...), 0, 0))
	}
	marker := byte(0)
	if resources {
		marker = 2
		flag0, flag1 := byte(1), byte(5)
		if wide {
			flag0, flag1 = 5, 1
		}
		global := func(name string, typ wasm.ValType) []byte {
			return append(append(Name("env.resources"), Name(name)...), 3, wasm.MustEncodeValType(typ), 1)
		}
		memory := func(name string, flag byte) []byte {
			return append(append(Name("env.resources"), Name(name)...), 2, flag, 1, 3)
		}
		// Resource and function ordinals belong to separate import namespaces.
		imports = [][]byte{imports[0], global("counter", wasm.I32), memory("memory.0", flag0), imports[1], memory("memory.1", flag1), global("payload", typ), imports[2], memory("alias.1", flag1)}
	}
	root := []byte{0x20, 1, 0x20, 3, 4, 0x70, 0xd0, 0x70, 5, 0xd2, 4, 0x0b, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 5, 0x41}
	root = append(root, SLEB32(113)...)
	root = append(root, 0x22, 4, 0x24, marker, 0x20, 4)
	if resources {
		root = append(root, 0x3f, 2)
		if wide {
			root = append(root, 0xad)
		} // Secondary is memory32.
		root = append(root, 0x24, 3)
		address := byte(0x42)
		if wide {
			address = 0x41
		}
		root = append(root, address)
		root = append(root, SLEB32(65536+64)...)
		if vector {
			root = append(root, 0xfd, 0, 0x44, 2, 0)
		} else {
			root = append(root, 0x2b, 0x43, 2, 0)
		}
		root = append(root, 0x24, 4)
		// Update the exact borrowed cells after continuation; raw payload bits
		// remain visible independently in the owned resumed-payload global.
		root = append(root, 0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x23, 4, 0x24, 1)
	}
	root = append(root, 0x0b)
	rootCode := append([]byte{1, 1, 0x7f}, root...)
	rootCode = append(ULEB(uint32(len(rootCode))), rootCode...)
	locals := []byte{2, 1, 0x70, 1, wasm.MustEncodeValType(typ)}
	payloadLocal := byte(5)
	if large {
		locals = []byte{3, 1, 0x70, 64, 0x7e, 1, wasm.MustEncodeValType(typ)}
		payloadLocal = 69
	}
	leaf := []byte{0x20, 0, 0x22, 4, 0x20, 2, 0x21, payloadLocal, 0x20, 3, 0x45, 4, 0x40, 0x20, 1, 0x12, 0, 5, 0x20, 3, 0x41, 1, 0x46, 4, 0x40, 0x20, 1, 0x12, 1, 5, 0x20, 1, 0x12, 2, 0x0b, 0x0b, 0, 0x0b}
	if recursive {
		leaf = []byte{0x20, 0, 0x22, 4, 0x20, 2, 0x21, payloadLocal, 0x20, 1, 0x45, 4, 0x40, 0x41}
		leaf = append(leaf, SLEB32(-2147483647)...)
		leaf = append(leaf, 0x12, 0, 5, 0x20, 0, 0x20, 1, 0x41, 1, 0x6b, 0x20, 2, 0x20, 3, 0x12, 5, 0x0b, 0, 0x0b)
	}
	leafCode := append(locals, leaf...)
	leafCode = append(ULEB(uint32(len(leafCode))), leafCode...)
	globals := [][]byte{GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b})}
	exports := [][]byte{ExportEntry("run", 0, 3), ExportEntry("continued", 3, uint32(marker))}
	if resources {
		zero := []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}
		if vector {
			zero = append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
		}
		globals = append(globals, GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b}), GlobalEntry(typ, true, zero))
		exports = append(exports, ExportEntry("memory0", 2, 0), ExportEntry("memory1", 2, 1), ExportEntry("alias1", 2, 2), ExportEntry("counter", 3, 0), ExportEntry("payload", 3, 1), ExportEntry("resumed-size", 3, 3), ExportEntry("resumed-payload", 3, 4))
	}
	return Module(Section(1, Vec(types...)), Section(2, Vec(imports...)), Section(3, []byte{3, 1, 2, 3}), Section(6, Vec(globals...)), Section(7, Vec(exports...)), Section(9, []byte{1, 3, 0, 1, 4}), Section(10, Vec(rootCode, Code([]byte{0x0b}), leafCode)))
}

// ClosedReferenceVectorImportTail has a native-only pair transport target;
// its private reference parameter and lower operand are discarded at the tail.
func ClosedReferenceVectorImportTail() []byte {
	types := [][]byte{FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), FuncType([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128, wasm.I32}), FuncType(nil, nil), FuncType([]wasm.ValType{wasm.FuncRef, wasm.V128}, []wasm.ValType{wasm.V128})}
	root := []byte{0x20, 1, 4, 0x70, 0xd0, 0x70, 5, 0xd2, 2, 0x0b, 0x20, 0, 0x10, 3, 0x41}
	root = append(root, SLEB32(113)...)
	root = append(root, 0x0b)
	leaf := []byte{0x20, 0, 0x20, 1, 0x12, 0, 0x0b}
	return Module(Section(1, Vec(types...)), Section(2, Vec(append(append(Name("env.calls"), Name("native.fn")...), 0, 0))), Section(3, []byte{3, 1, 2, 3}), Section(7, Vec(ExportEntry("run", 0, 1))), Section(9, []byte{1, 3, 0, 1, 2}), Section(10, Vec(Code(root), Code([]byte{0x0b}), Code(leaf))))
}
