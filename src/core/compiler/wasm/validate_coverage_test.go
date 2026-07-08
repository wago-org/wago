package wasm

import (
	"errors"
	"testing"
)

func constFor(t ValType) Instruction {
	switch {
	case equalValType(t, I64):
		return Instruction{Kind: InstrI64Const}
	case equalValType(t, F32):
		return Instruction{Kind: InstrF32Const}
	case equalValType(t, F64):
		return Instruction{Kind: InstrF64Const}
	case equalValType(t, V128):
		return Instruction{Kind: InstrV128Const}
	default:
		return Instruction{Kind: InstrI32Const}
	}
}

func TestValidatorCoverageCoreOpcodeFamilies(t *testing.T) {
	for k, vt := range unary {
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{vt}, constFor(vt), Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k, vt := range binaryOps {
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{vt}, constFor(vt), constFor(vt), Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k, vt := range compare {
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{I32}, constFor(vt), constFor(vt), Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k, vt := range test {
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{I32}, constFor(vt), Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k, eff := range conversions {
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{eff.to}, constFor(eff.from), Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k, eff := range loads {
		t.Run(k.String(), func(t *testing.T) {
			m := modWithFunc(nil, []ValType{eff.t}, Instruction{Kind: InstrI32Const}, Instruction{Kind: k})
			m.Memories = []MemType{{Limits: Limits{Min: 1}}}
			if err := ValidateModule(m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k, eff := range stores {
		t.Run(k.String(), func(t *testing.T) {
			m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, constFor(eff.t), Instruction{Kind: k})
			m.Memories = []MemType{{Limits: Limits{Min: 1}}}
			if err := ValidateModule(m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
}

func TestValidatorCoverageSIMDOpcodeFamilies(t *testing.T) {
	for k := range simdLoads {
		t.Run(k.String(), func(t *testing.T) {
			m := modWithFunc(nil, []ValType{V128}, Instruction{Kind: InstrI32Const}, Instruction{Kind: k})
			m.Memories = []MemType{{Limits: Limits{Min: 1}}}
			if err := ValidateModule(m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k := range simdMemLane {
		t.Run(k.String(), func(t *testing.T) {
			body := []Instruction{{Kind: InstrI32Const}, {Kind: InstrV128Const}, {Kind: k}}
			results := []ValType{V128}
			if k >= InstrV128Store8Lane && k <= InstrV128Store64Lane {
				results = nil
			}
			m := modWithFunc(nil, results, body...)
			m.Memories = []MemType{{Limits: Limits{Min: 1}}}
			if err := ValidateModule(m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k, scalar := range simdSplat {
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{V128}, constFor(scalar), Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k, scalar := range simdExtract {
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{scalar}, Instruction{Kind: InstrV128Const}, Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k, scalar := range simdReplace {
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{V128}, Instruction{Kind: InstrV128Const}, constFor(scalar), Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k := range simdShift {
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{V128}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k := range simdUnary {
		t.Run(k.String(), func(t *testing.T) {
			body := []Instruction{{Kind: InstrV128Const}, {Kind: k}}
			if k == InstrI8x16Swizzle {
				body = []Instruction{{Kind: InstrV128Const}, {Kind: InstrV128Const}, {Kind: k}}
			}
			if err := ValidateModule(modWithFunc(nil, []ValType{V128}, body...)); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	for k := range simdBinary {
		if _, isShift := simdShift[k]; isShift {
			continue
		}
		t.Run(k.String(), func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, []ValType{V128}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrV128Const}, Instruction{Kind: k})); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
}

func TestSIMDEffectTableMatchesAdmissionSet(t *testing.T) {
	for k := range simdAll {
		if simdEffects[k].cat == simdNone {
			t.Fatalf("%s is admitted by simdAll but has no validation effect", k)
		}
	}
	for k, eff := range simdEffects {
		if eff.cat == simdNone {
			continue
		}
		kind := InstrKind(k)
		if _, ok := simdAll[kind]; !ok {
			t.Fatalf("%s has validation effect but is missing from simdAll", kind)
		}
	}
}

func TestValidatorProposalCoverageArrayNewForms(t *testing.T) {
	arrayMod := func(kind InstrKind, body ...Instruction) *Module {
		m := &Module{
			Types:     []RecType{arrayType(field(I32, Var)), ft(nil, []ValType{refToType(0, false)})},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code:      []Func{{Body: Expr{Instrs: append(body, Instruction{Kind: kind, Index: 0})}}},
			DataCount: ptr(uint32(1)),
			Data:      []Data{{Mode: DataMode{Kind: DataPassive}}},
			Elements:  []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemTypedExprs, Ref: AbsRef(HeapFunc)}}},
		}
		return m
	}
	cases := []struct {
		name string
		kind InstrKind
		body []Instruction
	}{
		{"new", InstrArrayNew, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}},
		{"default", InstrArrayNewDefault, []Instruction{{Kind: InstrI32Const}}},
		{"fixed", InstrArrayNewFixed, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}},
		{"data", InstrArrayNewData, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}},
		{"elem", InstrArrayNewElem, []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := arrayMod(tc.kind, tc.body...)
			if tc.kind == InstrArrayNewFixed {
				m.Code[0].Body.Instrs[len(m.Code[0].Body.Instrs)-1].Index2 = 2
			}
			if err := ValidateModule(m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}

	t.Run("unknown array type", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrArrayNew, Index: 99}), ErrUnknownType)
	})
	t.Run("default rejects non-nullable reference element", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{arrayType(field(refToType(0, false), Var)), ft(nil, nil)},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code:      []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrArrayNewDefault, Index: 0}, {Kind: InstrDrop}}}}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("new_data unknown segment", func(t *testing.T) {
		m := arrayMod(InstrArrayNewData, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const})
		m.Code[0].Body.Instrs[2].Index2 = 1
		expectValidateErr(t, m, ErrInvalidDataCount)
	})
	t.Run("new_elem unknown segment", func(t *testing.T) {
		m := arrayMod(InstrArrayNewElem, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const})
		m.Code[0].Body.Instrs[2].Index2 = 1
		expectValidateErr(t, m, ErrUnknownTable)
	})
}

func TestValidatorProposalCoverageGCBranches(t *testing.T) {
	t.Run("ref and i31 conversions", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefI31}, Instruction{Kind: InstrI31GetU}, Instruction{Kind: InstrDrop},
			Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapExtern)}}, Instruction{Kind: InstrAnyConvertExtern}, Instruction{Kind: InstrExternConvertAny}, Instruction{Kind: InstrDrop},
		)
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("array len accepts array heap subtype", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{arrayType(field(I32, Var)), ft([]ValType{refToType(0, true)}, []ValType{I32})},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code:      []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrArrayLen}}}}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("array set and fill", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{arrayType(field(I64, Var)), ft([]ValType{refToType(0, true)}, nil)},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code: []Func{{Body: Expr{Instrs: []Instruction{
				{Kind: InstrLocalGet, Index: 0}, {Kind: InstrI32Const}, {Kind: InstrI64Const}, {Kind: InstrArraySet, Index: 0},
				{Kind: InstrLocalGet, Index: 0}, {Kind: InstrI32Const}, {Kind: InstrI64Const}, {Kind: InstrI32Const}, {Kind: InstrArrayFill, Index: 0},
			}}}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("array copy init data init elem", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{arrayType(field(I32, Var)), arrayType(field(I32, Var)), ft([]ValType{refToType(0, true), refToType(1, true)}, nil)},
			FuncTypes: []TypeIdx{{Index: 2}},
			Code: []Func{{Body: Expr{Instrs: []Instruction{
				{Kind: InstrLocalGet, Index: 0}, {Kind: InstrI32Const}, {Kind: InstrLocalGet, Index: 1}, {Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrArrayCopy, Index: 0, Index2: 1},
				{Kind: InstrLocalGet, Index: 0}, {Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrArrayInitData, Index: 0},
				{Kind: InstrLocalGet, Index: 0}, {Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrArrayInitElem, Index: 0},
			}}}},
			DataCount: ptr(uint32(1)),
			Data:      []Data{{Mode: DataMode{Kind: DataPassive}}},
			Elements:  []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemTypedExprs, Ref: AbsRef(HeapFunc)}}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("struct set requires mutable field", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{structType([]FieldType{field(I32, Var)}, TypeMetadata{}), ft([]ValType{refToType(0, true)}, nil)},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code:      []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrI32Const}, {Kind: InstrStructSet, Index: 0}}}}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		immutable := m
		immutable.Types[0] = structType([]FieldType{field(I32, Const)}, TypeMetadata{})
		expectValidateErr(t, immutable, ErrTypeMismatch)
	})
}

func TestValidatorCoverageStackEffectCategories(t *testing.T) {
	cases := []struct {
		name    string
		body    []Instruction
		results []ValType
	}{
		{"unary", []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Clz}}, []ValType{I32}},
		{"binary", []Instruction{{Kind: InstrI64Const}, {Kind: InstrI64Const}, {Kind: InstrI64Add}}, []ValType{I64}},
		{"compare", []Instruction{{Kind: InstrF32Const}, {Kind: InstrF32Const}, {Kind: InstrF32Eq}}, []ValType{I32}},
		{"test", []Instruction{{Kind: InstrI64Const}, {Kind: InstrI64Eqz}}, []ValType{I32}},
		{"conversion", []Instruction{{Kind: InstrF64Const}, {Kind: InstrI32TruncF64S}}, []ValType{I32}},
		{"i32.extend8_s", []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Extend8S}}, []ValType{I32}},
		{"i32.extend16_s", []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Extend16S}}, []ValType{I32}},
		{"i64.extend8_s", []Instruction{{Kind: InstrI64Const}, {Kind: InstrI64Extend8S}}, []ValType{I64}},
		{"i64.extend16_s", []Instruction{{Kind: InstrI64Const}, {Kind: InstrI64Extend16S}}, []ValType{I64}},
		{"i64.extend32_s", []Instruction{{Kind: InstrI64Const}, {Kind: InstrI64Extend32S}}, []ValType{I64}},
		{"truncsat-f32-i32", []Instruction{{Kind: InstrF32Const}, {Kind: InstrI32TruncSatF32S}}, []ValType{I32}},
		{"truncsat-f64-i32", []Instruction{{Kind: InstrF64Const}, {Kind: InstrI32TruncSatF64S}}, []ValType{I32}},
		{"truncsat-f32-i64", []Instruction{{Kind: InstrF32Const}, {Kind: InstrI64TruncSatF32S}}, []ValType{I64}},
		{"truncsat-f64-i64", []Instruction{{Kind: InstrF64Const}, {Kind: InstrI64TruncSatF64S}}, []ValType{I64}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateModule(modWithFunc(nil, tc.results, tc.body...)); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
	t.Run("unsupported opcode", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrMemorySize}), ErrUnknownMemory)
	})
}

