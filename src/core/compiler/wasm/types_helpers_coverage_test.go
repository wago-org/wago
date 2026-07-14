package wasm

import "testing"

func TestInstructionPayloadAccessorsAndErrorFormatting(t *testing.T) {
	mem := MemIdx(2)
	lanes := [16]LaneIdx{3}
	in := Instruction{ext: &instrExt{
		BlockType: BlockType{Kind: BlockVal, Val: I32},
		Body:      Expr{Instrs: []Instruction{{Kind: InstrNop}}},
		Then:      []Instruction{{Kind: InstrI32Const}},
		Else:      []Instruction{{Kind: InstrI64Const}},
		Catches:   []Catch{{Kind: CatchAll}},
		Indices:   []uint32{1},
		ValTypes:  []ValType{I32},
		MemArg:    MemArg{Mem: &mem},
		Bytes:     []byte{1},
		Lanes:     lanes,
		RefType:   AbsRef(HeapExtern),
		HeapType:  AbsHeap(HeapFunc),
		HeapType2: IndexedHeap(TypeIdx{Index: 4}),
	}}
	if in.BlockType().Val != I32 || len(in.Body().Instrs) != 1 || len(in.Then()) != 1 || len(in.Else()) != 1 ||
		len(in.Catches()) != 1 || len(in.Indices()) != 1 || len(in.ValTypes()) != 1 || in.MemArg().Mem == nil ||
		len(in.Bytes()) != 1 || in.Lanes()[0] != 3 || !in.RefType().Nullable || in.HeapType().Abs != HeapFunc || in.HeapType2().Type.Index != 4 {
		t.Fatal("instruction payload accessors changed")
	}
	var zero Instruction
	if zero.BlockType().Kind != BlockVoid || len(zero.Body().Instrs) != 0 || zero.Then() != nil || zero.Else() != nil ||
		zero.Catches() != nil || zero.Indices() != nil || zero.ValTypes() != nil || zero.MemArg().Mem != nil || zero.Bytes() != nil ||
		zero.Lanes()[0] != 0 || zero.RefType().Nullable || zero.HeapType().Kind != HeapAbs || zero.HeapType2().Kind != HeapAbs {
		t.Fatal("zero instruction accessors changed")
	}
	if !AbsRef(HeapFunc).IsDefaultable() || Ref(false, AbsHeap(HeapFunc), false).IsDefaultable() {
		t.Fatal("reference defaultability changed")
	}

	for code := ErrIndexOutOfBounds; code <= ErrInvalidModule; code++ {
		if (&DecodeError{Code: code}).Error() == "" || code.String() == "" {
			t.Fatalf("decode error %d has empty formatting", code)
		}
	}
	if (&DecodeError{Code: ErrBadMagic, Offset: 2, SectionID: 1, SectionStart: 1, SectionEnd: 3}).Error() == "" || DecodeErrorCode(99).String() == "" {
		t.Fatal("decode error formatting changed")
	}
	for code := ErrTypeMismatch; code <= ErrUnsupportedFeature; code++ {
		if (&ValidationError{Code: code, Func: -1}).Error() == "" || code.String() == "" {
			t.Fatalf("validation error %d has empty formatting", code)
		}
	}
	if (&ValidationError{Code: ErrUnknownFunc, Func: 2, Detail: "detail"}).Error() == "" || ValidationErrorCode(99).String() == "" {
		t.Fatal("validation error formatting changed")
	}
}

func TestSIMDValidationInstructionKindsSnapshot(t *testing.T) {
	first := SIMDValidationInstructionKinds()
	if len(first) == 0 {
		t.Fatal("SIMD validation instruction set is empty")
	}
	if _, ok := first[InstrV128Const]; !ok {
		t.Fatal("v128.const missing from SIMD validation instruction set")
	}
	delete(first, InstrV128Const)
	if _, ok := SIMDValidationInstructionKinds()[InstrV128Const]; !ok {
		t.Fatal("SIMD validation instruction set was not copied")
	}
}

func TestDecodeCatchEncodings(t *testing.T) {
	for _, tc := range []struct {
		data       []byte
		kind       CatchKind
		tag, label uint32
		wantErr    bool
	}{
		{[]byte{0, 2, 3}, CatchTag, 2, 3, false},
		{[]byte{1, 4, 5}, CatchRef, 4, 5, false},
		{[]byte{2, 6}, CatchAll, 0, 6, false},
		{[]byte{3, 7}, CatchAllRef, 0, 7, false},
		{[]byte{0, 0x80}, 0, 0, 0, true},
		{[]byte{4}, 0, 0, 0, true},
	} {
		got, err := decodeCatch(newReader(tc.data))
		if (err != nil) != tc.wantErr {
			t.Fatalf("decodeCatch(%x) error = %v", tc.data, err)
		}
		if !tc.wantErr && (got.Kind != tc.kind || uint32(got.Tag) != tc.tag || uint32(got.Label) != tc.label) {
			t.Fatalf("decodeCatch(%x) = %#v", tc.data, got)
		}
	}
}

