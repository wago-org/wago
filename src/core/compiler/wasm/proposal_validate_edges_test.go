package wasm

import (
	"fmt"
	"testing"
)

func structType(fields []FieldType, meta TypeMetadata) RecType {
	return RecType{SubTypes: []SubType{{Final: true, Metadata: meta, Comp: CompType{Kind: CompStruct, Fields: fields}}}}
}
func arrayType(field FieldType) RecType {
	return RecType{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompArray, Array: field}}}}
}
func field(v ValType, mut Mut) FieldType { return NewFieldType(StorageVal(v), mut) }
func packedField(p PackType, mut Mut) FieldType {
	return NewFieldType(StoragePacked(p), mut)
}
func refToType(idx uint32, nullable bool) ValType {
	return RefVal(Ref(nullable, IndexedHeap(TypeIdx{Index: idx}), false))
}
func descriptorModule(body ...Instruction) *Module {
	return &Module{
		Types: []RecType{
			{SubTypes: []SubType{
				{Final: true, Metadata: TypeMetadata{Descriptor: SomeTypeIdx(TypeIdx{Index: 1, Rec: true})}, Comp: CompType{Kind: CompStruct}},
				{Final: true, Metadata: TypeMetadata{Describes: SomeTypeIdx(TypeIdx{Index: 0, Rec: true})}, Comp: CompType{Kind: CompStruct}},
			}},
			ft(nil, nil),
		},
		FuncTypes: []TypeIdx{{Index: 2}},
		Code:      []Func{{Body: Expr{Instrs: body}}},
	}
}