func TestValidatorCoverageAtomicsAndSIMDForms(t *testing.T) {
	shared := []MemType{{Shared: true, Limits: Limits{Min: 1, Max: ptr(uint64(1))}}}
	t.Run("atomic load store rmw cmpxchg effects", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64AtomicLoad32U}, Instruction{Kind: InstrDrop},
			Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI64AtomicStore32},
			Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrAtomicRmw, AtomicOp: 76}, Instruction{Kind: InstrDrop},
			Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrAtomicCmpxchg, AtomicOp: 76}, Instruction{Kind: InstrDrop},
		)
		m.Memories = shared
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("simd memory store replace predicates bitselect const", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrV128Store},
			Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI8x16ReplaceLane}, Instruction{Kind: InstrDrop},
			Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrI8x16AllTrue}, Instruction{Kind: InstrDrop},
			Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrV128Bitselect}, Instruction{Kind: InstrDrop},
			Instruction{Kind: InstrV128Const}, Instruction{Kind: InstrDrop},
		)
		m.Memories = []MemType{{Limits: Limits{Min: 1}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
}

func TestValidatorCoverageHelpers(t *testing.T) {
	mv := &moduleValidator{m: &Module{Types: []RecType{
		structType(nil, TypeMetadata{}),
		RecType{SubTypes: []SubType{{Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompStruct}}}},
		arrayType(field(I32, Var)),
		ft(nil, nil),
	}}}
	if valTypeDefaultable(refToType(0, false)) {
		t.Fatal("non-null reference should not be defaultable")
	}
	if !valTypeDefaultable(refToType(0, true)) {
		t.Fatal("nullable reference should be defaultable")
	}
	if !mv.typeIdxSuperSubtype(TypeIdx{Index: 1}, TypeIdx{Index: 0}) {
		t.Fatal("expected explicit supertype relation")
	}
	if mv.typeIdxSuperSubtype(TypeIdx{Rec: true}, TypeIdx{Index: 0}) {
		t.Fatal("recursive local type indexes are not valid module indexes")
	}
	if !mv.heapSubtype(IndexedHeap(TypeIdx{Index: 2}), AbsHeap(HeapArray)) {
		t.Fatal("array heap index should subtype array")
	}
	if mv.heapSubtype(IndexedHeap(TypeIdx{Index: 99}), AbsHeap(HeapArray)) {
		t.Fatal("unknown heap index should not subtype array")
	}
}

func TestValidatorCoverageModuleIndexesAndImports(t *testing.T) {
	t.Run("imported tag func type", func(t *testing.T) {
		m := &Module{
			Types:   []RecType{ft([]ValType{I32}, nil), ft(nil, nil)},
			Imports: []Import{{Type: ExternType{Kind: ExternTag, Tag: TagType{Type: TypeIdx{Index: 0}}}}},
		}
		mv := &moduleValidator{m: m}
		if ft, ok := mv.tagFuncType(0); !ok || len(ft.Params) != 1 {
			t.Fatalf("tagFuncType imported = %v, %v", ft, ok)
		}
		if _, ok := mv.tagFuncType(1); ok {
			t.Fatal("unexpected tagFuncType success")
		}
	})
	t.Run("imported table memory and global indexes", func(t *testing.T) {
		m := &Module{
			Imports: []Import{
				{Type: ExternType{Kind: ExternTable, Table: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}},
				{Type: ExternType{Kind: ExternMem, Mem: MemType{Limits: Limits{Min: 1}}}},
				{Type: ExternType{Kind: ExternGlobal, Global: GlobalType{Type: I64}}},
			},
		}
		mv := &moduleValidator{m: m}
		if _, ok := mv.tableType(0); !ok {
			t.Fatal("imported table not found")
		}
		if _, ok := mv.memoryType(0); !ok {
			t.Fatal("imported memory not found")
		}
		if gt, ok := mv.globalType(0); !ok || !equalValType(gt.Type, I64) {
			t.Fatalf("imported global = %v, %v", gt, ok)
		}
	})
}

func TestValidatorCoverageModuleLevelBranches(t *testing.T) {
	t.Run("top-level validation failures", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{{SubTypes: []SubType{{Supers: []TypeIdx{{Index: 9}}, Comp: CompType{Kind: CompStruct}}}}}}, ErrUnknownType)
		expectValidateErr(t, &Module{Types: []RecType{{SubTypes: []SubType{{Metadata: TypeMetadata{Describes: ptr(TypeIdx{Index: 9})}, Comp: CompType{Kind: CompStruct}}}}}}, ErrUnknownType)
		expectValidateErr(t, &Module{Types: []RecType{{SubTypes: []SubType{{Metadata: TypeMetadata{Descriptor: ptr(TypeIdx{Index: 9})}, Comp: CompType{Kind: CompStruct}}}}}}, ErrUnknownType)
		expectValidateErr(t, &Module{Types: []RecType{{SubTypes: []SubType{{Comp: CompType{Kind: CompTypeKind(99)}}}}}}, ErrUnknownType)
		expectValidateErr(t, &Module{Types: []RecType{{SubTypes: []SubType{{Comp: CompType{Kind: CompStruct, Fields: []FieldType{{Storage: StorageType{Packed: true, Pack: PackType(0xff)}}}}}}}}}, ErrUnknownType)
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, []ValType{{Kind: ValTypeKind(99)}})}}, ErrUnknownType)
		expectValidateErr(t, &Module{Tables: []Table{{Type: TableType{Ref: Ref(true, HeapType{Kind: HeapTypeKind(99)}, false)}}}}, ErrUnknownType)
		expectValidateErr(t, &Module{Tables: []Table{{Type: TableType{Ref: Ref(true, HeapType{Kind: HeapDefType}, false)}}}}, ErrUnknownType)
	})
	t.Run("extern imports all kinds validate", func(t *testing.T) {
		max := uint64(1)
		m := &Module{
			Types: []RecType{ft(nil, nil)},
			Imports: []Import{
				{Type: ExternType{Kind: ExternFunc, Type: TypeIdx{Index: 0}}},
				{Type: ExternType{Kind: ExternTable, Table: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}},
				{Type: ExternType{Kind: ExternMem, Mem: MemType{Limits: Limits{Min: 1, Max: &max}, Shared: true}}},
				{Type: ExternType{Kind: ExternGlobal, Global: GlobalType{Type: I32}}},
				{Type: ExternType{Kind: ExternTag, Tag: TagType{Type: TypeIdx{Index: 0}}}},
			},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("table init const expression", func(t *testing.T) {
		m := &Module{Tables: []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}, Init: &Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("data active and code without type", func(t *testing.T) {
		m := &Module{Memories: []MemType{{Limits: Limits{Min: 1}}}, Data: []Data{{Mode: DataMode{Kind: DataActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		expectValidateErr(t, &Module{Code: []Func{{}}}, ErrUnknownFunc)
	})
	t.Run("validExternIdx default false", func(t *testing.T) {
		mv := &moduleValidator{m: &Module{}}
		if mv.validExternIdx(ExternIdx{Kind: ExternKind(99)}) {
			t.Fatal("invalid extern kind accepted")
		}
	})
}

func TestValidatorCoverageElementPayloadBranches(t *testing.T) {
	t.Run("element funcs valid and invalid", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{ft(nil, nil)},
			FuncTypes: []TypeIdx{{Index: 0}},
			Code:      []Func{{}},
			Tables:    []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}},
			Elements:  []Elem{{Mode: ElemMode{Kind: ElemActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}, Kind: ElemKind{Kind: ElemFuncs, Funcs: []FuncIdx{0}}}},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		m.Elements[0].Kind.Funcs = []FuncIdx{99}
		expectValidateErr(t, m, ErrUnknownFunc)
	})
	t.Run("element func expressions and unknown kind", func(t *testing.T) {
		m := &Module{Elements: []Elem{{Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		expectValidateErr(t, &Module{Elements: []Elem{{Kind: ElemKind{Kind: ElemKindKind(99)}}}}, ErrTypeMismatch)
	})
	t.Run("typed element expr rejects bad expression", func(t *testing.T) {
		m := &Module{Elements: []Elem{{Kind: ElemKind{Kind: ElemTypedExprs, Ref: AbsRef(HeapFunc), Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestValidatorCoverageTryTableAndCastBranches(t *testing.T) {
	t.Run("try_table catch refs validate", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: RefVal(AbsRef(HeapExn))}, Body: Expr{Instrs: []Instruction{
				{Kind: InstrTryTable, ext: &instrExt{
					Catches:   []Catch{{Kind: CatchAllRef, Label: 0}},
					BlockType: BlockType{Kind: BlockVal, Val: RefVal(AbsRef(HeapExn))},
					Body:      Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapExn)}}}},
				}},
			}}}},
			Instruction{Kind: InstrDrop},
		)
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("try_table catch_all rejects payload label", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{
				{Kind: InstrTryTable, ext: &instrExt{Catches: []Catch{{Kind: CatchAll, Label: 0}}}},
			}}}},
			Instruction{Kind: InstrDrop},
		)
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("br_on_cast success and fail paths", func(t *testing.T) {
		success := modWithFunc([]ValType{AnyRef}, nil,
			Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: EqRef}, Body: Expr{Instrs: []Instruction{
				{Kind: InstrLocalGet, Index: 0},
				{Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}},
				{Kind: InstrDrop},
				{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapEq)}},
			}}}},
			Instruction{Kind: InstrDrop},
		)
		if err := ValidateModule(success); err != nil {
			t.Fatalf("ValidateModule br_on_cast: %v", err)
		}
		fail := modWithFunc([]ValType{AnyRef}, nil,
			Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: AnyRef}, Body: Expr{Instrs: []Instruction{
				{Kind: InstrLocalGet, Index: 0},
				{Kind: InstrBrOnCastFail, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}},
				{Kind: InstrDrop},
				{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapAny)}},
			}}}},
			Instruction{Kind: InstrDrop},
		)
		if err := ValidateModule(fail); err != nil {
			t.Fatalf("ValidateModule br_on_cast_fail: %v", err)
		}
	})
}