func TestDecodeTagType(t *testing.T) {
	if got, err := decodeTagType(newReader([]byte{0, 9})); err != nil || got.Type != (TypeIdx{Index: 9}) {
		t.Fatalf("decodeTagType(valid) = %#v, %v", got, err)
	}
	for _, data := range [][]byte{nil, {1}, {0, 0x80}} {
		if _, err := decodeTagType(newReader(data)); err == nil {
			t.Fatalf("decodeTagType(%x) accepted malformed input", data)
		}
	}
}

func TestDecodeNullableReferenceType(t *testing.T) {
	if got, err := decodeRefTypeForNull(newReader([]byte{0x70})); err != nil || !got.Nullable || got.Exact || got.Heap.Abs != HeapFunc {
		t.Fatalf("decodeRefTypeForNull(funcref) = %#v, %v", got, err)
	}
	if got, err := decodeRefTypeForNull(newReader([]byte{0x62, 3})); err != nil || !got.Nullable || !got.Exact || got.Heap.Type != (TypeIdx{Index: 3}) {
		t.Fatalf("decodeRefTypeForNull(exact indexed) = %#v, %v", got, err)
	}
	if _, err := decodeRefTypeForNull(newReader([]byte{0x62, 0x80})); err == nil {
		t.Fatal("truncated exact indexed nullable reference accepted")
	}
}

func TestDecodeExternTypeKinds(t *testing.T) {
	for _, tc := range []struct {
		data []byte
		kind ExternKind
	}{
		{[]byte{byte(ExternFunc), 2}, ExternFunc},
		{[]byte{byte(ExternTable), 0x70, 0, 1}, ExternTable},
		{[]byte{byte(ExternMem), 0, 1}, ExternMem},
		{[]byte{byte(ExternGlobal), 0x7f, 1}, ExternGlobal},
		{[]byte{byte(ExternTag), 0, 3}, ExternTag},
	} {
		got, err := decodeExternType(newReader(tc.data))
		if err != nil || got.Kind != tc.kind {
			t.Fatalf("decodeExternType(%x) = %#v, %v", tc.data, got, err)
		}
	}
	if _, err := decodeExternType(newReader([]byte{99})); err == nil {
		t.Fatal("invalid external type kind accepted")
	}
}

func TestDecodeLimitsEncodings(t *testing.T) {
	for _, tc := range []struct {
		data     []byte
		min, max uint64
		addr64   bool
		hasMax   bool
	}{
		{[]byte{0, 3}, 3, 0, false, false},
		{[]byte{1, 3, 9}, 3, 9, false, true},
		{[]byte{4, 3}, 3, 0, true, false},
		{[]byte{5, 3, 9}, 3, 9, true, true},
	} {
		got, err := decodeLimits(newReader(tc.data))
		if err != nil || got.Min != tc.min || got.Addr64 != tc.addr64 || (got.Max != nil) != tc.hasMax || tc.hasMax && *got.Max != tc.max {
			t.Fatalf("decodeLimits(%x) = %#v, %v", tc.data, got, err)
		}
	}
	for _, data := range [][]byte{nil, {2}, {1, 3}, {5, 3}} {
		if _, err := decodeLimits(newReader(data)); err == nil {
			t.Fatalf("decodeLimits(%x) accepted malformed input", data)
		}
	}
}

func TestDecodeMemoryTypeEncodings(t *testing.T) {
	for _, tc := range []struct {
		data                   []byte
		min, max               uint64
		shared, addr64, hasMax bool
	}{
		{[]byte{0, 3}, 3, 0, false, false, false},
		{[]byte{1, 3, 9}, 3, 9, false, false, true},
		{[]byte{3, 3, 9}, 3, 9, true, false, true},
		{[]byte{4, 3}, 3, 0, false, true, false},
		{[]byte{5, 3, 9}, 3, 9, false, true, true},
		{[]byte{7, 3, 9}, 3, 9, true, true, true},
	} {
		got, err := decodeMemType(newReader(tc.data))
		if err != nil || got.Limits.Min != tc.min || got.Shared != tc.shared || got.Limits.Addr64 != tc.addr64 || (got.Limits.Max != nil) != tc.hasMax || tc.hasMax && *got.Limits.Max != tc.max {
			t.Fatalf("decodeMemType(%x) = %#v, %v", tc.data, got, err)
		}
	}
	for _, data := range [][]byte{nil, {2, 3}, {6, 3}, {8}} {
		if _, err := decodeMemType(newReader(data)); err == nil {
			t.Fatalf("decodeMemType(%x) accepted malformed input", data)
		}
	}
}