func TestTypecheckDescriptorEqualityCast(t *testing.T) {
	for _, offset := range []uint32{0, 1} {
		for _, exact := range []bool{false, true} {
			for _, descriptorExact := range []bool{false, true} {
				for _, nullable := range []bool{false, true} {
					name := fmt.Sprintf("offset=%d/exact=%t/descriptorExact=%t/nullable=%t", offset, exact, descriptorExact, nullable)
					t.Run(name, func(t *testing.T) {
						target := IndexedHeap(TypeIdx{Index: offset})
						m := descriptorModule(
							Instruction{Kind: InstrLocalGet, Index: 0},
							Instruction{Kind: InstrLocalGet, Index: 1},
							Instruction{Kind: InstrRefCastDescEq, Cast: CastOp{SourceNullable: exact, TargetNullable: nullable}, ext: &instrExt{HeapType: target}},
						)
						if offset != 0 {
							// Keep the descriptor's recursive index at 1 while its flat index is 2.
							m.Types = append([]RecType{ft(nil, nil)}, m.Types...)
							m.FuncTypes[0].Index += offset
						}
						descriptor := RefVal(Ref(nullable, IndexedHeap(TypeIdx{Index: offset + 1}), descriptorExact))
						result := RefVal(Ref(nullable, target, exact))
						m.Types[len(m.Types)-1] = ft([]ValType{AnyRef, descriptor}, []ValType{result})
						if exact && !descriptorExact {
							expectValidateErr(t, m, ErrTypeMismatch)
						} else if err := ValidateModule(m); err != nil {
							t.Fatalf("matching descriptor rejected: %v", err)
						}
					})
				}
			}
		}
	}
	t.Run("rejects targets without struct descriptors", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			target HeapType
			typ    RecType
		}{
			{"abstract", AbsHeap(HeapAny), structType(nil, TypeMetadata{})},
			{"array", IndexedHeap(TypeIdx{Index: 0}), arrayType(field(I32, Var))},
			{"function", IndexedHeap(TypeIdx{Index: 0}), ft(nil, nil)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := &Module{
					Types:     []RecType{tc.typ, ft(nil, nil)},
					FuncTypes: []TypeIdx{{Index: 1}},
					Code: []Func{{Body: Expr{Instrs: []Instruction{
						{Kind: InstrUnreachable},
						{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: tc.target}},
						{Kind: InstrDrop},
					}}}},
				}
				expectValidateErr(t, m, ErrTypeMismatch)
			})
		}
	})
	t.Run("unreachable operands remain polymorphic", func(t *testing.T) {
		m := descriptorModule(
			Instruction{Kind: InstrUnreachable},
			Instruction{Kind: InstrRefCastDescEq, Cast: CastOp{SourceNullable: true}, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 0})}},
			Instruction{Kind: InstrDrop},
		)
		if err := ValidateModule(m); err != nil {
			t.Fatalf("unreachable operands rejected: %v", err)
		}
		m.Code[0].Body.Instrs = []Instruction{
			{Kind: InstrUnreachable},
			{Kind: InstrI32Const},
			{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 0})}},
			{Kind: InstrDrop},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestValidateDescriptorEqualityCastBytes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		descriptor []byte
		exact      bool
		missing    bool
		valid      bool
	}{
		{"matching inexact", []byte{0x63, 0x01}, false, false, true},
		{"matching exact", []byte{0x63, 0x62, 0x01}, true, false, true},
		{"inexact for exact", []byte{0x63, 0x01}, true, false, false},
		{"unrelated descriptor", []byte{0x6e}, false, false, false},
		{"missing descriptor", []byte{0x6e}, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// One recursive group holds the reciprocal descriptor pair; flat type 2 is the function.
			types := []byte{0x02, 0x4e, 0x02, 0x4d, 0x01, 0x5f, 0x00, 0x4c, 0x00, 0x5f, 0x00}
			if tc.missing {
				types = []byte{0x02, 0x4e, 0x02, 0x5f, 0x00, 0x5f, 0x00}
			}
			types = append(types, 0x60, 0x02, 0x6e)
			types = append(types, tc.descriptor...)
			types = append(types, 0x00)
			body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xfb, 0x23}
			if tc.exact {
				body = append(body, 0x62)
			}
			body = append(body, 0x00, 0x1a, 0x0b)
			data := module(
				section(secType, types...),
				section(secFunction, 0x01, 0x02),
				section(secCode, append([]byte{0x01, byte(len(body))}, body...)...),
			)
			for _, validate := range []struct {
				name string
				fn   func([]byte, ValidationFeatures) error
			}{{"AST", decodeThenValidateWithFeatures}, {"byte-backed", byteBackedDecodeThenValidateWithFeatures}} {
				err := validate.fn(data, ValidationFeatures{})
				if tc.valid && err != nil || !tc.valid && !isValidationCode(err, ErrTypeMismatch) {
					t.Errorf("%s validation = %v, want valid=%t", validate.name, err, tc.valid)
				}
			}
		})
	}
}

