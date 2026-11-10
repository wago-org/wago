package wasmtest

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ClosedReferenceExpressionElements combines literal active/passive/declarative
// forms with the qualified private proper indirect and direct imported tails.
func ClosedReferenceExpressionElements(vector, wide bool, start, target int) []byte {
	return closedReferenceLocalCells(vector, true, wide, start, 3, target, true, true, false, true, true)
}
func ClosedReferenceExpressionElementsImportedInitializer(vector, wide bool, start int) []byte {
	return closedReferenceLocalCells(vector, true, wide, start, 0, 0, true, true, false, true, true)
}

// ClosedReferenceDeclarativeExpressions has no table or globals. With drop,
// all256 original ordinals are retained as already-dropped empty state; without
// drop, no declaration survives and no unused value-pool type may be interned.
// Payload0 is empty,1 null,2 a numeric function. The private row stays uncallable.
func ClosedReferenceDeclarativeExpressions(vector, drop bool, payload int, trailingExpression ...bool) []byte {
	types := [][]byte{FuncType(nil, []wasm.ValType{wasm.I32}), FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I32})}
	functions := [][]byte{{0}, {1}, {0}}
	private := []byte{}
	if drop {
		private = []byte{0xfc, 13, 0xff, 1, 0xfc, 13, 0xff, 1}
		if len(trailingExpression) != 0 && trailingExpression[0] {
			private = []byte{0xfc, 13, 0xfe, 1, 0xfc, 13, 0xfe, 1}
		}
	}
	private = append(private, 0x41, 42, 0x0b)
	codes := [][]byte{Code([]byte{0xd0, 0x70, 0x10, 1, 0x0b}), Code(private), Code([]byte{0x41, 43, 0x0b})}
	if vector {
		types = append(types, FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
		functions = append(functions, []byte{2})
		codes = append(codes, Code([]byte{0x20, 0, 0x0b}))
	}
	elements := [][]byte{}
	if drop {
		for range 255 {
			if len(trailingExpression) != 0 && trailingExpression[0] {
				elements = append(elements, []byte{3, 0, 0})
			} else {
				elements = append(elements, []byte{7, 0x70, 0})
			}
		}
	}
	last := []byte{7, 0x70, 0}
	if payload == 1 {
		last = []byte{7, 0x70, 1, 0xd0, 0x70, 0x0b}
	}
	if payload == 2 {
		last = []byte{7, 0x70, 1, 0xd2, 2, 0x0b}
	}
	elements = append(elements, last)
	return Module(Section(1, Vec(types...)), Section(3, Vec(functions...)), Section(7, Vec(ExportEntry("run", 0, 0))), Section(9, Vec(elements...)), Section(10, Vec(codes...)))
}