func TestDecodeTableAndGlobalTypes(t *testing.T) {
	if table, err := decodeTableType(newReader([]byte{0x70, 0, 2})); err != nil || table.Ref != AbsRef(HeapFunc) || table.Limits.Min != 2 {
		t.Fatalf("decodeTableType = %#v, %v", table, err)
	}
	for _, data := range [][]byte{{0xff}, {0x70, 2}} {
		if _, err := decodeTableType(newReader(data)); err == nil {
			t.Fatalf("decodeTableType(%x) accepted malformed input", data)
		}
	}
	if global, err := decodeGlobalType(newReader([]byte{0x7e, 1})); err != nil || global.Type != I64 || !global.Mutable {
		t.Fatalf("decodeGlobalType = %#v, %v", global, err)
	}
	for _, data := range [][]byte{{0xff}, {0x7f, 2}} {
		if _, err := decodeGlobalType(newReader(data)); err == nil {
			t.Fatalf("decodeGlobalType(%x) accepted malformed input", data)
		}
	}
}

func TestDecodeNumericAndAbstractHeapTypes(t *testing.T) {
	for _, want := range []NumType{NumI32, NumI64, NumF32, NumF64} {
		if got, err := decodeNumType(newReader([]byte{byte(want)})); err != nil || got != want {
			t.Fatalf("decodeNumType(%#x) = %#x, %v", want, got, err)
		}
	}
	for _, want := range append([]byte{0x64}, func() []byte {
		out := make([]byte, 0, 0x74-0x69+1)
		for b := byte(0x69); b <= 0x74; b++ {
			out = append(out, b)
		}
		return out
	}()...) {
		if got, err := decodeAbsHeapType(newReader([]byte{want})); err != nil || byte(got) != want {
			t.Fatalf("decodeAbsHeapType(%#x) = %#x, %v", want, got, err)
		}
	}
	for _, data := range [][]byte{nil, {0x70}} {
		if _, err := decodeNumType(newReader(data)); err == nil {
			t.Fatalf("decodeNumType(%x) accepted malformed input", data)
		}
	}
	for _, data := range [][]byte{nil, {0x68}} {
		if _, err := decodeAbsHeapType(newReader(data)); err == nil {
			t.Fatalf("decodeAbsHeapType(%x) accepted malformed input", data)
		}
	}
}

func TestStructuralTypeEqualityHelpers(t *testing.T) {
	fun := CompType{Kind: CompFunc, Params: []ValType{I32}, Results: []ValType{I64}}
	if !equalCompType(fun, fun) || equalCompType(fun, CompType{Kind: CompFunc, Params: []ValType{I64}, Results: []ValType{I64}}) {
		t.Fatal("function type equality changed")
	}
	field := FieldType{Storage: StorageType{Val: I32}, Mut: Var}
	strct := CompType{Kind: CompStruct, Fields: []FieldType{field}}
	if !equalCompType(strct, strct) || equalCompType(strct, CompType{Kind: CompStruct, Fields: []FieldType{{Storage: StorageType{Val: I32}, Mut: Const}}}) {
		t.Fatal("struct type equality changed")
	}
	array := CompType{Kind: CompArray, Array: FieldType{Storage: StorageType{Packed: true, Pack: PackI8}}}
	if !equalCompType(array, array) || equalCompType(array, CompType{Kind: CompArray, Array: FieldType{Storage: StorageType{Packed: true, Pack: PackI16}}}) {
		t.Fatal("array type equality changed")
	}
	if EqualValType(RefVal(AbsRef(HeapFunc)), RefVal(AbsRef(HeapExtern))) || !EqualValType(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 2}), true)), RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 2}), true))) {
		t.Fatal("reference value type equality changed")
	}
	def := &DefType{GroupIndex: 1, Index: 2}
	if !equalHeapType(HeapType{Kind: HeapDefType, Def: def}, HeapType{Kind: HeapDefType, Def: &DefType{GroupIndex: 1, Index: 2}}) || equalHeapType(HeapType{Kind: HeapDefType, Def: def}, HeapType{Kind: HeapDefType}) {
		t.Fatal("defined heap type equality changed")
	}
}