func TestValidatorCoverageSubtypeHelpers(t *testing.T) {
	for _, tc := range []struct {
		a, b AbsHeapType
		ok   bool
	}{
		{HeapNoFunc, HeapFunc, true},
		{HeapNoExtern, HeapExtern, true},
		{HeapNone, HeapStruct, true},
		{HeapI31, HeapEq, true},
		{HeapEq, HeapAny, true},
		{HeapFunc, HeapAny, false},
		{HeapString, HeapAny, false},
		{HeapExtern, HeapFunc, false},
	} {
		if got := absHeapSubtype(tc.a, tc.b); got != tc.ok {
			t.Fatalf("absHeapSubtype(%v,%v)=%v want %v", tc.a, tc.b, got, tc.ok)
		}
	}
	mv := &moduleValidator{m: &Module{Types: []RecType{
		structType(nil, TypeMetadata{}),
		structType(nil, TypeMetadata{}),
		arrayType(field(I32, Var)),
		ft(nil, nil),
	}}}
	if !mv.refSubtype(Ref(false, IndexedHeap(TypeIdx{Index: 3}), false), AbsRef(HeapFunc)) {
		t.Fatal("function type index should subtype func")
	}
	if mv.refSubtype(Ref(true, IndexedHeap(TypeIdx{Index: 0}), false), Ref(false, IndexedHeap(TypeIdx{Index: 0}), false)) {
		t.Fatal("nullable ref should not subtype non-null ref")
	}
	if !mv.descriptorCompatible(AbsRef(HeapEq), AbsRef(HeapAny)) {
		t.Fatal("abstract heaps should be descriptor-compatible when related")
	}
	if mv.descriptorCompatible(AbsRef(HeapFunc), AbsRef(HeapAny)) {
		t.Fatal("disjoint abstract heaps should not be descriptor-compatible")
	}
	if mv.descriptorCompatible(Ref(true, IndexedHeap(TypeIdx{Index: 99}), true), Ref(true, IndexedHeap(TypeIdx{Index: 0}), true)) {
		t.Fatal("unknown exact descriptor type should not be compatible")
	}
	if !mv.heapSubtype(AbsHeap(HeapI31), AbsHeap(HeapEq)) {
		t.Fatal("abstract heap subtype not recognized")
	}
}

func coverageFuncValidator(m *Module, results []ValType) *funcValidator {
	mv := &moduleValidator{m: m}
	fv := &funcValidator{moduleValidator: mv}
	_ = fv.pushCtrl(ctrlFunc, nil, results)
	return fv
}

func expectStepErr(t *testing.T, fv *funcValidator, in Instruction, code ValidationErrorCode) {
	t.Helper()
	err := fv.step(in)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != code {
		t.Fatalf("expected %v, got %v", code, err)
	}
}

func isValidationCode(err error, code ValidationErrorCode) bool {
	var ve *ValidationError
	return errors.As(err, &ve) && ve.Code == code
}

func coverageFuncValidatorWithStack(m *Module, stack ...ValType) *funcValidator {
	fv := coverageFuncValidator(m, nil)
	for _, vt := range stack {
		fv.push(vt)
	}
	return fv
}