func TestTypecheckNegativeDescriptorAndGC(t *testing.T) {
	t.Run("ref.get_desc rejects non-reference operand", func(t *testing.T) {
		expectValidateErr(t, descriptorModule(Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefGetDesc, Index: 0}, Instruction{Kind: InstrDrop}), ErrTypeMismatch)
	})
	t.Run("ref.get_desc rejects types without descriptors", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{structType(nil, TypeMetadata{}), ft([]ValType{refToType(0, false)}, nil)},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code: []Func{{
				Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrRefGetDesc, Index: 0}, {Kind: InstrDrop}}},
			}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("ref.test_desc rejects non-reference operand", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefTestDesc, ext: &instrExt{HeapType: AbsHeap(HeapEq)}}, Instruction{Kind: InstrDrop}), ErrTypeMismatch)
	})
	t.Run("ref.test_desc rejects incompatible hierarchy", func(t *testing.T) {
		m := modWithFunc([]ValType{RefVal(Ref(false, AbsHeap(HeapFunc), false))}, nil, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrRefTestDesc, ext: &instrExt{HeapType: AbsHeap(HeapI31)}}, Instruction{Kind: InstrDrop})
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("ref.cast_desc_eq rejects invalid target type index", func(t *testing.T) {
		m := modWithFunc([]ValType{AnyRef, AnyRef}, nil, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalGet, Index: 1}, Instruction{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 999})}}, Instruction{Kind: InstrDrop})
		expectValidateErr(t, m, ErrUnknownType)
	})
	t.Run("ref.cast_desc_eq rejects target without descriptor", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{structType(nil, TypeMetadata{}), ft([]ValType{AnyRef, AnyRef}, nil)},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code: []Func{{Body: Expr{Instrs: []Instruction{
				{Kind: InstrLocalGet, Index: 0},
				{Kind: InstrLocalGet, Index: 1},
				{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 0})}},
				{Kind: InstrDrop},
			}}}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("ref.cast_desc_eq rejects unrelated descriptor operand", func(t *testing.T) {
		m := descriptorModule(
			Instruction{Kind: InstrLocalGet, Index: 0},
			Instruction{Kind: InstrLocalGet, Index: 1},
			Instruction{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 0})}},
			Instruction{Kind: InstrDrop},
		)
		m.Types[1] = ft([]ValType{AnyRef, AnyRef}, nil)
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("ref.cast_desc_eq rejects inexact operand for exact descriptor", func(t *testing.T) {
		m := descriptorModule(
			Instruction{Kind: InstrLocalGet, Index: 0},
			Instruction{Kind: InstrLocalGet, Index: 1},
			Instruction{Kind: InstrRefCastDescEq, Cast: CastOp{SourceNullable: true}, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 0})}},
			Instruction{Kind: InstrDrop},
		)
		m.Types[1] = ft([]ValType{AnyRef, RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 1}), false))}, nil)
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("ref.cast_desc_eq accepts matching descriptor operand", func(t *testing.T) {
		m := descriptorModule(
			Instruction{Kind: InstrLocalGet, Index: 0},
			Instruction{Kind: InstrLocalGet, Index: 1},
			Instruction{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 0})}},
			Instruction{Kind: InstrDrop},
		)
		m.Types[1] = ft([]ValType{AnyRef, RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 1}), false))}, nil)
		if err := ValidateModule(m); err != nil {
			t.Fatalf("matching descriptor rejected: %v", err)
		}
	})
	t.Run("ref.cast rejects a disjoint reference hierarchy", func(t *testing.T) {
		m := modWithFunc([]ValType{FuncRef}, nil,
			Instruction{Kind: InstrLocalGet, Index: 0},
			Instruction{Kind: InstrRefCast, ext: &instrExt{HeapType: AbsHeap(HeapAny)}},
			Instruction{Kind: InstrDrop},
		)
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("struct.new rejects descriptor-bearing structs", func(t *testing.T) {
		expectValidateErr(t, descriptorModule(Instruction{Kind: InstrStructNew, Index: 0}, Instruction{Kind: InstrDrop}), ErrTypeMismatch)
	})
	t.Run("struct.new_default_desc rejects inexact descriptor operand", func(t *testing.T) {
		m := descriptorModule(Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrStructNewDefaultDesc, Index: 0}, Instruction{Kind: InstrDrop})
		m.Types[1] = ft([]ValType{refToType(1, false)}, nil)
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("struct and array field stack effects", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{structType([]FieldType{field(I32, Var), packedField(PackI8, Const)}, TypeMetadata{}), arrayType(field(I64, Var)), ft(nil, nil)},
			FuncTypes: []TypeIdx{{Index: 2}},
			Code: []Func{{
				Locals: Locals{Runs: []LocalRun{{Count: 1, Type: refToType(0, true)}, {Count: 1, Type: refToType(1, true)}}},
				Body: Expr{Instrs: []Instruction{
					{Kind: InstrLocalGet, Index: 0}, {Kind: InstrStructGet, Index: 0, Index2: 0}, {Kind: InstrDrop},
					{Kind: InstrLocalGet, Index: 0}, {Kind: InstrStructAtomicGetS, Index: 0, Index2: 1}, {Kind: InstrDrop},
					{Kind: InstrLocalGet, Index: 1}, {Kind: InstrI32Const}, {Kind: InstrArrayGet, Index: 1}, {Kind: InstrDrop},
				}},
			}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("plain field gets reject packed storage", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			typ  RecType
			body []Instruction
		}{
			{
				name: "struct.get",
				typ:  structType([]FieldType{packedField(PackI8, Var)}, TypeMetadata{}),
				body: []Instruction{{Kind: InstrStructNewDefault, Index: 0}, {Kind: InstrStructGet, Index: 0, Index2: 0}, {Kind: InstrDrop}},
			},
			{
				name: "struct.atomic.get",
				typ:  structType([]FieldType{packedField(PackI8, Var)}, TypeMetadata{}),
				body: []Instruction{{Kind: InstrStructNewDefault, Index: 0}, {Kind: InstrStructAtomicGet, Index: 0, Index2: 0}, {Kind: InstrDrop}},
			},
			{
				name: "array.get",
				typ:  arrayType(packedField(PackI8, Var)),
				body: []Instruction{{Kind: InstrI32Const}, {Kind: InstrArrayNewDefault, Index: 0}, {Kind: InstrI32Const}, {Kind: InstrArrayGet, Index: 0}, {Kind: InstrDrop}},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := &Module{Types: []RecType{tc.typ, ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: tc.body}}}}
				expectValidateErr(t, m, ErrTypeMismatch)
			})
		}
	})
	t.Run("packed field gets reject unpacked storage", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			typ  RecType
			body []Instruction
		}{
			{
				name: "struct.get_s",
				typ:  structType([]FieldType{field(I32, Var)}, TypeMetadata{}),
				body: []Instruction{{Kind: InstrStructNewDefault, Index: 0}, {Kind: InstrStructGetS, Index: 0, Index2: 0}, {Kind: InstrDrop}},
			},
			{
				name: "struct.get_u",
				typ:  structType([]FieldType{field(I32, Var)}, TypeMetadata{}),
				body: []Instruction{{Kind: InstrStructNewDefault, Index: 0}, {Kind: InstrStructGetU, Index: 0, Index2: 0}, {Kind: InstrDrop}},
			},
			{
				name: "struct.atomic.get_s",
				typ:  structType([]FieldType{field(I32, Var)}, TypeMetadata{}),
				body: []Instruction{{Kind: InstrStructNewDefault, Index: 0}, {Kind: InstrStructAtomicGetS, Index: 0, Index2: 0}, {Kind: InstrDrop}},
			},
			{
				name: "struct.atomic.get_u",
				typ:  structType([]FieldType{field(I32, Var)}, TypeMetadata{}),
				body: []Instruction{{Kind: InstrStructNewDefault, Index: 0}, {Kind: InstrStructAtomicGetU, Index: 0, Index2: 0}, {Kind: InstrDrop}},
			},
			{
				name: "array.get_s",
				typ:  arrayType(field(I32, Var)),
				body: []Instruction{{Kind: InstrI32Const}, {Kind: InstrArrayNewDefault, Index: 0}, {Kind: InstrI32Const}, {Kind: InstrArrayGetS, Index: 0}, {Kind: InstrDrop}},
			},
			{
				name: "array.get_u",
				typ:  arrayType(field(I32, Var)),
				body: []Instruction{{Kind: InstrI32Const}, {Kind: InstrArrayNewDefault, Index: 0}, {Kind: InstrI32Const}, {Kind: InstrArrayGetU, Index: 0}, {Kind: InstrDrop}},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := &Module{Types: []RecType{tc.typ, ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: tc.body}}}}
				expectValidateErr(t, m, ErrTypeMismatch)
			})
		}
	})
}