func TestDecodeSignedTypeIndex(t *testing.T) {
	if got, err := decodeS33TypeIdx(newReader([]byte{5})); err != nil || got != (TypeIdx{Index: 5}) {
		t.Fatalf("decodeS33TypeIdx(valid) = %#v, %v", got, err)
	}
	for _, data := range [][]byte{{0x7f}, {0x80}} {
		if _, err := decodeS33TypeIdx(newReader(data)); err == nil {
			t.Fatalf("decodeS33TypeIdx(%x) accepted malformed input", data)
		}
	}
}

func TestDecodeReferenceTypeEncodings(t *testing.T) {
	if got, err := decodeRefType(newReader([]byte{0x63, 0x70})); err != nil || got != AbsRef(HeapFunc) {
		t.Fatalf("decodeRefType(nullable funcref) = %#v, %v", got, err)
	}
	if got, err := decodeRefType(newReader([]byte{0x64, 0x70})); err != nil || got.Nullable || got.Heap.Abs != HeapFunc {
		t.Fatalf("decodeRefType(non-null funcref) = %#v, %v", got, err)
	}
	if got, err := decodeRefType(newReader([]byte{0x63, 0x62, 4})); err != nil || !got.Nullable || !got.Exact || got.Heap.Type != (TypeIdx{Index: 4}) {
		t.Fatalf("decodeRefType(exact index) = %#v, %v", got, err)
	}
	if got, err := decodeRefType(newReader([]byte{0x6f})); err != nil || got != AbsRef(HeapExtern) {
		t.Fatalf("decodeRefType(shorthand externref) = %#v, %v", got, err)
	}
	for _, data := range [][]byte{nil, {0x63}, {0xff}} {
		if _, err := decodeRefType(newReader(data)); err == nil {
			t.Fatalf("decodeRefType(%x) accepted malformed input", data)
		}
	}
}

func TestDecodeStorageAndFieldTypes(t *testing.T) {
	for _, tc := range []struct {
		data   []byte
		packed bool
		pack   PackType
		val    ValType
	}{
		{[]byte{0x77}, true, PackI16, ValType{}},
		{[]byte{0x78}, true, PackI8, ValType{}},
		{[]byte{0x7f}, false, 0, I32},
	} {
		got, err := decodeStorageType(newReader(tc.data))
		if err != nil || got.Packed != tc.packed || got.Pack != tc.pack || got.Val != tc.val {
			t.Fatalf("decodeStorageType(%x) = %#v, %v", tc.data, got, err)
		}
	}
	for _, data := range [][]byte{nil, {0xff}} {
		if _, err := decodeStorageType(newReader(data)); err == nil {
			t.Fatalf("decodeStorageType(%x) accepted malformed input", data)
		}
	}
	if got, err := decodeFieldType(newReader([]byte{0x77, 1})); err != nil || !got.Storage.Packed || got.Mut != Var {
		t.Fatalf("decodeFieldType(packed) = %#v, %v", got, err)
	}
	if got, err := decodeFieldType(newReader([]byte{0x7e, 0})); err != nil || got.Storage.Val != I64 || got.Mut != Const {
		t.Fatalf("decodeFieldType(value) = %#v, %v", got, err)
	}
	if _, err := decodeFieldType(newReader([]byte{0x7f, 2})); err == nil {
		t.Fatal("decodeFieldType accepted invalid mutability")
	}
}