func TestValidatorCoverageModuleLevelNegativeBranches(t *testing.T) {
	t.Run("table init expression type mismatch", func(t *testing.T) {
		m := &Module{Tables: []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}, Init: &Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapExtern)}}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("global has invalid value type", func(t *testing.T) {
		expectValidateErr(t, &Module{Globals: []Global{{Type: GlobalType{Type: ValType{Kind: ValTypeKind(99)}}}}}, ErrUnknownType)
	})
	t.Run("active data has unknown memory", func(t *testing.T) {
		m := &Module{Data: []Data{{Mode: DataMode{Kind: DataActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}}
		expectValidateErr(t, m, ErrUnknownMemory)
	})
	t.Run("import func and tag use non-function type", func(t *testing.T) {
		types := []RecType{structType(nil, TypeMetadata{})}
		expectValidateErr(t, &Module{Types: types, Imports: []Import{{Type: ExternType{Kind: ExternFunc, Type: TypeIdx{Index: 0}}}}}, ErrUnknownType)
		expectValidateErr(t, &Module{Types: types, Imports: []Import{{Type: ExternType{Kind: ExternTag, Tag: TagType{Type: TypeIdx{Index: 0}}}}}}, ErrUnknownType)
	})
	t.Run("function parameter has invalid value type", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{ft([]ValType{{Kind: ValTypeKind(99)}}, nil)}}, ErrUnknownType)
	})
	t.Run("active element has unknown table", func(t *testing.T) {
		m := &Module{Elements: []Elem{{Mode: ElemMode{Kind: ElemActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}}
		expectValidateErr(t, m, ErrUnknownTable)
	})
	t.Run("typed element expression mismatch", func(t *testing.T) {
		m := &Module{Elements: []Elem{{Kind: ElemKind{Kind: ElemTypedExprs, Ref: AbsRef(HeapExtern), Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("function element expression rejects non-funcref", func(t *testing.T) {
		m := &Module{Elements: []Elem{{Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestValidatorCoverageInternalHelperBranches(t *testing.T) {
	mv := &moduleValidator{m: &Module{
		Types: []RecType{
			structType(nil, TypeMetadata{}),
			RecType{SubTypes: []SubType{{Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompStruct}}}},
			arrayType(field(I32, Var)),
			ft(nil, nil),
		},
		Imports: []Import{
			{Type: ExternType{Kind: ExternGlobal, Global: GlobalType{Type: I32}}},
			{Type: ExternType{Kind: ExternTable, Table: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}},
			{Type: ExternType{Kind: ExternMem, Mem: MemType{Limits: Limits{Min: 1}}}},
		},
		Globals:  []Global{{Type: GlobalType{Type: I64}}},
		Tables:   []Table{{Type: TableType{Ref: AbsRef(HeapExtern), Limits: Limits{Min: 1}}}},
		Memories: []MemType{{Limits: Limits{Min: 1}}},
	}}
	if _, _, ok := mv.structFields(TypeIdx{Index: 2}); ok {
		t.Fatal("array type reported as struct")
	}
	if _, ok := mv.globalType(1); !ok {
		t.Fatal("local global index did not resolve")
	}
	if _, ok := mv.globalType(2); ok {
		t.Fatal("unknown global index resolved")
	}
	if _, ok := mv.tableType(1); !ok {
		t.Fatal("local table index did not resolve")
	}
	if _, ok := mv.memoryType(1); !ok {
		t.Fatal("local memory index did not resolve")
	}
	if !mv.descriptorCompatible(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false), Ref(false, IndexedHeap(TypeIdx{Index: 1}), false)) {
		t.Fatal("super/sub descriptor-compatible refs should match")
	}
	if mv.descriptorCompatible(AbsRef(HeapFunc), Ref(false, IndexedHeap(TypeIdx{Index: 0}), false)) {
		t.Fatal("unrelated descriptor refs should not match")
	}
	if mv.refSubtype(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false), Ref(false, IndexedHeap(TypeIdx{Index: 0}), true)) {
		t.Fatal("exact super target should reject inexact source")
	}
	if mv.refSubtype(Ref(false, AbsHeap(HeapExtern), false), Ref(false, IndexedHeap(TypeIdx{Index: 0}), false)) {
		t.Fatal("abstract heap should not subtype indexed heap")
	}
	if !coverageFuncValidator(mv.m, nil).subtype(ValType{Kind: ValBot}, I32) {
		t.Fatal("value bottom should subtype any value")
	}
	if _, _, err := coverageFuncValidator(&Module{}, nil).blockSig(BlockType{Kind: BlockVal, Val: ValType{Kind: ValTypeKind(99)}}); err == nil {
		t.Fatal("invalid block result type accepted")
	}
	if _, _, err := coverageFuncValidator(&Module{}, nil).blockSig(BlockType{Kind: BlockTypeKind(99)}); err == nil {
		t.Fatal("invalid block type accepted")
	}
	if mv.heapSubtype(AbsHeap(HeapExtern), AbsHeap(HeapFunc)) {
		t.Fatal("unrelated abstract heaps should not subtype")
	}
	if !mv.heapSubtype(AbsHeap(HeapExtern), AbsHeap(HeapExtern)) {
		t.Fatal("equal heaps should subtype")
	}
	if err := mv.validateHeapType(HeapType{Kind: HeapDefType, Def: &DefType{}}); err != nil {
		t.Fatalf("heap def type: %v", err)
	}
}

func TestValidatorCoverageCoreNegativeStepBranches(t *testing.T) {
	t.Run("const-only rejects non-const instruction", func(t *testing.T) {
		fv := coverageFuncValidator(&Module{}, nil)
		fv.constOnly = true
		expectStepErr(t, fv, Instruction{Kind: InstrNop}, ErrConstExprRequired)
	})
	t.Run("typed select validates immediate value type", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{{Kind: ValTypeKind(99)}}}}), ErrUnknownType)
	})
	t.Run("load and store stack failures", func(t *testing.T) {
		expectStepErr(t, coverageFuncValidator(&Module{}, nil), Instruction{Kind: InstrI32Load}, ErrUnknownMemory)
		expectStepErr(t, coverageFuncValidator(&Module{}, nil), Instruction{Kind: InstrI32Store}, ErrUnknownMemory)
		fv := coverageFuncValidator(&Module{Memories: []MemType{{Limits: Limits{Min: 1}}}}, nil)
		expectStepErr(t, fv, Instruction{Kind: InstrI32Load}, ErrTypeMismatch)
		fv = coverageFuncValidator(&Module{Memories: []MemType{{Limits: Limits{Min: 1}}}}, nil)
		expectStepErr(t, fv, Instruction{Kind: InstrI32Store}, ErrTypeMismatch)
	})
	t.Run("block and if input arity failures", func(t *testing.T) {
		m := &Module{Types: []RecType{ft([]ValType{I32}, nil)}}
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 0}}}}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrIf, ext: &instrExt{BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 0}}}}, ErrTypeMismatch)
	})
	t.Run("control frame helpers reject malformed state", func(t *testing.T) {
		fv := coverageFuncValidator(&Module{}, nil)
		if err := fv.pushCtrl(ctrlBlock, []ValType{I32}, nil); err == nil {
			t.Fatal("pushCtrl accepted missing input")
		}
		if _, err := (&funcValidator{}).popCtrl(); err == nil {
			t.Fatal("popCtrl accepted empty control stack")
		}
		fv = coverageFuncValidator(&Module{}, nil)
		fv.push(I32)
		if _, err := fv.popCtrl(); err == nil {
			t.Fatal("popCtrl accepted leftover values")
		}
	})
	t.Run("if branch bookkeeping errors", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrIf, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: ValType{Kind: ValTypeKind(99)}}}}), ErrUnknownType)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrIf, ext: &instrExt{Then: []Instruction{{Kind: InstrLocalGet, Index: 0}}}}), ErrUnknownLocal)
		expectValidateErr(t, modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrIf, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Then: []Instruction{{Kind: InstrI32Const}}}}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrIf, ext: &instrExt{Then: []Instruction{{Kind: InstrI32Const}}, Else: []Instruction{}}}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrIf, ext: &instrExt{Then: []Instruction{}, Else: []Instruction{{Kind: InstrLocalGet, Index: 0}}}}), ErrUnknownLocal)
	})
	t.Run("branch table negative paths", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBrIf, Index: 1}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrIf, Index: 1}), ErrUnknownLabel)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrIf, Index: 0}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBrTable, Index: 0}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrTable, Index: 0, ext: &instrExt{Indices: []uint32{1}}}), ErrUnknownLabel)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrBrTable, Index: 0}}}}}), ErrTypeMismatch)
	})
	t.Run("calls locals globals refs and tables negative paths", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrReturn}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrCall, Index: 1}), ErrUnknownFunc)
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, []ValType{I64}), ft(nil, []ValType{I32})}, Imports: []Import{{Type: ExternType{Kind: ExternFunc, Type: TypeIdx{Index: 0}}}}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrReturnCall, Index: 0}}}}}}, ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrCallIndirect, Index: 9}), ErrUnknownType)
		ci := &Module{Types: []RecType{ft(nil, nil), ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 1}}, Tables: []Table{{Type: TableType{Ref: AbsRef(HeapExtern), Limits: Limits{Min: 1}}}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrCallIndirect, Index: 0}}}}}}
		expectValidateErr(t, ci, ErrTypeMismatch)
		ci.Tables[0].Type.Ref = AbsRef(HeapFunc)
		ci.Code[0].Body.Instrs = []Instruction{{Kind: InstrCallIndirect, Index: 0}}
		expectValidateErr(t, ci, ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrLocalSet, Index: 0}), ErrUnknownLocal)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrLocalTee, Index: 0}), ErrUnknownLocal)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrGlobalGet, Index: 0}), ErrUnknownGlobal)
		expectValidateErr(t, &Module{Globals: []Global{{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}, Types: []RecType{ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 0}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrGlobalSet, Index: 0}}}}}}, ErrImmutableGlobal)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrTableGet, Index: 0}), ErrUnknownTable)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrTableSet, Index: 0}), ErrUnknownTable)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: Ref(true, HeapType{Kind: HeapTypeKind(99)}, false)}}), ErrUnknownType)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrRefFunc, Index: 1}, Instruction{Kind: InstrDrop}), ErrUnknownFunc)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrRefIsNull}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrRefEq}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrStringConst}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefAsNonNull}), ErrTypeMismatch)
	})
}