func TestTypecheckNegativeAtomicAndMemory(t *testing.T) {
	shared := []MemType{{Shared: true, Limits: Limits{Min: 1, Max: 1, HasMax: true}}}
	t.Run("memory.atomic.notify stack effect and count type", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryAtomicNotify})
		m.Memories = shared
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		bad := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrMemoryAtomicNotify})
		bad.Memories = shared
		expectValidateErr(t, bad, ErrTypeMismatch)
	})
	t.Run("atomic waits reject operand positions", func(t *testing.T) {
		badTimeout := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryAtomicWait32})
		badTimeout.Memories = shared
		expectValidateErr(t, badTimeout, ErrTypeMismatch)
		badExpected := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrMemoryAtomicWait64})
		badExpected.Memories = shared
		expectValidateErr(t, badExpected, ErrTypeMismatch)
	})
	t.Run("atomic wait64 accepts natural alignment", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrMemoryAtomicWait64, ext: &instrExt{MemArg: MemArg{Align: 3}}})
		m.Memories = shared
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("rmw and cmpxchg reject wrong value types", func(t *testing.T) {
		badRMW := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrAtomicRmw, AtomicOp: 30})
		badRMW.Memories = shared
		expectValidateErr(t, badRMW, ErrTypeMismatch)
		badCmpxchg := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrAtomicCmpxchg, AtomicOp: 73})
		badCmpxchg.Memories = shared
		expectValidateErr(t, badCmpxchg, ErrTypeMismatch)
	})
	t.Run("atomic instructions accept unshared memories", func(t *testing.T) {
		// Atomic memory operators are proposal-valid only when their target memory
		// exists and their alignment is exactly natural. Wait traps at execution on
		// an unshared memory, while notify returns zero; both still validate.
		cases := []struct {
			name  string
			instr Instruction
			body  []Instruction
			drop  bool
		}{
			{"load", Instruction{Kind: InstrI32AtomicLoad, ext: &instrExt{MemArg: MemArg{Align: 2}}}, []Instruction{{Kind: InstrI32Const}}, true},
			{"store", Instruction{Kind: InstrI32AtomicStore, ext: &instrExt{MemArg: MemArg{Align: 2}}}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}, false},
			{"rmw", Instruction{Kind: InstrAtomicRmw, AtomicOp: 30, ext: &instrExt{MemArg: MemArg{Align: 2}}}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}, true},
			{"cmpxchg", Instruction{Kind: InstrAtomicCmpxchg, AtomicOp: 72, ext: &instrExt{MemArg: MemArg{Align: 2}}}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrI32Const}}, true},
			{"wait", Instruction{Kind: InstrMemoryAtomicWait32, ext: &instrExt{MemArg: MemArg{Align: 2}}}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrI64Const}}, true},
			{"notify", Instruction{Kind: InstrMemoryAtomicNotify, ext: &instrExt{MemArg: MemArg{Align: 2}}}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}, true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				body := append(append([]Instruction(nil), tc.body...), tc.instr)
				if tc.drop {
					body = append(body, Instruction{Kind: InstrDrop})
				}
				m := modWithFunc(nil, nil, body...)
				m.Memories = []MemType{{Limits: Limits{Min: 1}}}
				if err := ValidateModule(m); err != nil {
					t.Fatalf("ValidateModule: %v", err)
				}
			})
		}
	})
	t.Run("atomic memarg indexes alignment offset", func(t *testing.T) {
		mi := MemIdx(1)
		badIndex := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32AtomicLoad, ext: &instrExt{MemArg: MemArg{Mem: &mi}}})
		badIndex.Memories = shared
		expectValidateErr(t, badIndex, ErrUnknownMemory)
		badAlign := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32AtomicLoad, ext: &instrExt{MemArg: MemArg{Align: 3}}})
		badAlign.Memories = shared
		expectValidateErr(t, badAlign, ErrInvalidAlignment)
		badOffset := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32AtomicStore, ext: &instrExt{MemArg: MemArg{Offset: 1 << 32}}})
		badOffset.Memories = shared
		expectValidateErr(t, badOffset, ErrInvalidAlignment)
	})
	t.Run("atomic.fence preserves stack", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{I32, I64}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrAtomicFence})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
}

