package wasm3

import "testing"

func structType(fields []FieldType, meta TypeMetadata) RecType {
	return RecType{SubTypes: []SubType{{Final: true, Metadata: meta, Comp: CompType{Kind: CompStruct, Fields: fields}}}}
}
func arrayType(field FieldType) RecType {
	return RecType{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompArray, Array: field}}}}
}
func field(v ValType, mut Mut) FieldType { return FieldType{Storage: StorageType{Val: v}, Mut: mut} }
func packedField(p PackType, mut Mut) FieldType {
	return FieldType{Storage: StorageType{Packed: true, Pack: p}, Mut: mut}
}
func refToType(idx uint32, nullable bool) ValType {
	return RefVal(Ref(nullable, IndexedHeap(TypeIdx{Index: idx}), false))
}
func exactRefToType(idx uint32, nullable bool) ValType {
	return RefVal(Ref(nullable, IndexedHeap(TypeIdx{Index: idx}), true))
}

func descriptorModule(body ...Instruction) *Module {
	return &Module{
		Types: []RecType{
			structType(nil, TypeMetadata{Descriptor: ptr(TypeIdx{Index: 1})}),
			structType(nil, TypeMetadata{Describes: ptr(TypeIdx{Index: 0})}),
			ft(nil, nil),
		},
		FuncTypes: []TypeIdx{{Index: 2}},
		Code:      []Func{{Body: Expr{Instrs: body}}},
	}
}

func TestTypecheckNegativeDescriptorAndGC(t *testing.T) {
	t.Run("ref.get_desc rejects non-reference operand", func(t *testing.T) {
		expectValidateErr(t, descriptorModule(Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefGetDesc, Index: 0}, Instruction{Kind: InstrDrop}), ErrTypeMismatch)
	})
	t.Run("ref.get_desc rejects types without descriptors", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{structType(nil, TypeMetadata{}), ft(nil, nil)},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code: []Func{{
				Locals: Locals{Runs: []LocalRun{{Count: 1, Type: refToType(0, false)}}},
				Body:   Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrRefGetDesc, Index: 0}, {Kind: InstrDrop}}},
			}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("ref.test_desc rejects non-reference operand", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefTestDesc, HeapType: AbsHeap(HeapEq)}, Instruction{Kind: InstrDrop}), ErrTypeMismatch)
	})
	t.Run("ref.test_desc rejects incompatible hierarchy", func(t *testing.T) {
		m := modWithFunc([]ValType{RefVal(Ref(false, AbsHeap(HeapFunc), false))}, nil, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrRefTestDesc, HeapType: AbsHeap(HeapI31)}, Instruction{Kind: InstrDrop})
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("ref.cast_desc_eq rejects invalid target type index", func(t *testing.T) {
		m := modWithFunc([]ValType{AnyRef, AnyRef}, nil, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalGet, Index: 1}, Instruction{Kind: InstrRefCastDescEq, HeapType: IndexedHeap(TypeIdx{Index: 999})}, Instruction{Kind: InstrDrop})
		expectValidateErr(t, m, ErrUnknownType)
	})
	t.Run("struct.new rejects descriptor-bearing structs", func(t *testing.T) {
		expectValidateErr(t, descriptorModule(Instruction{Kind: InstrStructNew, Index: 0}, Instruction{Kind: InstrDrop}), ErrTypeMismatch)
	})
	t.Run("struct.new_default_desc rejects inexact descriptor operand", func(t *testing.T) {
		m := descriptorModule(Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrStructNewDefaultDesc, Index: 0}, Instruction{Kind: InstrDrop})
		m.Code[0].Locals = Locals{Runs: []LocalRun{{Count: 1, Type: refToType(1, false)}}}
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
}