func TestValidatorCoverageMoreCoreStepBranches(t *testing.T) {
	t.Run("select success and operand failures", func(t *testing.T) {
		if err := ValidateModule(modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{I32}}})); err != nil {
			t.Fatalf("typed select: %v", err)
		}
		if err := ValidateModule(modWithFunc(nil, []ValType{I64}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect})); err != nil {
			t.Fatalf("untyped select: %v", err)
		}
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{I32}}}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{I32}}}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect}), ErrTypeMismatch)
	})
	t.Run("reference branch failures", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBrOnNull, Index: 1}), ErrUnknownLabel)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrOnNull, Index: 0}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBrOnNonNull, Index: 1}), ErrUnknownLabel)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrOnNonNull, Index: 0}), ErrTypeMismatch)
	})
	t.Run("memory bulk negative paths", func(t *testing.T) {
		base := func(body ...Instruction) *Module {
			m := modWithFunc(nil, nil, body...)
			m.Memories = []MemType{{Limits: Limits{Min: 1}}}
			m.DataCount = ptr(uint32(1))
			m.Data = []Data{{Mode: DataMode{Kind: DataPassive}}}
			return m
		}
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrMemoryInit, Index: 1}), ErrInvalidDataCount)
		expectValidateErr(t, base(Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryInit}), ErrTypeMismatch)
		expectValidateErr(t, base(Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryCopy}), ErrTypeMismatch)
		expectValidateErr(t, base(Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryFill}), ErrTypeMismatch)
		expectValidateErr(t, base(Instruction{Kind: InstrMemoryGrow}), ErrTypeMismatch)
		if err := ValidateModule(base(Instruction{Kind: InstrMemorySize}, Instruction{Kind: InstrDrop})); err != nil {
			t.Fatalf("memory.size: %v", err)
		}
	})
	t.Run("table bulk negative paths", func(t *testing.T) {
		base := func(body ...Instruction) *Module {
			m := modWithFunc(nil, nil, body...)
			m.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
			m.Elements = []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}
			return m
		}
		expectValidateErr(t, base(Instruction{Kind: InstrTableInit, Index: 1}), ErrUnknownTable)
		expectValidateErr(t, base(Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableInit}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrTableCopy}), ErrUnknownTable)
		expectValidateErr(t, base(Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableCopy}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrElemDrop}), ErrUnknownTable)
		expectValidateErr(t, base(Instruction{Kind: InstrTableSize, Index: 1}), ErrUnknownTable)
		expectValidateErr(t, base(Instruction{Kind: InstrTableGrow, Index: 1}), ErrUnknownTable)
		expectValidateErr(t, base(Instruction{Kind: InstrTableGrow}), ErrTypeMismatch)
		expectValidateErr(t, base(Instruction{Kind: InstrTableFill, Index: 1}), ErrUnknownTable)
		expectValidateErr(t, base(Instruction{Kind: InstrTableFill}), ErrTypeMismatch)
	})
	t.Run("stack effect pop failures", func(t *testing.T) {
		for _, in := range []Instruction{
			{Kind: InstrI32Clz},
			{Kind: InstrI32Add},
			{Kind: InstrI32Eq},
			{Kind: InstrI32Eqz},
			{Kind: InstrI64ExtendI32S},
			{Kind: InstrI32TruncSatF32S},
			{Kind: InstrI32TruncSatF64S},
			{Kind: InstrI64TruncSatF32S},
			{Kind: InstrI64TruncSatF64S},
		} {
			expectStepErr(t, coverageFuncValidator(&Module{}, nil), in, ErrTypeMismatch)
		}
		expectStepErr(t, coverageFuncValidator(&Module{}, nil), Instruction{Kind: InstrMemorySize}, ErrUnknownMemory)
	})
	t.Run("core success tails", func(t *testing.T) {
		if !sameValTypes([]ValType{I32}, []ValType{I32}) || sameValTypes([]ValType{I32}, []ValType{I64}) || sameValTypes([]ValType{I32}, nil) {
			t.Fatal("sameValTypes")
		}
		if err := ValidateModule(modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrReturn})); err != nil {
			t.Fatalf("return success: %v", err)
		}
		if err := ValidateModule(modWithFunc(nil, nil, Instruction{Kind: InstrReturnCall, Index: 0})); err != nil {
			t.Fatalf("return_call success: %v", err)
		}
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrCall, Index: 1}, Instruction{Kind: InstrDrop})
		m.Types = []RecType{ft(nil, nil), ft([]ValType{I32}, []ValType{I32})}
		m.Imports = []Import{{Type: ExternType{Kind: ExternFunc, Type: TypeIdx{Index: 1}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("call success: %v", err)
		}
		m = modWithFunc([]ValType{I32}, nil, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalTee, Index: 0}, Instruction{Kind: InstrLocalSet, Index: 0})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("locals success: %v", err)
		}
		m = modWithFunc(nil, nil, Instruction{Kind: InstrGlobalGet, Index: 0}, Instruction{Kind: InstrGlobalSet, Index: 0})
		m.Globals = []Global{{Type: GlobalType{Type: I32, Mutable: true}, Init: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("globals success: %v", err)
		}
		m = modWithFunc(nil, nil,
			Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableGet}, Instruction{Kind: InstrDrop},
			Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}, Instruction{Kind: InstrTableSet},
		)
		m.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("table get/set success: %v", err)
		}
	})
	t.Run("remaining core operand order branches", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrIf, ext: &instrExt{Then: nil, Else: []Instruction{{Kind: InstrI32Const}}}}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrBrIf, Index: 0}, {Kind: InstrI32Const}}}}}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrBrTable, Index: 0, ext: &instrExt{Indices: []uint32{1}}}}}}}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil,
			Instruction{Kind: InstrBlock, ext: &instrExt{Body: Expr{Instrs: []Instruction{
				{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{
					{Kind: InstrI32Const},
					{Kind: InstrBrTable, Index: 1, ext: &instrExt{Indices: []uint32{0}}},
				}}}},
			}}}},
		), ErrTypeMismatch)
		m := &Module{Types: []RecType{ft(nil, nil), ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrCallIndirect, Index: 0}}}}}}
		expectValidateErr(t, m, ErrUnknownTable)
		m.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1, Addr64: true}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
		m = &Module{Types: []RecType{ft(nil, []ValType{I64}), ft(nil, []ValType{I32})}, FuncTypes: []TypeIdx{{Index: 1}}, Tables: []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrReturnCallIndirect, Index: 0}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
		if err := ValidateModule(modWithFunc(nil, nil, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapEq)}}, Instruction{Kind: InstrRefIsNull}, Instruction{Kind: InstrDrop})); err != nil {
			t.Fatalf("ref.is_null success: %v", err)
		}
		if err := ValidateModule(modWithFunc(nil, nil, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapEq)}}, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapEq)}}, Instruction{Kind: InstrRefEq}, Instruction{Kind: InstrDrop})); err != nil {
			t.Fatalf("ref.eq success: %v", err)
		}
		if err := ValidateModule(&Module{StringRefs: [][]byte{[]byte("x")}, Types: []RecType{ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 0}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrStringConst}, {Kind: InstrDrop}}}}}}); err != nil {
			t.Fatalf("string.const success: %v", err)
		}
		if err := ValidateModule(modWithFunc(nil, []ValType{RefVal(Ref(false, AbsHeap(HeapEq), false))}, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapEq)}}, Instruction{Kind: InstrRefAsNonNull})); err != nil {
			t.Fatalf("ref.as_non_null success: %v", err)
		}
		m = modWithFunc(nil, nil, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapEq)}}, Instruction{Kind: InstrBrOnNull, Index: 0}, Instruction{Kind: InstrDrop})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("br_on_null success: %v", err)
		}
		m = modWithFunc(nil, nil, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapEq)}}, Instruction{Kind: InstrBrOnNonNull, Index: 0}, Instruction{Kind: InstrDrop})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("br_on_non_null success: %v", err)
		}
	})
}

func TestValidatorCoverageProposalNegativeBranches(t *testing.T) {
	t.Run("call_ref negative paths", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrCallRef, Index: 99}), ErrUnknownType)
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 0}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrCallRef}}}}}}, ErrTypeMismatch)
		expectValidateErr(t, &Module{Types: []RecType{ft([]ValType{I32}, nil), ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}, {Kind: InstrCallRef, Index: 0}}}}}}, ErrTypeMismatch)
		m := &Module{Types: []RecType{ft(nil, []ValType{I32}), ft(nil, []ValType{I32})}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: Ref(true, IndexedHeap(TypeIdx{Index: 0}), false)}}, {Kind: InstrReturnCallRef, Index: 0}}}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("return_call_ref: %v", err)
		}
	})
	t.Run("try_table negative paths", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrTryTable, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: ValType{Kind: ValTypeKind(99)}}}}), ErrUnknownType)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBlock, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrTryTable, ext: &instrExt{Catches: []Catch{{Kind: CatchTag, Tag: 99, Label: 0}}}}}}}}), ErrUnknownTag)
		m := &Module{Types: []RecType{ft([]ValType{I32}, nil), ft(nil, nil)}, Tags: []TagType{{Type: TypeIdx{Index: 0}}}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrBlock, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrTryTable, ext: &instrExt{Catches: []Catch{{Kind: CatchTag, Tag: 0, Label: 0}}}}}}}}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("atomic helpers and unsupported path", func(t *testing.T) {
		for _, op := range []uint32{0, 30, 31, 32, 33, 34, 35, 36} {
			_ = atomicRmwEffect(op)
		}
		for _, op := range []uint32{0, 72, 73, 74, 75, 76, 77, 78} {
			_ = atomicCmpxchgEffect(op)
		}
		expectStepErr(t, coverageFuncValidator(&Module{Memories: []MemType{{Limits: Limits{Min: 1}}}}, nil), Instruction{Kind: InstrI32AtomicLoad}, ErrInvalidSharedMemory)
		expectStepErr(t, coverageFuncValidator(&Module{Memories: []MemType{{Shared: true, Limits: Limits{Min: 1, Max: ptr(uint64(1))}}}}, nil), Instruction{Kind: InstrI32AtomicLoad}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(&Module{Memories: []MemType{{Shared: true, Limits: Limits{Min: 1, Max: ptr(uint64(1))}}}}, nil), Instruction{Kind: InstrInvalid}, ErrUnsupportedValidationOpcode)
	})
}