func TestTypecheckSIMDStackShapes(t *testing.T) {
	t.Run("lane memory natural alignment", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{V128}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrV128Load64Lane, Lane: 1, ext: &instrExt{MemArg: MemArg{Align: 3}}})
		m.Memories = []MemType{{Limits: Limits{Min: 1}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("swizzle consumes two vectors", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{V128}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrI8x16Swizzle})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		bad := modWithFunc(nil, nil, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrI8x16Swizzle}, Instruction{Kind: InstrDrop})
		expectValidateErr(t, bad, ErrTypeMismatch)
	})
	t.Run("shifts consume vector and scalar count", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{V128}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI16x8Shl})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		bad := modWithFunc(nil, nil, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrI16x8Shl}, Instruction{Kind: InstrDrop})
		expectValidateErr(t, bad, ErrTypeMismatch)
	})
}

func TestTypecheckSIMDLaneBounds(t *testing.T) {
	cases := []struct {
		name  string
		instr Instruction
		body  []Instruction
	}{
		{"extract", Instruction{Kind: InstrI8x16ExtractLaneS, Lane: 16}, []Instruction{{Kind: InstrV128Const}}},
		{"replace", Instruction{Kind: InstrI16x8ReplaceLane, Lane: 8}, []Instruction{{Kind: InstrV128Const}, {Kind: InstrI32Const}}},
		{"load_lane", Instruction{Kind: InstrV128Load8Lane, Lane: 16}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrV128Const}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Lane immediates are raw bytes in the decoder; validation enforces each
			// vector shape's lane count before accepting unsupported SIMD forms.
			body := append(append([]Instruction(nil), tc.body...), tc.instr)
			m := modWithFunc(nil, nil, body...)
			m.Memories = []MemType{{Limits: Limits{Min: 1}}}
			expectValidateErr(t, m, ErrTypeMismatch)
		})
	}
}