func TestRecursiveTypeIndexMarkingIncludesStructAndArrayFields(t *testing.T) {
	ref := func(index uint32) ValType {
		return ValType{Kind: ValRef, Ref: Ref(true, IndexedHeap(TypeIdx{Index: index}), false)}
	}
	types := []RecType{
		{SubTypes: []SubType{
			{Comp: CompType{Kind: CompStruct, Fields: []FieldType{{Storage: StorageType{Val: ref(1)}}, {Storage: StorageType{Packed: true, Pack: PackI8}}}}},
			{Comp: CompType{Kind: CompArray, Array: FieldType{Storage: StorageType{Val: ref(0)}}}},
		}},
		{SubTypes: []SubType{{Comp: CompType{Kind: CompFunc, Params: []ValType{ref(0)}}}}},
	}
	markRecursiveTypeIndexes(types)
	structRef := types[0].SubTypes[0].Comp.Fields[0].Storage.Val.Ref.Heap.Type
	arrayRef := types[0].SubTypes[1].Comp.Array.Storage.Val.Ref.Heap.Type
	outerRef := types[1].SubTypes[0].Comp.Params[0].Ref.Heap.Type
	if !structRef.Rec || structRef.Index != 1 || !arrayRef.Rec || arrayRef.Index != 0 || outerRef.Rec || outerRef.Index != 0 {
		t.Fatalf("recursive type indexes = struct=%#v array=%#v outer=%#v", structRef, arrayRef, outerRef)
	}
	m := &Module{Types: types}
	resolvedStruct := m.resolveCompTypeRecIndexes(types[0].SubTypes[0].Comp, 0)
	resolvedArray := m.resolveCompTypeRecIndexes(types[0].SubTypes[1].Comp, 0)
	if got := resolvedStruct.Fields[0].Storage.Val.Ref.Heap.Type; got.Rec || got.Index != 1 {
		t.Fatalf("resolved struct field index = %#v", got)
	}
	if got := resolvedArray.Array.Storage.Val.Ref.Heap.Type; got.Rec || got.Index != 0 {
		t.Fatalf("resolved array field index = %#v", got)
	}
	if !resolvedStruct.Fields[1].Storage.Packed {
		t.Fatal("packed struct field was changed while resolving indexes")
	}
	if got := m.resolveValTypeRecIndexes(I32, 0); got != I32 {
		t.Fatalf("scalar value type changed while resolving indexes: %#v", got)
	}
	if got := m.resolveRefTypeRecIndexes(AbsRef(HeapFunc), 0); got != AbsRef(HeapFunc) {
		t.Fatalf("abstract reference changed while resolving indexes: %#v", got)
	}
	invalid := TypeIdx{Index: 9, Rec: true}
	if got := m.resolveTypeIdxRecIndex(invalid, 0); got != invalid {
		t.Fatalf("invalid recursive index changed: %#v", got)
	}
}

func TestBytecodeSkipClassificationHelpers(t *testing.T) {
	if err := skipResultTypeBytes(newReader([]byte{2, 0x7f, 0x7e})); err != nil {
		t.Fatalf("skip result types: %v", err)
	}
	if err := skipResultTypeBytes(newReader([]byte{1})); err == nil {
		t.Fatal("truncated result type accepted")
	}
	if err := skipValTypeBytes(newReader([]byte{0x7f})); err != nil {
		t.Fatalf("skip value type: %v", err)
	}
	if err := skipRefHeapTypeBytes(newReader([]byte{0x70})); err != nil {
		t.Fatalf("skip reference heap type: %v", err)
	}
	for sub, want := range map[uint32]InstrKind{0: InstrStructNew, 1: InstrStructNewDefault, 6: InstrArrayNew, 7: InstrArrayNewDefault, 11: InstrArrayGet, 12: InstrArrayGetS, 13: InstrArrayGetU, 14: InstrArraySet, 16: InstrArrayFill, 32: InstrStructNewDesc, 33: InstrStructNewDefaultDesc, 34: InstrRefGetDesc, 0x82: InstrStringConst} {
		if got := fbOneIndexKind(sub); got != want {
			t.Fatalf("fb one-index %#x = %s, want %s", sub, got, want)
		}
	}
	for sub, want := range map[uint32]InstrKind{2: InstrStructGet, 3: InstrStructGetS, 4: InstrStructGetU, 5: InstrStructSet, 8: InstrArrayNewFixed, 9: InstrArrayNewData, 10: InstrArrayNewElem, 17: InstrArrayCopy, 18: InstrArrayInitData, 19: InstrArrayInitElem} {
		if got := fbTwoIndexKind(sub); got != want {
			t.Fatalf("fb two-index %#x = %s, want %s", sub, got, want)
		}
	}
	for sub, want := range map[uint32]InstrKind{0xb0: InstrStringNewUtf8Array, 0xb1: InstrStringNewWtf16Array, 0xb2: InstrStringEncodeUtf8Array, 0xb3: InstrStringEncodeWtf16Array, 0xb4: InstrStringNewLossyUtf8Array, 0xb5: InstrStringNewWtf8Array, 0xb6: InstrStringEncodeLossyUtf8Array, 0xb7: InstrStringEncodeWtf8Array} {
		if got := fbStringArrayKind(sub); got != want {
			t.Fatalf("fb string-array %#x = %s, want %s", sub, got, want)
		}
	}
	if fbOneIndexKind(99) != InstrInvalid || fbTwoIndexKind(99) != InstrInvalid || fbStringArrayKind(99) != InstrInvalid {
		t.Fatal("unknown fb classification changed")
	}
}