func TestValidatorCoverageGCAndSIMDNegativeBranches(t *testing.T) {
	t.Run("gc opcode pop failures", func(t *testing.T) {
		for _, in := range []Instruction{
			{Kind: InstrRefI31},
			{Kind: InstrI31GetS},
			{Kind: InstrAnyConvertExtern},
			{Kind: InstrExternConvertAny},
			{Kind: InstrRefTest, ext: &instrExt{HeapType: AbsHeap(HeapEq)}},
			{Kind: InstrRefCast, ext: &instrExt{HeapType: AbsHeap(HeapEq)}},
			{Kind: InstrRefGetDesc, Index: 0},
			{Kind: InstrArrayLen},
		} {
			m := &Module{Types: []RecType{structType(nil, TypeMetadata{Descriptor: ptr(TypeIdx{Index: 1})}), structType(nil, TypeMetadata{})}}
			expectStepErr(t, coverageFuncValidator(m, nil), in, ErrTypeMismatch)
		}
	})
	t.Run("struct and array constructor failures", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{structType([]FieldType{field(refToType(0, false), Var)}, TypeMetadata{}), ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrStructNewDefault, Index: 0}, {Kind: InstrDrop}}}}}}, ErrTypeMismatch)
		expectValidateErr(t, &Module{Types: []RecType{structType([]FieldType{field(I32, Var)}, TypeMetadata{}), ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrStructNew, Index: 0}, {Kind: InstrDrop}}}}}}, ErrTypeMismatch)
		expectValidateErr(t, &Module{Types: []RecType{arrayType(field(I32, Var)), ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrArrayNewFixed, Index: 0, Index2: 1}, {Kind: InstrDrop}}}}}}, ErrTypeMismatch)
	})
	t.Run("simd pop and memory failures", func(t *testing.T) {
		expectStepErr(t, coverageFuncValidator(&Module{}, nil), Instruction{Kind: InstrV128Load}, ErrUnknownMemory)
		expectStepErr(t, coverageFuncValidator(&Module{Memories: []MemType{{Limits: Limits{Min: 1}}}}, nil), Instruction{Kind: InstrV128Load}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(&Module{Memories: []MemType{{Limits: Limits{Min: 1}}}}, nil), Instruction{Kind: InstrV128Store}, ErrTypeMismatch)
		for _, in := range []Instruction{
			{Kind: InstrI8x16Splat},
			{Kind: InstrI8x16ExtractLaneS},
			{Kind: InstrI8x16ReplaceLane},
			{Kind: InstrI8x16Swizzle},
			{Kind: InstrI8x16Shl},
			{Kind: InstrV128Not},
			{Kind: InstrV128And},
			{Kind: InstrV128AnyTrue},
			{Kind: InstrV128Bitselect},
		} {
			expectStepErr(t, coverageFuncValidator(&Module{}, nil), in, ErrTypeMismatch)
		}
		expectStepErr(t, coverageFuncValidator(&Module{}, nil), Instruction{Kind: InstrI8x16ExtractLaneS, Lane: 16}, ErrTypeMismatch)
	})
}

func TestValidatorRelaxedSIMDTernaryShape(t *testing.T) {
	for _, kind := range []InstrKind{
		InstrF32x4RelaxedMadd,
		InstrF32x4RelaxedNmadd,
		InstrF64x2RelaxedMadd,
		InstrF64x2RelaxedNmadd,
		InstrI32x4RelaxedDotI8x16I7x16AddS,
	} {
		fv := coverageFuncValidatorWithStack(&Module{}, V128, V128, V128)
		if err := fv.step(Instruction{Kind: kind}); err != nil {
			t.Fatalf("%s ternary shape: %v", kind, err)
		}
		if len(fv.vals) != 1 || fv.vals[0].t != V128 {
			t.Fatalf("%s stack = %#v, want one v128 result", kind, fv.vals)
		}
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, V128, V128), Instruction{Kind: kind}, ErrTypeMismatch)
	}
}

func TestValidatorCoverageMoreProposalBranches(t *testing.T) {
	gcModule := func() *Module {
		return &Module{
			Types: []RecType{
				structType([]FieldType{field(I32, Var), field(I64, Const)}, TypeMetadata{}),
				arrayType(field(I32, Var)),
				arrayType(field(I64, Const)),
				structType(nil, TypeMetadata{Descriptor: ptr(TypeIdx{Index: 4})}),
				structType(nil, TypeMetadata{Describes: ptr(TypeIdx{Index: 3})}),
			},
			DataCount: ptr(uint32(1)),
			Data:      []Data{{Mode: DataMode{Kind: DataPassive}}},
			Elements:  []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}},
		}
	}
	t.Run("gc cast and descriptor branches", func(t *testing.T) {
		m := gcModule()
		expectStepErr(t, coverageFuncValidatorWithStack(m, AnyRef), Instruction{Kind: InstrRefTest, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 99})}}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrRefTest, ext: &instrExt{HeapType: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, FuncRef), Instruction{Kind: InstrRefTest, ext: &instrExt{HeapType: AbsHeap(HeapI31)}}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, AnyRef), Instruction{Kind: InstrRefCast, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 99})}}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidatorWithStack(m, AnyRef), Instruction{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, AnyRef, I32), Instruction{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, FuncRef, RefVal(AbsRef(HeapFunc))), Instruction{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: AbsHeap(HeapI31)}}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrRefGetDesc, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(0, true)), Instruction{Kind: InstrRefGetDesc, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrRefGetDesc, Index: 3}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(0, true)), Instruction{Kind: InstrRefGetDesc, Index: 3}, ErrTypeMismatch)
	})
	t.Run("struct field branches", func(t *testing.T) {
		m := gcModule()
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrStructGet, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrStructGet, Index: 0, Index2: 9}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrStructGet, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrStructSet, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrStructSet, Index: 0, Index2: 9}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrStructSet, Index: 0, Index2: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(0, true)), Instruction{Kind: InstrStructSet, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrStructSet, Index: 0}, ErrTypeMismatch)
	})
	t.Run("array field branches", func(t *testing.T) {
		m := gcModule()
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayGet, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayGet, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrArrayGet, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArraySet, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArraySet, Index: 2}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(1, true), I32), Instruction{Kind: InstrArraySet, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(1, true)), Instruction{Kind: InstrArraySet, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrArrayLen}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayFill, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(1, true), I32), Instruction{Kind: InstrArrayFill, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayCopy, Index: 99, Index2: 1}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(1, true), I32, refToType(1, true), I32), Instruction{Kind: InstrArrayCopy, Index: 1, Index2: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayInitData, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayInitData, Index: 1, Index2: 99}, ErrInvalidDataCount)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(1, true), I32), Instruction{Kind: InstrArrayInitData, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayInitElem, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayInitElem, Index: 1, Index2: 99}, ErrUnknownTable)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(1, true), I32), Instruction{Kind: InstrArrayInitElem, Index: 1}, ErrTypeMismatch)
	})
	t.Run("constructors and br_on_cast branches", func(t *testing.T) {
		m := gcModule()
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrStructNew, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrStructNew, Index: 3}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrStructNewDefaultDesc, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrStructNewDefaultDesc, Index: 3}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayNew, Index: 99}, ErrUnknownType)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrArrayNew, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrArrayNewDefault, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrArrayNewData, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrArrayNewElem, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrBrOnCast, Index: 1}, ErrUnknownLabel)
		fv := coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{I32})
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCast, Index: 0}, ErrTypeMismatch)
		fv = coverageFuncValidatorWithStack(&Module{}, I32)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{AnyRef})
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCast, Index: 0, ext: &instrExt{HeapType: AbsHeap(HeapEq), HeapType2: AbsHeap(HeapAny)}}, ErrTypeMismatch)
	})
	t.Run("atomics and simd operand order branches", func(t *testing.T) {
		shared := &Module{Memories: []MemType{{Shared: true, Limits: Limits{Min: 1, Max: ptr(uint64(1))}}}}
		expectStepErr(t, coverageFuncValidator(shared, nil), Instruction{Kind: InstrMemoryAtomicNotify}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I32), Instruction{Kind: InstrMemoryAtomicNotify}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(shared, nil), Instruction{Kind: InstrMemoryAtomicWait64}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I64), Instruction{Kind: InstrMemoryAtomicWait32}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I32), Instruction{Kind: InstrI32AtomicStore}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I32), Instruction{Kind: InstrAtomicRmw}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I32, I32), Instruction{Kind: InstrAtomicCmpxchg}, ErrTypeMismatch)
		mem := &Module{Memories: []MemType{{Limits: Limits{Min: 1}}}}
		expectStepErr(t, coverageFuncValidator(mem, nil), Instruction{Kind: InstrV128Load8Lane}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(mem, V128), Instruction{Kind: InstrV128Load8Lane}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(mem, I32), Instruction{Kind: InstrV128Store8Lane}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, V128), Instruction{Kind: InstrI8x16ReplaceLane}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, V128), Instruction{Kind: InstrI8x16Shl}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, V128), Instruction{Kind: InstrV128And}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, V128, V128), Instruction{Kind: InstrV128Bitselect}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(&Module{}, nil), Instruction{Kind: InstrInvalid}, ErrUnsupportedValidationOpcode)
	})
	t.Run("remaining proposal operand branches", func(t *testing.T) {
		m := gcModule()
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{Types: []RecType{ft(nil, nil)}}, I32), Instruction{Kind: InstrCallRef}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{Types: []RecType{ft([]ValType{I32}, nil)}}, RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false))), Instruction{Kind: InstrCallRef}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{Types: []RecType{ft(nil, []ValType{I32})}}, RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false))), Instruction{Kind: InstrReturnCallRef}, ErrTypeMismatch)
		tv := coverageFuncValidator(&Module{Types: []RecType{ft([]ValType{I32}, nil)}, Tags: []TagType{{Type: TypeIdx{Index: 9}}}}, nil)
		_ = tv.pushCtrl(ctrlBlock, nil, []ValType{I32})
		expectStepErr(t, tv, Instruction{Kind: InstrTryTable, ext: &instrExt{Catches: []Catch{{Kind: CatchTag, Tag: 0, Label: 0}}}}, ErrUnknownTag)
		tv = coverageFuncValidator(&Module{}, nil)
		expectStepErr(t, tv, Instruction{Kind: InstrTryTable, ext: &instrExt{BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 0}}}}, ErrUnknownType)
		tv = coverageFuncValidator(&Module{Types: []RecType{ft([]ValType{I32}, nil)}}, nil)
		expectStepErr(t, tv, Instruction{Kind: InstrTryTable, ext: &instrExt{BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 0}}}}, ErrTypeMismatch)
		tv = coverageFuncValidator(&Module{}, nil)
		_ = tv.pushCtrl(ctrlBlock, nil, nil)
		expectStepErr(t, tv, Instruction{Kind: InstrTryTable, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}}}}}, ErrUnknownLocal)
		tv = coverageFuncValidator(&Module{}, nil)
		_ = tv.pushCtrl(ctrlBlock, nil, nil)
		expectStepErr(t, tv, Instruction{Kind: InstrTryTable, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32), Instruction{Kind: InstrRefCast, ext: &instrExt{HeapType: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(m, nil), Instruction{Kind: InstrRefCastDescEq, ext: &instrExt{HeapType: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		if err := coverageFuncValidatorWithStack(m, RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 3}), false))).step(Instruction{Kind: InstrRefGetDesc, Index: 3}); err != nil {
			t.Fatalf("ref.get_desc success: %v", err)
		}
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(1, true), I32, I32), Instruction{Kind: InstrArrayFill, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, refToType(1, true), I32, I32, I32, I32), Instruction{Kind: InstrArrayCopy, Index: 1, Index2: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32, I32, I32), Instruction{Kind: InstrArrayInitData, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I32, I32, I32), Instruction{Kind: InstrArrayInitElem, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I64, I32), Instruction{Kind: InstrArrayNew, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I64, I32), Instruction{Kind: InstrArrayNewData, Index: 1}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(m, I64, I32), Instruction{Kind: InstrArrayNewElem, Index: 1}, ErrTypeMismatch)
		fv := coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{AnyRef})
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCast, Index: 0, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		fv = coverageFuncValidatorWithStack(&Module{}, AnyRef)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{FuncRef})
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCastFail, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		mem := &Module{Memories: []MemType{{Limits: Limits{Min: 1}}}}
		expectStepErr(t, coverageFuncValidator(mem, nil), Instruction{Kind: InstrV128Store}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(mem, nil), Instruction{Kind: InstrV128Load8Lane}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, I32), Instruction{Kind: InstrI8x16Swizzle}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, V128), Instruction{Kind: InstrV128Bitselect}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(&Module{}, nil), Instruction{Kind: InstrMemoryAtomicNotify, ext: &instrExt{MemArg: MemArg{Mem: ptr(MemIdx(1))}}}, ErrUnknownMemory)
	})
}