func TestValidateThrowAndTryTableReachability(t *testing.T) {
	m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrThrow, Index: 0})
	m.Types = []RecType{ft([]ValType{I32}, nil), ft(nil, nil)}
	m.Tags = []TagType{{Type: TypeIdx{Index: 0}}}
	m.FuncTypes = []TypeIdx{{Index: 1}}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule throw: %v", err)
	}

	bad := modWithFunc(nil, nil, Instruction{Kind: InstrThrow, Index: 0})
	bad.Types = []RecType{ft([]ValType{I32}, nil), ft(nil, nil)}
	bad.Tags = []TagType{{Type: TypeIdx{Index: 0}}}
	bad.FuncTypes = []TypeIdx{{Index: 1}}
	expectValidateErr(t, bad, ErrTypeMismatch)

	unknown := modWithFunc(nil, nil, Instruction{Kind: InstrThrow, Index: 0})
	expectValidateErr(t, unknown, ErrUnknownTag)
}

func TestTypecheckNegativeControlTailAndCast(t *testing.T) {
	t.Run("return_call_indirect result mismatch and table type", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{ft([]ValType{I32}, []ValType{I64}), ft(nil, []ValType{I32})},
			Tables:    []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code:      []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrReturnCallIndirect, Index: 0, Index2: 0}}}}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
		badTable := &Module{
			Types:     []RecType{ft(nil, nil)},
			Tables:    []Table{{Type: TableType{Ref: AbsRef(HeapExtern), Limits: Limits{Min: 1}}}},
			FuncTypes: []TypeIdx{{Index: 0}},
			Code:      []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrCallIndirect, Index: 0, Index2: 0}}}}},
		}
		expectValidateErr(t, badTable, ErrTypeMismatch)
	})
	t.Run("call_ref and return_call_ref", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{ft([]ValType{I32}, []ValType{I64}), ft(nil, []ValType{I32}), ft([]ValType{refToType(0, false)}, nil)},
			FuncTypes: []TypeIdx{{Index: 2}},
			Code: []Func{{
				Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrLocalGet, Index: 0}, {Kind: InstrCallRef, Index: 0}, {Kind: InstrDrop}}},
			}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		bad := &Module{
			Types:     []RecType{ft(nil, []ValType{I64}), ft([]ValType{refToType(0, false)}, []ValType{I32})},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code: []Func{{
				Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrReturnCallRef, Index: 0}}},
			}},
		}
		expectValidateErr(t, bad, ErrTypeMismatch)
	})
	t.Run("try_table catch validation", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrBlock, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrTryTable, ext: &instrExt{Catches: []Catch{{Kind: CatchTag, Tag: 0, Label: 0}}, Body: Expr{Instrs: []Instruction{{Kind: InstrNop}}}}}}}}})
		m.Types = []RecType{ft([]ValType{I32}, nil), ft(nil, nil)}
		m.Tags = []TagType{{Type: TypeIdx{Index: 0}}}
		m.FuncTypes = []TypeIdx{{Index: 1}}
		expectValidateErr(t, m, ErrTypeMismatch)
		badLabel := modWithFunc(nil, nil, Instruction{Kind: InstrTryTable, ext: &instrExt{Catches: []Catch{{Kind: CatchTag, Tag: 0, Label: 1}}, Body: Expr{Instrs: []Instruction{{Kind: InstrNop}}}}})
		badLabel.Tags = []TagType{{Type: TypeIdx{Index: 0}}}
		expectValidateErr(t, badLabel, ErrUnknownLabel)
	})
	t.Run("br_on_cast label and hierarchy checks", func(t *testing.T) {
		noPayload := modWithFunc([]ValType{AnyRef}, nil, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrBlock, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}}}}})
		expectValidateErr(t, noPayload, ErrTypeMismatch)
		badHierarchy := modWithFunc([]ValType{AnyRef}, nil, Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I31Ref}, Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapFunc), HeapType2: AbsHeap(HeapI31)}}}}}}, Instruction{Kind: InstrDrop})
		expectValidateErr(t, badHierarchy, ErrTypeMismatch)
	})
}

