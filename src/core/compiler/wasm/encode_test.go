package wasm

import (
	"bytes"
	"testing"
)

// Select shares decode opcode 0x1b with the untyped form but must encode as
// 0x1c + a result-type vector when typed. Guards against the fast simpleKindOpcode
// map path swallowing InstrSelect and always emitting 0x1b.
func TestEncodeExprSelect(t *testing.T) {
	// Untyped select -> 0x1b, then the expr-terminating 0x0b.
	got, err := EncodeExpr(Expr{Instrs: []Instruction{{Kind: InstrSelect}}})
	if err != nil {
		t.Fatalf("untyped select: %v", err)
	}
	if want := []byte{0x1b, 0x0b}; !bytes.Equal(got, want) {
		t.Errorf("untyped select = % x, want % x", got, want)
	}

	// Typed select -> 0x1c, count, result valtype(s), then 0x0b.
	vt, ok := EncodeValType(I32)
	if !ok {
		t.Fatal("EncodeValType(I32) not ok")
	}
	got, err = EncodeExpr(Expr{Instrs: []Instruction{{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{I32}}}}})
	if err != nil {
		t.Fatalf("typed select: %v", err)
	}
	if want := []byte{0x1c, 0x01, vt, 0x0b}; !bytes.Equal(got, want) {
		t.Errorf("typed select = % x, want % x", got, want)
	}
}

func TestEncodeExprInstructionShapes(t *testing.T) {
	block := func(kind InstrKind, body []Instruction) Instruction {
		return Instruction{Kind: kind, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: body}}}
	}
	ifInstr := Instruction{Kind: InstrIf, ext: &instrExt{
		BlockType: BlockType{Kind: BlockVoid},
		Then:      []Instruction{{Kind: InstrNop}},
		Else:      []Instruction{{Kind: InstrDrop}},
	}}
	e := Expr{Instrs: []Instruction{
		{Kind: InstrNop},
		{Kind: InstrI32Load, ext: &instrExt{MemArg: MemArg{Align: 2, Offset: 4}}},
		block(InstrBlock, []Instruction{{Kind: InstrNop}}),
		block(InstrLoop, []Instruction{{Kind: InstrBr, Index: 0}}),
		ifInstr,
		{Kind: InstrBrIf, Index: 3},
		{Kind: InstrBrTable, Index: 4, ext: &instrExt{Indices: []uint32{1, 2}}},
		{Kind: InstrCall, Index: 5},
		{Kind: InstrCallIndirect, Index: 6, Index2: 7},
		{Kind: InstrLocalGet, Index: 8}, {Kind: InstrLocalSet, Index: 9}, {Kind: InstrLocalTee, Index: 10},
		{Kind: InstrGlobalGet, Index: 11}, {Kind: InstrGlobalSet, Index: 12},
		{Kind: InstrMemorySize, Index: 0}, {Kind: InstrMemoryGrow, Index: 0},
		{Kind: InstrI32Const, I32: -65}, {Kind: InstrI64Const, I64: -66},
		{Kind: InstrF32Const, F32Bits: 0x3f800000}, {Kind: InstrF64Const, F64Bits: 0x3ff0000000000000},
		{Kind: InstrMemoryCopy, Index: 1, Index2: 2}, {Kind: InstrMemoryFill, Index: 3},
	}}
	got, err := EncodeExpr(e)
	if err != nil {
		t.Fatalf("encode shapes: %v", err)
	}
	if len(got) == 0 || got[len(got)-1] != 0x0b {
		t.Fatalf("encoded expression = %x", got)
	}
	for _, op := range []byte{0x28, 0x02, 0x03, 0x04, 0x0e, 0x10, 0x11, 0x41, 0x42, 0x43, 0x44, 0xfc} {
		if !bytes.Contains(got, []byte{op}) {
			t.Fatalf("encoded expression is missing opcode %#x: %x", op, got)
		}
	}
	if _, err := EncodeExpr(Expr{Instrs: []Instruction{{Kind: InstrI32Load, ext: &instrExt{MemArg: MemArg{Offset: 1 << 32}}}}}); err == nil {
		t.Fatal("memory64 offset was accepted by expression encoder")
	}
	if _, err := EncodeExpr(Expr{Instrs: []Instruction{{Kind: InstrInvalid}}}); err == nil {
		t.Fatal("invalid instruction was accepted by expression encoder")
	}
}

func TestEncodingHelpersAndStructuralTypeEquality(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    int64
		want []byte
	}{
		{"zero", 0, []byte{0}},
		{"negative one", -1, []byte{0x7f}},
		{"positive boundary", 64, []byte{0xc0, 0}},
		{"negative boundary", -65, []byte{0xbf, 0x7f}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []byte
			appendS64(&got, tc.v)
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("appendS64(%d) = %x, want %x", tc.v, got, tc.want)
			}
		})
	}
	var s32 []byte
	appendS32(&s32, -65)
	if !bytes.Equal(s32, []byte{0xbf, 0x7f}) {
		t.Fatalf("appendS32 = %x", s32)
	}
	var memarg []byte
	if err := appendU64AsU32(&memarg, 0x1234); err != nil || !bytes.Equal(memarg, []byte{0xb4, 0x24}) {
		t.Fatalf("appendU64AsU32 = %x, %v", memarg, err)
	}
	if err := appendU64AsU32(&memarg, 1<<32); err == nil {
		t.Fatal("wide memarg offset accepted")
	}

	var block []byte
	if err := appendBlockType(&block, BlockType{Kind: BlockVoid}); err != nil || !bytes.Equal(block, []byte{0x40}) {
		t.Fatalf("void block = %x, %v", block, err)
	}
	if err := appendBlockType(&block, BlockType{Kind: BlockVal, Val: I32}); err != nil || !bytes.Equal(block, []byte{0x40, 0x7f}) {
		t.Fatalf("value block = %x, %v", block, err)
	}
	if err := appendBlockType(&block, BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 2}}); err != nil {
		t.Fatalf("type-index block: %v", err)
	}
	if err := appendBlockType(&block, BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Rec: true}}); err == nil {
		t.Fatal("recursive block type accepted")
	}
	if err := appendBlockType(&block, BlockType{Kind: 99}); err == nil {
		t.Fatal("invalid block type accepted")
	}

	field := FieldType{Storage: StorageType{Val: I32}, Mut: Var}
	if !equalFieldType(field, field) || equalFieldType(field, FieldType{Storage: StorageType{Val: I64}, Mut: Var}) {
		t.Fatal("field type structural equality mismatch")
	}
	packed := StorageType{Packed: true, Pack: PackI8}
	if !equalStorageType(packed, packed) || equalStorageType(packed, StorageType{Packed: true, Pack: PackI16}) {
		t.Fatal("storage type structural equality mismatch")
	}
	ref := ValType{Kind: ValRef, Ref: Ref(true, IndexedHeap(TypeIdx{Index: 3}), false)}
	if !EqualValType(ref, ref) || EqualValType(ref, ValType{Kind: ValRef, Ref: Ref(false, IndexedHeap(TypeIdx{Index: 3}), false)}) {
		t.Fatal("reference value type structural equality mismatch")
	}
}