func TestValidatorCoverageFinalTailBranches(t *testing.T) {
	t.Run("core final tail branches", func(t *testing.T) {
		m := &Module{Types: []RecType{ft([]ValType{I32}, nil), ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 1}}, Tables: []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrCallIndirect, Index: 0}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
		m = &Module{Types: []RecType{ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 0}}, Tables: []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}, {Kind: InstrReturnCallIndirect, Index: 0}}}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("return_call_indirect success: %v", err)
		}
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapEq)}}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefEq}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrRefAsNonNull}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBrOnNull, Index: 0}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrOnNull, Index: 0}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBrOnNonNull, Index: 0}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrOnNonNull, Index: 0}), ErrTypeMismatch)
		mem := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryInit})
		mem.Memories = []MemType{{Limits: Limits{Min: 1}}}
		mem.DataCount = ptr(uint32(1))
		mem.Data = []Data{{Mode: DataMode{Kind: DataPassive}}}
		expectValidateErr(t, mem, ErrTypeMismatch)
		mem.Code[0].Body.Instrs = []Instruction{{Kind: InstrMemoryCopy}}
		expectValidateErr(t, mem, ErrTypeMismatch)
		mem.Code[0].Body.Instrs = []Instruction{{Kind: InstrI32Const}, {Kind: InstrMemoryFill}}
		expectValidateErr(t, mem, ErrTypeMismatch)
		tab := modWithFunc(nil, nil, Instruction{Kind: InstrTableInit})
		tab.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapExtern), Limits: Limits{Min: 1}}}}
		tab.Elements = []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}
		expectValidateErr(t, tab, ErrTypeMismatch)
		tab.Tables[0].Type.Ref = AbsRef(HeapFunc)
		tab.Code[0].Body.Instrs = []Instruction{{Kind: InstrI32Const}, {Kind: InstrTableInit}}
		expectValidateErr(t, tab, ErrTypeMismatch)
		tab.Code[0].Body.Instrs = []Instruction{{Kind: InstrTableCopy, Index2: 1}}
		expectValidateErr(t, tab, ErrUnknownTable)
		tab.Tables = append(tab.Tables, Table{Type: TableType{Ref: AbsRef(HeapExtern), Limits: Limits{Min: 1}}})
		tab.Code[0].Body.Instrs = []Instruction{{Kind: InstrTableCopy, Index: 1, Index2: 0}}
		expectValidateErr(t, tab, ErrTypeMismatch)
		tab.Code[0].Body.Instrs = []Instruction{{Kind: InstrI32Const}, {Kind: InstrTableGrow}}
		expectValidateErr(t, tab, ErrTypeMismatch)
		tab.Code[0].Body.Instrs = []Instruction{{Kind: InstrI32Const}, {Kind: InstrTableFill}}
		expectValidateErr(t, tab, ErrTypeMismatch)
	})
	t.Run("proposal final tail branches", func(t *testing.T) {
		m := &Module{Types: []RecType{ft([]ValType{I64}, nil)}, Tags: []TagType{{Type: TypeIdx{Index: 0}}}}
		fv := coverageFuncValidator(m, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{I32})
		expectStepErr(t, fv, Instruction{Kind: InstrTryTable, ext: &instrExt{Catches: []Catch{{Kind: CatchTag, Tag: 0, Label: 0}}}}, ErrTypeMismatch)
		shared := &Module{Memories: []MemType{{Shared: true, Limits: Limits{Min: 1, Max: ptr(uint64(1))}}}}
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I64, I32), Instruction{Kind: InstrMemoryAtomicWait32}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I64, I32), Instruction{Kind: InstrI32AtomicStore}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I64, I32, I32), Instruction{Kind: InstrAtomicCmpxchg}, ErrTypeMismatch)
		if err := coverageFuncValidator(shared, nil).stepAtomic(Instruction{Kind: InstrNop}); !isValidationCode(err, ErrUnsupportedValidationOpcode) {
			t.Fatalf("expected atomic fallback error, got %v", err)
		}
		gm := &Module{Types: []RecType{arrayType(field(I32, Var)), structType(nil, TypeMetadata{})}, DataCount: ptr(uint32(1)), Data: []Data{{Mode: DataMode{Kind: DataPassive}}}, Elements: []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}}
		if err := coverageFuncValidatorWithStack(gm, EqRef).step(Instruction{Kind: InstrRefTest, ext: &instrExt{HeapType: AbsHeap(HeapEq)}}); err != nil {
			t.Fatalf("ref.test success: %v", err)
		}
		if err := coverageFuncValidatorWithStack(gm, EqRef).step(Instruction{Kind: InstrRefCast, ext: &instrExt{HeapType: AbsHeap(HeapEq)}}); err != nil {
			t.Fatalf("ref.cast success: %v", err)
		}
		expectStepErr(t, coverageFuncValidatorWithStack(gm, refToType(0, true), I32), Instruction{Kind: InstrArrayFill, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(gm, I32, I32, refToType(0, true), I32, I32), Instruction{Kind: InstrArrayCopy, Index: 0, Index2: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(gm, refToType(0, true), I32), Instruction{Kind: InstrArrayNew, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(gm, I32), Instruction{Kind: InstrArrayNewData, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(gm, I32), Instruction{Kind: InstrArrayNewElem, Index: 0}, ErrTypeMismatch)
		fv = coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{AnyRef})
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		fv = coverageFuncValidatorWithStack(&Module{}, AnyRef)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{EqRef})
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		mem := &Module{}
		expectStepErr(t, coverageFuncValidator(mem, nil), Instruction{Kind: InstrV128Store}, ErrUnknownMemory)
		expectStepErr(t, coverageFuncValidator(mem, nil), Instruction{Kind: InstrV128Load8Lane}, ErrUnknownMemory)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, I32), Instruction{Kind: InstrI8x16ReplaceLane}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, I32), Instruction{Kind: InstrI8x16Shl}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, V128), Instruction{Kind: InstrV128And}, ErrTypeMismatch)
		if err := coverageFuncValidator(&Module{}, nil).stepSIMD(Instruction{Kind: InstrNop}); !isValidationCode(err, ErrUnsupportedValidationOpcode) {
			t.Fatalf("expected simd fallback error, got %v", err)
		}
	})
}