func TestSIMDStackEffectValidation(t *testing.T) {
	t.Run("simd splat and arithmetic validate", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32x4Splat}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32x4Splat}, Instruction{Kind: InstrI32x4Add}, Instruction{Kind: InstrDrop})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("simd rejects scalar mismatch", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32x4Splat}, Instruction{Kind: InstrDrop}), ErrTypeMismatch)
	})
}

func TestEnvAndMatchPortedHelpers(t *testing.T) {
	t.Run("block type resolves second subtype in rec group", func(t *testing.T) {
		m := &Module{
			Types: []RecType{{SubTypes: []SubType{
				{Comp: CompType{Kind: CompStruct}},
				{Comp: CompType{Kind: CompFunc, Params: []ValType{I32}, Results: []ValType{I64}}},
			}}, ft(nil, nil)},
			FuncTypes: []TypeIdx{{Index: 2}},
			Code: []Func{{Body: Expr{Instrs: []Instruction{
				{Kind: InstrI32Const},
				{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 1}}, Body: Expr{Instrs: []Instruction{{Kind: InstrDrop}, {Kind: InstrI64Const}}}}},
				{Kind: InstrDrop},
			}}}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("indexed heap subtyping follows supers", func(t *testing.T) {
		m := modWithFunc([]ValType{refToType(1, false)}, nil, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrDrop})
		m.Types = []RecType{openStructType(nil), RecType{SubTypes: []SubType{{Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompStruct}}}}, ft([]ValType{refToType(1, false)}, nil)}
		m.FuncTypes = []TypeIdx{{Index: 2}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("descriptor compatibility accepts equal exact shape and rejects unequal", func(t *testing.T) {
		mv := &moduleValidator{m: &Module{Types: []RecType{structType(nil, TypeMetadata{}), structType(nil, TypeMetadata{}), structType([]FieldType{field(I32, Const)}, TypeMetadata{})}}}
		if !mv.descriptorCompatible(Ref(true, IndexedHeap(TypeIdx{Index: 0}), true), Ref(true, IndexedHeap(TypeIdx{Index: 1}), true)) {
			t.Fatal("expected exact empty struct shapes compatible")
		}
		if mv.descriptorCompatible(Ref(true, IndexedHeap(TypeIdx{Index: 0}), true), Ref(true, IndexedHeap(TypeIdx{Index: 2}), true)) {
			t.Fatal("expected unequal exact struct shapes incompatible")
		}
	})
}