func TestTypecheckNegativeAtomicAndMemory(t *testing.T) {
	shared := []MemType{{Shared: true, Limits: Limits{Min: 1, Max: ptr(uint64(1))}}}
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
	t.Run("rmw and cmpxchg reject wrong value types", func(t *testing.T) {
		badRMW := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrAtomicRmw, AtomicOp: 30})
		badRMW.Memories = shared
		expectValidateErr(t, badRMW, ErrTypeMismatch)
		badCmpxchg := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrAtomicCmpxchg, AtomicOp: 73})
		badCmpxchg.Memories = shared
		expectValidateErr(t, badCmpxchg, ErrTypeMismatch)
	})
	t.Run("atomic instructions require shared memories", func(t *testing.T) {
		// Atomic memory operators are proposal-valid only when their target memory
		// is shared; stack shapes here are otherwise valid for the checked opcodes.
		cases := []struct {
			name  string
			instr Instruction
			body  []Instruction
		}{
			{"load", Instruction{Kind: InstrI32AtomicLoad}, []Instruction{{Kind: InstrI32Const}}},
			{"store", Instruction{Kind: InstrI32AtomicStore}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}},
			{"rmw", Instruction{Kind: InstrAtomicRmw, AtomicOp: 30}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}},
			{"cmpxchg", Instruction{Kind: InstrAtomicCmpxchg, AtomicOp: 72}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrI32Const}}},
			{"wait", Instruction{Kind: InstrMemoryAtomicWait32}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrI64Const}}},
			{"notify", Instruction{Kind: InstrMemoryAtomicNotify}, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				body := append(append([]Instruction(nil), tc.body...), tc.instr)
				m := modWithFunc(nil, nil, body...)
				m.Memories = []MemType{{Limits: Limits{Min: 1}}}
				expectValidateErr(t, m, ErrInvalidSharedMemory)
			})
		}
	})
	t.Run("atomic memarg indexes alignment offset", func(t *testing.T) {
		mi := MemIdx(1)
		badIndex := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32AtomicLoad, MemArg: MemArg{Mem: &mi}})
		badIndex.Memories = shared
		expectValidateErr(t, badIndex, ErrUnknownMemory)
		badAlign := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32AtomicLoad, MemArg: MemArg{Align: 3}})
		badAlign.Memories = shared
		expectValidateErr(t, badAlign, ErrInvalidAlignment)
		badOffset := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32AtomicStore, MemArg: MemArg{Offset: 1 << 32}})
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
			Types:     []RecType{ft([]ValType{I32}, []ValType{I64}), ft(nil, []ValType{I32}), ft(nil, nil)},
			FuncTypes: []TypeIdx{{Index: 2}},
			Code: []Func{{
				Locals: Locals{Runs: []LocalRun{{Count: 1, Type: refToType(0, false)}}},
				Body:   Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrLocalGet, Index: 0}, {Kind: InstrCallRef, Index: 0}, {Kind: InstrDrop}}},
			}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		bad := &Module{
			Types:     []RecType{ft(nil, []ValType{I64}), ft(nil, []ValType{I32})},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code: []Func{{
				Locals: Locals{Runs: []LocalRun{{Count: 1, Type: refToType(0, false)}}},
				Body:   Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrReturnCallRef, Index: 0}}},
			}},
		}
		expectValidateErr(t, bad, ErrTypeMismatch)
	})
	t.Run("try_table catch validation", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrBlock, Body: Expr{Instrs: []Instruction{{Kind: InstrTryTable, Catches: []Catch{{Kind: CatchTag, Tag: 0, Label: 0}}, Body: Expr{Instrs: []Instruction{{Kind: InstrNop}}}}}}})
		m.Types = []RecType{ft([]ValType{I32}, nil), ft(nil, nil)}
		m.Tags = []TagType{{Type: TypeIdx{Index: 0}}}
		m.FuncTypes = []TypeIdx{{Index: 1}}
		expectValidateErr(t, m, ErrTypeMismatch)
		badLabel := modWithFunc(nil, nil, Instruction{Kind: InstrTryTable, Catches: []Catch{{Kind: CatchTag, Tag: 0, Label: 1}}, Body: Expr{Instrs: []Instruction{{Kind: InstrNop}}}})
		badLabel.Tags = []TagType{{Type: TypeIdx{Index: 0}}}
		expectValidateErr(t, badLabel, ErrUnknownLabel)
	})
	t.Run("br_on_cast label and hierarchy checks", func(t *testing.T) {
		noPayload := modWithFunc([]ValType{AnyRef}, nil, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrBlock, Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}}})
		expectValidateErr(t, noPayload, ErrTypeMismatch)
		badHierarchy := modWithFunc([]ValType{AnyRef}, nil, Instruction{Kind: InstrBlock, BlockType: BlockType{Kind: BlockVal, Val: I31Ref}, Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, HeapType: AbsHeap(HeapFunc), HeapType2: AbsHeap(HeapI31)}}}}, Instruction{Kind: InstrDrop})
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
				{Kind: InstrBlock, BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 1}}, Body: Expr{Instrs: []Instruction{{Kind: InstrDrop}, {Kind: InstrI64Const}}}},
				{Kind: InstrDrop},
			}}}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("indexed heap subtyping follows supers", func(t *testing.T) {
		m := modWithFunc([]ValType{refToType(1, false)}, nil, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrDrop})
		m.Types = []RecType{structType(nil, TypeMetadata{}), RecType{SubTypes: []SubType{{Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompStruct}}}}, ft([]ValType{refToType(1, false)}, nil)}
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
