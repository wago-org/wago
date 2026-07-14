package wasm

import "testing"

func TestModuleMetadataHelperCoverage(t *testing.T) {
	func0 := CompType{Kind: CompFunc, Params: []ValType{I32}, Results: []ValType{I64}}
	func1 := CompType{Kind: CompFunc, Params: []ValType{I32}, Results: []ValType{I64}}
	m := &Module{
		Types: []RecType{{SubTypes: []SubType{{Comp: func0}, {Comp: CompType{Kind: CompStruct}}}}, {SubTypes: []SubType{{Comp: func1}}}},
		Imports: []Import{
			{Type: ExternType{Kind: ExternFunc, Type: TypeIdx{Index: 0}}},
			{Type: ExternType{Kind: ExternTable, Table: TableType{Ref: FuncRef.Ref}}},
			{Type: ExternType{Kind: ExternGlobal, Global: GlobalType{Type: I64}}},
		},
		FuncTypes: []TypeIdx{{Index: 2}},
		Tables:    []Table{{Type: TableType{Ref: ExternRef.Ref, Limits: Limits{Addr64: true}}}},
		Globals:   []Global{{Type: GlobalType{Type: F32}}},
	}
	if got := m.flattenedTypeCount(); got != 3 {
		t.Fatalf("flattened types = %d", got)
	}
	if tt, ok := m.TableType(0); !ok || !EqualValType(RefVal(tt.Ref), FuncRef) {
		t.Fatalf("imported table = %#v, %v", tt, ok)
	}
	if tt, ok := m.TableType(1); !ok || !EqualValType(RefVal(tt.Ref), ExternRef) || TableAddrType(tt) != I64 {
		t.Fatalf("local table = %#v, %v", tt, ok)
	}
	if _, ok := m.TableType(2); ok || MemoryAddrType(MemType{}) != I32 || MemoryAddrType(MemType{Limits: Limits{Addr64: true}}) != I64 {
		t.Fatal("table/memory address helpers changed")
	}
	for _, idx := range []uint32{0, 1} {
		if ft, ok := m.FuncSignature(idx); !ok || ft.Kind != CompFunc {
			t.Fatalf("function signature %d = %#v, %v", idx, ft, ok)
		}
	}
	if _, ok := m.FuncSignature(2); ok || m.CanonicalTypeID(2) != 0 || m.StructuralTypeID(2) != m.StructuralTypeID(0) {
		t.Fatal("function signature IDs changed")
	}
	if _, ok := m.LocalFuncType(-1); ok {
		t.Fatal("negative local function index accepted")
	}
	if gt, ok := m.GlobalTypeByIndex(0); !ok || !EqualValType(gt.Type, I64) {
		t.Fatal("imported global type not found")
	}
	if gt, ok := m.GlobalTypeByIndex(1); !ok || !EqualValType(gt.Type, F32) {
		t.Fatal("local global type not found")
	}
	if _, ok := m.GlobalTypeByIndex(2); ok {
		t.Fatal("out-of-range global found")
	}
	if !FuncTypeEqual(&func0, &func1) || FuncTypeEqual(&func0, nil) || !IsNumericGlobalType(F64) || IsNumericGlobalType(FuncRef) {
		t.Fatal("function/global type helpers changed")
	}
	for _, tc := range []struct {
		t ValType
		b byte
	}{{I32, 0x7f}, {I64, 0x7e}, {F32, 0x7d}, {F64, 0x7c}, {V128, 0x7b}, {FuncRef, 0x70}, {ExternRef, 0x6f}} {
		if b, ok := EncodeValType(tc.t); !ok || b != tc.b || MustEncodeValType(tc.t) != tc.b {
			t.Fatalf("value type encoding %s = %#x, %v", tc.t, b, ok)
		}
	}
	if _, ok := EncodeValType(RefVal(Ref(true, IndexedHeap(TypeIdx{}), false))); ok {
		t.Fatal("indexed reference received a one-byte encoding")
	}
}

func TestElementExpressionAndDecodeErrors(t *testing.T) {
	for _, tc := range []struct {
		body []byte
		null bool
		idx  uint32
	}{
		{[]byte{0xd0, 0x70, 0x0b}, true, 0},
		{[]byte{0xd2, 0x81, 0x01, 0x0b}, false, 129},
	} {
		got, err := ParseElementExpr(Expr{BodyBytes: tc.body})
		if err != nil || got.Null != tc.null || got.FuncIndex != tc.idx {
			t.Fatalf("element expression %#v = %#v, %v", tc.body, got, err)
		}
	}
	if _, err := ParseFuncrefElementExpr(Expr{BodyBytes: []byte{0xd0, 0x6f, 0x0b}}); err == nil {
		t.Fatal("externref expression accepted as funcref")
	}
	if _, err := ParseElementExpr(Expr{BodyBytes: []byte{0xd2, 0x00, 0x0b, 0}}); err == nil {
		t.Fatal("trailing element-expression bytes accepted")
	}
	if (&DecodeError{Code: ErrBadMagic, Offset: 4}).Error() == "" || DecodeErrorCode(99).String() != "decode error 99" {
		t.Fatal("decode error formatting changed")
	}
}

func TestReaderExportedCursorHelpers(t *testing.T) {
	r := NewReader([]byte{1, 0x81, 1, 4, 3, 2, 1, 9})
	if b, ok := r.Peek(); !ok || b != 1 || r.Offset() != 0 {
		t.Fatalf("initial peek = %#x, %v at %d", b, ok, r.Offset())
	}
	if err := r.SkipU32N(2); err != nil || r.Offset() != 3 {
		t.Fatalf("skip u32s: offset %d, err %v", r.Offset(), err)
	}
	if got, err := r.LEU32(); err != nil || got != 0x01020304 {
		t.Fatalf("LEU32 = %#x, %v", got, err)
	}
	if got, err := r.Bytes(1); err != nil || len(got) != 1 || got[0] != 9 || r.HasNext() {
		t.Fatalf("Bytes = %x, %v, hasNext=%v", got, err, r.HasNext())
	}
	if _, ok := r.Peek(); ok {
		t.Fatal("peek succeeded at EOF")
	}
	if err := r.JumpTo(-1); err == nil || r.Step(1) == nil || r.BytesLeft() != 0 {
		t.Fatal("reader bounds checks changed")
	}
	if _, err := NewReader([]byte{1, 2, 3}).LEU32(); err == nil {
		t.Fatal("short LEU32 accepted")
	}
	if got, err := NewReader([]byte{8, 7, 6, 5, 4, 3, 2, 1}).LEU64(); err != nil || got != 0x0102030405060708 {
		t.Fatalf("LEU64 = %#x, %v", got, err)
	}
}