func TestValidatorCoverageLastPassBranches(t *testing.T) {
	t.Run("core last pass", func(t *testing.T) {
		if err := ValidateModule(modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrTable, Index: 0})); err != nil {
			t.Fatalf("br_table success: %v", err)
		}
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{I32}}}), ErrTypeMismatch)
		fv := coverageFuncValidator(&Module{Globals: []Global{{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}, nil)
		fv.constOnly = true
		expectStepErr(t, fv, Instruction{Kind: InstrGlobalGet}, ErrConstExprRequired)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, EqRef), Instruction{Kind: InstrRefEq}, ErrTypeMismatch)
		fv = coverageFuncValidatorWithStack(&Module{}, AnyRef)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{I32})
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnNull, Index: 0}, ErrTypeMismatch)
		fv = coverageFuncValidatorWithStack(&Module{}, AnyRef)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{I32})
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnNonNull, Index: 0}, ErrTypeMismatch)
		mem := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryInit})
		mem.Memories = []MemType{{Limits: Limits{Min: 1}}}
		mem.DataCount = ptr(uint32(1))
		mem.Data = []Data{{Mode: DataMode{Kind: DataPassive}}}
		expectValidateErr(t, mem, ErrTypeMismatch)
		mem.Code[0].Body.Instrs = []Instruction{{Kind: InstrI64Const}, {Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrMemoryCopy}}
		expectValidateErr(t, mem, ErrTypeMismatch)
		mem.Code[0].Body.Instrs = []Instruction{{Kind: InstrI64Const}, {Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrMemoryFill}}
		expectValidateErr(t, mem, ErrTypeMismatch)
		tab := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableInit})
		tab.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
		tab.Elements = []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}
		expectValidateErr(t, tab, ErrTypeMismatch)
		tab.Code[0].Body.Instrs = []Instruction{{Kind: InstrI64Const}, {Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}, {Kind: InstrI32Const}, {Kind: InstrTableFill}}
		expectValidateErr(t, tab, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{Memories: []MemType{{Limits: Limits{Min: 1}}}}, I64), Instruction{Kind: InstrMemoryGrow}, ErrTypeMismatch)
	})
	t.Run("proposal last pass", func(t *testing.T) {
		shared := &Module{Memories: []MemType{{Shared: true, Limits: Limits{Min: 1, Max: ptr(uint64(1))}}}}
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I64, I32, I64), Instruction{Kind: InstrMemoryAtomicWait32}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I64), Instruction{Kind: InstrI32AtomicStore}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(shared, I32), Instruction{Kind: InstrAtomicCmpxchg}, ErrTypeMismatch)
		if _, err := coverageFuncValidator(shared, nil).checkSharedMemArg(MemArg{Mem: ptr(MemIdx(0))}, 0); err != nil {
			t.Fatalf("explicit memory shared arg: %v", err)
		}
		gm := &Module{Types: []RecType{arrayType(field(I32, Var)), structType([]FieldType{field(I32, Var)}, TypeMetadata{}), ft(nil, nil)}, DataCount: ptr(uint32(1)), Data: []Data{{Mode: DataMode{Kind: DataPassive}}}, Elements: []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}}
		if err := coverageFuncValidatorWithStack(gm, I32).step(Instruction{Kind: InstrStructNew, Index: 1}); err != nil {
			t.Fatalf("struct.new success: %v", err)
		}
		expectStepErr(t, coverageFuncValidator(gm, nil), Instruction{Kind: InstrArrayFill, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(gm, nil), Instruction{Kind: InstrArrayCopy, Index: 0, Index2: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(gm, I64, I32, refToType(0, true), I32, I32), Instruction{Kind: InstrArrayCopy, Index: 0, Index2: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(gm, nil), Instruction{Kind: InstrArrayInitData, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(gm, nil), Instruction{Kind: InstrArrayInitElem, Index: 0}, ErrTypeMismatch)
		if err := coverageFuncValidator(gm, nil).stepGC(Instruction{Kind: InstrNop}); !isValidationCode(err, ErrUnsupportedValidationOpcode) {
			t.Fatalf("expected gc fallback error, got %v", err)
		}
		if err := coverageFuncValidatorWithStack(gm, I32, I32).step(Instruction{Kind: InstrArrayNew, Index: 0}); err != nil {
			t.Fatalf("array.new success: %v", err)
		}
		if err := coverageFuncValidatorWithStack(gm, I32, I32).step(Instruction{Kind: InstrArrayNewData, Index: 0}); err != nil {
			t.Fatalf("array.new_data success: %v", err)
		}
		if err := coverageFuncValidatorWithStack(gm, I32, I32).step(Instruction{Kind: InstrArrayNewElem, Index: 0}); err != nil {
			t.Fatalf("array.new_elem success: %v", err)
		}
		fv := coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{EqRef})
		fv.push(AnyRef)
		if err := fv.step(Instruction{Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}); err != nil {
			t.Fatalf("br_on_cast success: %v", err)
		}
		fv = coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{AnyRef})
		fv.push(AnyRef)
		if err := fv.step(Instruction{Kind: InstrBrOnCastFail, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}); err != nil {
			t.Fatalf("br_on_cast_fail success: %v", err)
		}
	})
}

func TestValidatorCoverageRemainingBranches(t *testing.T) {
	t.Run("core remaining", func(t *testing.T) {
		fv := coverageFuncValidator(&Module{}, nil)
		fv.unreachable()
		if err := fv.step(Instruction{Kind: InstrSelect}); err != nil {
			t.Fatalf("polymorphic select: %v", err)
		}
		fv = coverageFuncValidator(&Module{Globals: []Global{{Type: GlobalType{Type: I32}}}}, nil)
		fv.constOnly = true
		expectStepErr(t, fv, Instruction{Kind: InstrGlobalGet}, ErrConstExprRequired)
		fv = coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{I32})
		fv.push(AnyRef)
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnNull, Index: 0}, ErrTypeMismatch)
		fv = coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{I32})
		fv.push(AnyRef)
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnNonNull, Index: 0}, ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrMemoryInit}), ErrInvalidDataCount)
		m := modWithFunc(nil, nil, Instruction{Kind: InstrMemoryCopy})
		m.Memories = []MemType{{Limits: Limits{Min: 1}}}
		m.Code[0].Body.Instrs[0].Index = 1
		expectValidateErr(t, m, ErrUnknownMemory)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrMemoryFill}), ErrUnknownMemory)
		tab := modWithFunc(nil, nil, Instruction{Kind: InstrTableInit})
		tab.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
		tab.Elements = []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}}
		expectValidateErr(t, tab, ErrTypeMismatch)
		tab.Elements = []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}
		expectValidateErr(t, tab, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, I32), Instruction{Kind: InstrI32Add}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidatorWithStack(&Module{}, I32), Instruction{Kind: InstrI32Eq}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(&Module{}, nil), Instruction{Kind: InstrMemoryGrow}, ErrUnknownMemory)
	})
	t.Run("proposal remaining", func(t *testing.T) {
		gm := &Module{Types: []RecType{arrayType(field(I32, Var))}, DataCount: ptr(uint32(1)), Data: []Data{{Mode: DataMode{Kind: DataPassive}}}, Elements: []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}}}}}}}}
		expectStepErr(t, coverageFuncValidatorWithStack(gm, refToType(0, true), I64, refToType(0, true), I32, I32), Instruction{Kind: InstrArrayCopy, Index: 0, Index2: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(gm, nil), Instruction{Kind: InstrArrayNew, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(gm, nil), Instruction{Kind: InstrArrayNewData, Index: 0}, ErrTypeMismatch)
		expectStepErr(t, coverageFuncValidator(gm, nil), Instruction{Kind: InstrArrayNewElem, Index: 0}, ErrTypeMismatch)
		fv := coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{I31Ref})
		fv.push(FuncRef)
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapEq), HeapType2: AbsHeap(HeapI31)}}, ErrTypeMismatch)
		fv = coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{FuncRef})
		fv.push(AnyRef)
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCastFail, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		fv = coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{I32, EqRef})
		fv.push(I64)
		fv.push(AnyRef)
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}, ErrTypeMismatch)
		fv = coverageFuncValidator(&Module{}, nil)
		_ = fv.pushCtrl(ctrlBlock, nil, []ValType{I32, AnyRef})
		fv.push(I64)
		fv.push(AnyRef)
		expectStepErr(t, fv, Instruction{Kind: InstrBrOnCastFail, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}, ErrTypeMismatch)
	})
}

func TestValidatorCoverageFinalFiveBranches(t *testing.T) {
	expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrGlobalSet}), ErrUnknownGlobal)
	m := modWithFunc(nil, nil, Instruction{Kind: InstrMemoryInit})
	m.DataCount = ptr(uint32(1))
	m.Data = []Data{{Mode: DataMode{Kind: DataPassive}}}
	expectValidateErr(t, m, ErrUnknownMemory)
	m = modWithFunc(nil, nil, Instruction{Kind: InstrMemoryFill})
	m.Memories = []MemType{{Limits: Limits{Min: 1}}}
	expectValidateErr(t, m, ErrTypeMismatch)
	tab := modWithFunc(nil, nil, Instruction{Kind: InstrTableInit})
	tab.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
	tab.Elements = []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}}
	expectValidateErr(t, tab, ErrTypeMismatch)
	expectStepErr(t, coverageFuncValidator(tab, nil), Instruction{Kind: InstrTableInit}, ErrTypeMismatch)
	fv := coverageFuncValidator(&Module{}, nil)
	_ = fv.pushCtrl(ctrlBlock, nil, []ValType{FuncRef})
	fv.push(AnyRef)
	expectStepErr(t, fv, Instruction{Kind: InstrBrOnCast, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapAny), HeapType2: AbsHeap(HeapEq)}}, ErrTypeMismatch)
}
