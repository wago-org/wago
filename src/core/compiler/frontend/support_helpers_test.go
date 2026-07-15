package frontend

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestInstructionFamilyClassification(t *testing.T) {
	for _, k := range []wasm.InstrKind{wasm.InstrRefNull, wasm.InstrCallRef, wasm.InstrI31GetU} {
		if !isReferenceInstruction(k) {
			t.Fatalf("%s not classified as reference instruction", k)
		}
	}
	for _, k := range []wasm.InstrKind{wasm.InstrStructNew, wasm.InstrArrayLen, wasm.InstrRefGetDesc} {
		if !isGCInstruction(k) {
			t.Fatalf("%s not classified as GC instruction", k)
		}
	}
	if isReferenceInstruction(wasm.InstrI32Add) || isGCInstruction(wasm.InstrI32Add) {
		t.Fatal("ordinary numeric instruction classified as proposal instruction")
	}
}

func TestReferenceAndBlockSupportHelpers(t *testing.T) {
	for _, abs := range []wasm.AbsHeapType{wasm.HeapFunc, wasm.HeapExtern, wasm.HeapNoFunc, wasm.HeapNoExtern} {
		if !isNullableAbsRef(wasm.AbsRef(abs)) {
			t.Fatalf("%s null reference not accepted", abs)
		}
	}
	if isNullableAbsRef(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false)) ||
		isNullableAbsRef(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false)) {
		t.Fatal("non-bare nullable reference accepted")
	}
	if !isFuncRef(wasm.AbsRef(wasm.HeapFunc)) || !isExternRef(wasm.AbsRef(wasm.HeapExtern)) {
		t.Fatal("bare runtime references not recognized")
	}
	if isFuncRef(wasm.AbsRef(wasm.HeapExtern)) || isExternRef(wasm.AbsRef(wasm.HeapFunc)) {
		t.Fatal("reference family confused")
	}

	p := supportPass{}
	if err := p.blockType(wasm.BlockType{Kind: wasm.BlockVoid}, "test"); err != nil {
		t.Fatalf("void block: %v", err)
	}
	if err := p.blockType(wasm.BlockType{Kind: wasm.BlockTypeIndex}, "test"); err != nil {
		t.Fatalf("type-index block: %v", err)
	}
	if err := p.blockType(wasm.BlockType{Kind: 99}, "test"); err == nil {
		t.Fatal("invalid block type accepted")
	}
}

func TestReferenceAndTableMetadataScanners(t *testing.T) {
	if instrsUseRefFunc(nil) || exprUsesRefFunc(wasm.Expr{BodyBytes: []byte{0x0b}}) {
		t.Fatal("empty expressions unexpectedly use ref.func")
	}
	if !instrsUseRefFunc([]wasm.Instruction{{Kind: wasm.InstrRefFunc}}) ||
		!exprUsesRefFunc(wasm.Expr{BodyBytes: []byte{0xd2, 0x00, 0x0b}}) ||
		!exprUsesRefFunc(wasm.Expr{BodyBytes: []byte{0xff}}) {
		t.Fatal("ref.func or malformed byte expression was not detected")
	}
	if !RequiresFuncRefDescriptors(&wasm.Module{Globals: []wasm.Global{{Init: wasm.Expr{BodyBytes: []byte{0xd2, 0x00, 0x0b}}}}}) {
		t.Fatal("ref.func global did not require descriptors")
	}
	if RequiresFuncRefDescriptors(&wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapExtern)}}}}) {
		t.Fatal("externref-only module unexpectedly requires function descriptors")
	}

	if _, err := SupportedTableRuntimeShapes(nil); err == nil {
		t.Fatal("nil module accepted")
	}
	funcRef := wasm.AbsRef(wasm.HeapFunc)
	externRef := wasm.AbsRef(wasm.HeapExtern)
	max := uint64(9)
	m := &wasm.Module{
		Imports: []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternTable, Table: wasm.TableType{Ref: externRef}}}},
		Exports: []wasm.Export{{Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 2}}},
		Tables: []wasm.Table{
			{Type: wasm.TableType{Ref: funcRef, Limits: wasm.Limits{Min: 3, Max: &max}}},
			{Type: wasm.TableType{Ref: externRef, Limits: wasm.Limits{Min: 2}}},
		},
	}
	shapes, err := SupportedTableRuntimeShapes(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(shapes) != 3 || shapes[0].EntryBytes != 8 || shapes[1].Size != 3 || shapes[1].Capacity != 9 || shapes[2].Capacity != int(minOnlyExternrefTableGrowCapacity) {
		t.Fatalf("table shapes = %#v", shapes)
	}
	if _, _, _, err := SupportedTableRuntimeShape(m); err == nil {
		t.Fatal("single-table API accepted multiple tables")
	}
	if has, size, cap, err := SupportedTableRuntimeShape(&wasm.Module{Tables: m.Tables[:1]}); err != nil || !has || size != 3 || cap != 9 {
		t.Fatalf("single-table shape = %v, %d, %d, %v", has, size, cap, err)
	}
	overflow := uint64(maxInt()) + 1
	if _, err := SupportedTableRuntimeShapes(&wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: funcRef, Limits: wasm.Limits{Min: overflow}}}}}); err == nil {
		t.Fatal("overflowing table minimum accepted")
	}

	if !instrsRequireSIMD([]wasm.Instruction{{Kind: wasm.InstrV128Const}}) ||
		!exprRequiresSIMD(wasm.Expr{BodyBytes: []byte{0xfd, 0x00, 0x0b}}) ||
		valTypeRequiresSIMD(wasm.I32) || !valTypeRequiresSIMD(wasm.V128) {
		t.Fatal("SIMD scanners misclassified values or instructions")
	}
}

func TestModuleRequiresSIMDScansEveryModuleComponent(t *testing.T) {
	funcType := wasm.RecType{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}}
	fd := []byte{0xfd, 0x00, 0x0b}
	cases := []struct {
		name string
		m    *wasm.Module
	}{
		{"type", &wasm.Module{Types: []wasm.RecType{funcType}}},
		{"function import", &wasm.Module{Types: []wasm.RecType{funcType}, Imports: []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternFunc, Type: wasm.TypeIdx{Index: 0}}}}}},
		{"global import", &wasm.Module{Imports: []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternGlobal, Global: wasm.GlobalType{Type: wasm.V128}}}}}},
		{"global type", &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.V128}}}}},
		{"global initializer", &wasm.Module{Globals: []wasm.Global{{Init: wasm.Expr{BodyBytes: fd}}}}},
		{"table initializer", &wasm.Module{Tables: []wasm.Table{{Init: &wasm.Expr{BodyBytes: fd}}}}},
		{"element offset", &wasm.Module{Elements: []wasm.Elem{{Mode: wasm.ElemMode{Offset: wasm.Expr{BodyBytes: fd}}}}}},
		{"element expression", &wasm.Module{Elements: []wasm.Elem{{Kind: wasm.ElemKind{Exprs: []wasm.Expr{{BodyBytes: fd}}}}}}},
		{"data offset", &wasm.Module{Data: []wasm.Data{{Mode: wasm.DataMode{Offset: wasm.Expr{BodyBytes: fd}}}}}},
		{"local", &wasm.Module{Code: []wasm.Func{{Locals: wasm.Locals{Runs: []wasm.LocalRun{{Type: wasm.V128}}}}}}},
		{"body", &wasm.Module{Code: []wasm.Func{{BodyBytes: fd}}}},
	}
	if ModuleRequiresSIMD(nil) || ModuleRequiresSIMD(&wasm.Module{}) {
		t.Fatal("empty module requires SIMD")
	}
	for _, tc := range cases {
		if !ModuleRequiresSIMD(tc.m) {
			t.Errorf("%s did not require SIMD", tc.name)
		}
	}
}

func TestRuntimeFootprintSupportValidation(t *testing.T) {
	if err := (supportPass{m: &wasm.Module{}}).runtimeFootprint(); err != nil {
		t.Fatalf("empty module footprint: %v", err)
	}
	funcRef := wasm.AbsRef(wasm.HeapFunc)
	m := &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{{Kind: wasm.ValRef, Ref: funcRef}}}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Tables:    []wasm.Table{{Type: wasm.TableType{Ref: funcRef, Limits: wasm.Limits{Min: 2}}}},
		Elements:  []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0, 1}}}},
		Data:      []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataPassive}}},
	}
	if err := (supportPass{m: m}).runtimeFootprint(); err != nil {
		t.Fatalf("table/passive footprint: %v", err)
	}
	overflow := uint64(maxInt()) + 1
	if err := (supportPass{m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: funcRef, Limits: wasm.Limits{Min: overflow}}}}}}).runtimeFootprint(); err == nil {
		t.Fatal("overflowing table footprint accepted")
	}
}

func TestExpressionSupportASTAndByteForms(t *testing.T) {
	p := supportPass{feat: AllFeatures()}
	if err := p.expr(wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Add}, {Kind: wasm.InstrIf}}}, "ast"); err != nil {
		t.Fatalf("supported AST expression: %v", err)
	}
	if err := p.expr(wasm.Expr{BodyBytes: []byte{0x41, 0x00, 0x0b}}, "bytes"); err != nil {
		t.Fatalf("supported byte expression: %v", err)
	}
	if err := p.expr(wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrMemoryGrow, Index: 1}}}, "ast"); err == nil {
		t.Fatal("multi-memory AST instruction accepted")
	}
}

func TestFunctionSupportValidation(t *testing.T) {
	valid := supportPass{m: &wasm.Module{Code: []wasm.Func{
		{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Add}}}},
		{BodyBytes: []byte{0x41, 0x00, 0x0b}},
	}}, feat: AllFeatures()}
	if err := valid.funcs(); err != nil {
		t.Fatalf("valid functions: %v", err)
	}
	for _, tc := range []struct {
		name string
		pass supportPass
	}{
		{"simd local disabled", supportPass{m: &wasm.Module{Code: []wasm.Func{{Locals: wasm.Locals{Runs: []wasm.LocalRun{{Type: wasm.V128}}}}}}, feat: Features{}}},
		{"invalid byte body", supportPass{m: &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0xff}}}}, feat: AllFeatures()}},
	} {
		if err := tc.pass.funcs(); err == nil {
			t.Errorf("%s accepted", tc.name)
		}
	}
}

func TestSupportPresentationAndSIMDByteHelpers(t *testing.T) {
	if got := (&UnsupportedError{Category: "instruction", Feature: "gc"}).Error(); got != "unsupported instruction gc" {
		t.Fatalf("context-free UnsupportedError = %q", got)
	}
	if got := (&UnsupportedError{Category: "instruction", Feature: "gc", Context: "function 2"}).Error(); got != "unsupported instruction gc at function 2" {
		t.Fatalf("contextual UnsupportedError = %q", got)
	}
	for kind, want := range map[wasm.CompTypeKind]string{
		wasm.CompArray:        "array type",
		wasm.CompStruct:       "struct type",
		wasm.CompFunc:         "function type",
		wasm.CompTypeKind(99): "unknown type",
	} {
		if got := compTypeName(kind); got != want {
			t.Errorf("compTypeName(%d) = %q, want %q", kind, got, want)
		}
	}
	if elemModeName(wasm.ElemPassive) != "passive segment" || elemModeName(wasm.ElemActive) != "active segment" ||
		elemModeName(wasm.ElemDeclarative) != "declarative segment" || elemModeName(wasm.ElemModeKind(99)) != "unknown segment mode" ||
		elemKindName(wasm.ElemFuncs) != "function index segment" || elemKindName(wasm.ElemFuncExprs) != "function expression segment" ||
		elemKindName(wasm.ElemTypedExprs) != "typed expression segment" || elemKindName(wasm.ElemKindKind(99)) != "unknown segment kind" {
		t.Fatal("element support names changed")
	}
	if simdUnsupportedName(wasm.InstructionImmediate{Kind: wasm.InstrInvalid, Subopcode: 99}) != "0xFD opcode 99" ||
		simdUnsupportedName(wasm.InstructionImmediate{Kind: wasm.InstrV128Load}) != wasm.InstrV128Load.String() {
		t.Fatal("SIMD unsupported names changed")
	}
	if uses, ok := blockTypeBytesRequireSIMD(wasm.NewReader([]byte{0x7b})); !uses || !ok {
		t.Fatal("v128 block type not recognized")
	}
	if uses, ok := blockTypeBytesRequireSIMD(wasm.NewReader([]byte{0x40})); uses || !ok {
		t.Fatal("void block type not recognized")
	}
	if uses, ok := blockTypeBytesRequireSIMD(wasm.NewReader([]byte{0x70})); uses || !ok {
		t.Fatal("reference block type not recognized")
	}
	if uses, ok := blockTypeBytesRequireSIMD(wasm.NewReader([]byte{0x81, 0x00})); uses || !ok {
		t.Fatal("type-index block type not recognized")
	}
	if _, ok := blockTypeBytesRequireSIMD(wasm.NewReader([]byte{0x80})); ok {
		t.Fatal("truncated indexed block type accepted")
	}
	if _, ok := blockTypeBytesRequireSIMD(wasm.NewReader(nil)); ok {
		t.Fatal("truncated block type accepted")
	}
	if !bodyBytesUseTableGrow([]byte{0xfc, 0x0f, 0x00, 0x0b}) || bodyBytesUseTableGrow([]byte{0x0b}) || !bodyBytesUseTableGrow([]byte{0xff}) {
		t.Fatal("table.grow byte scanner changed")
	}
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"simd prefix", []byte{0xfd, 0x00, 0x0b}, true},
		{"v128 block", []byte{0x02, 0x7b, 0x0b}, true},
		{"scalar block", []byte{0x03, 0x7f, 0x0b}, false},
		{"typed select v128", []byte{0x1c, 0x01, 0x7b, 0x0b}, true},
		{"typed select scalar", []byte{0x1c, 0x01, 0x7f, 0x0b}, false},
		{"scalar immediate containing fd", []byte{0x41, 0xfd, 0x01, 0x0b}, false},
		{"truncated block", []byte{0x02}, false},
		{"unknown opcode", []byte{0xff}, false},
	} {
		if got := exprBytesRequireSIMD(tc.body); got != tc.want {
			t.Errorf("exprBytesRequireSIMD(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestElementExpressionSupportValidation(t *testing.T) {
	p := supportPass{}
	if err := p.elementExpr(wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}, "element expression"); err != nil {
		t.Fatalf("valid element expression: %v", err)
	}
	if err := p.elementExpr(wasm.Expr{BodyBytes: []byte{0x41, 0x00, 0x0b}}, "element expression"); err == nil {
		t.Fatal("non-reference element expression accepted")
	}
}

func TestElementSegmentSupportModesAndKinds(t *testing.T) {
	funcRef := wasm.AbsRef(wasm.HeapFunc)
	expr := wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}
	activeOffset := wasm.Expr{BodyBytes: []byte{0x41, 0x00, 0x0b}}
	valid := []wasm.Elem{
		{Mode: wasm.ElemMode{Kind: wasm.ElemActive, Offset: activeOffset}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}},
		{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}},
		{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}},
		{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncExprs, Exprs: []wasm.Expr{expr}}},
		{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: funcRef, Exprs: []wasm.Expr{expr}}},
	}
	if err := (supportPass{m: &wasm.Module{Elements: valid}, feat: AllFeatures()}).elements(); err != nil {
		t.Fatalf("valid element segments: %v", err)
	}
	for _, tc := range []struct {
		name string
		pass supportPass
	}{
		{"passive feature", supportPass{m: &wasm.Module{Elements: valid[1:2]}, feat: Features{}}},
		{"reference feature", supportPass{m: &wasm.Module{Elements: valid[3:4]}, feat: Features{BulkMemory: true}}},
		{"invalid typed reference", supportPass{m: &wasm.Module{Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false)}}}}, feat: AllFeatures()}},
		{"unknown mode", supportPass{m: &wasm.Module{Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: 99}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}}}}, feat: AllFeatures()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.pass.elements(); err == nil {
				t.Fatal("unsupported element segment accepted")
			}
		})
	}
}

func TestExportSupportValidation(t *testing.T) {
	funcRef := wasm.AbsRef(wasm.HeapFunc)
	externRef := wasm.AbsRef(wasm.HeapExtern)
	valid := &wasm.Module{
		Tables: []wasm.Table{{Type: wasm.TableType{Ref: funcRef}}, {Type: wasm.TableType{Ref: externRef}}},
		Exports: []wasm.Export{
			{Name: "fn", Index: wasm.ExternIdx{Kind: wasm.ExternFunc}},
			{Name: "global", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal}},
			{Name: "func-table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
			{Name: "memory", Index: wasm.ExternIdx{Kind: wasm.ExternMem}},
		},
	}
	if err := (supportPass{m: valid, feat: AllFeatures()}).exports(); err != nil {
		t.Fatalf("valid exports: %v", err)
	}
	for _, tc := range []struct {
		name string
		pass supportPass
	}{
		{"unknown table", supportPass{m: &wasm.Module{Exports: []wasm.Export{{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable}}}}, feat: AllFeatures()}},
		{"externref disabled", supportPass{m: &wasm.Module{Tables: valid.Tables[1:], Exports: []wasm.Export{{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable}}}}, feat: Features{}}},
		{"tag", supportPass{m: &wasm.Module{Exports: []wasm.Export{{Name: "tag", Index: wasm.ExternIdx{Kind: wasm.ExternTag}}}}, feat: AllFeatures()}},
		{"unknown kind", supportPass{m: &wasm.Module{Exports: []wasm.Export{{Name: "unknown", Index: wasm.ExternIdx{Kind: wasm.ExternKind(99)}}}}, feat: AllFeatures()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.pass.exports(); err == nil {
				t.Fatal("unsupported export accepted")
			}
		})
	}
}

func TestConstantExpressionSupportForms(t *testing.T) {
	all := supportPass{feat: AllFeatures()}
	v128 := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	v128 = append(v128, 0x0b)
	for _, body := range [][]byte{
		{0x23, 0x00, 0x0b},
		{0x41, 0x00, 0x41, 0x00, 0x6a, 0x0b},
		{0x41, 0x00, 0x0b},
		{0x42, 0x00, 0x0b},
		{0x43, 0, 0, 0, 0, 0x0b},
		{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b},
		{0xd0, 0x70, 0x0b},
		{0xd0, 0x6f, 0x0b},
		{0xd0, 0x73, 0x0b},
		{0xd0, 0x72, 0x0b},
		{0xd2, 0x00, 0x0b},
		v128,
	} {
		if err := all.constExpr(wasm.Expr{BodyBytes: body}, "constant"); err != nil {
			t.Fatalf("valid const expression %x: %v", body, err)
		}
	}
	for _, in := range []wasm.Instruction{
		{Kind: wasm.InstrI32Const},
		{Kind: wasm.InstrI64Const},
		{Kind: wasm.InstrF32Const},
		{Kind: wasm.InstrF64Const},
		{Kind: wasm.InstrGlobalGet},
		{Kind: wasm.InstrI32Add},
		{Kind: wasm.InstrRefFunc},
		{Kind: wasm.InstrV128Const},
	} {
		if err := all.constExpr(wasm.Expr{Instrs: []wasm.Instruction{in}}, "constant"); err != nil {
			t.Fatalf("valid instruction constant expression %s: %v", in.Kind, err)
		}
	}
	for _, tc := range []struct {
		name string
		pass supportPass
		expr wasm.Expr
	}{
		{"v128 disabled", supportPass{feat: Features{}}, wasm.Expr{BodyBytes: v128}},
		{"ref null disabled", supportPass{feat: Features{}}, wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}},
		{"ref func disabled", supportPass{feat: Features{}}, wasm.Expr{BodyBytes: []byte{0xd2, 0x00, 0x0b}}},
		{"invalid ref heap", all, wasm.Expr{BodyBytes: []byte{0xd0, 0x7f, 0x0b}}},
		{"unknown opcode", all, wasm.Expr{BodyBytes: []byte{0x01, 0x0b}}},
		{"instruction form disabled", supportPass{feat: Features{}}, wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrV128Const}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.pass.constExpr(tc.expr, "constant"); err == nil {
				t.Fatal("unsupported constant expression accepted")
			}
		})
	}
}

func TestGlobalSupportValidation(t *testing.T) {
	valid := &wasm.Module{Globals: []wasm.Global{
		{Type: wasm.GlobalType{Type: wasm.I32}, Init: wasm.Expr{BodyBytes: []byte{0x41, 0x00, 0x0b}}},
		{Type: wasm.GlobalType{Type: wasm.V128}, Init: wasm.Expr{BodyBytes: append(append([]byte{0xfd, 0x0c}, make([]byte, 16)...), 0x0b)}},
		{Type: wasm.GlobalType{Type: wasm.FuncRef}, Init: wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}},
	}}
	if err := (supportPass{m: valid, feat: AllFeatures()}).globals(); err != nil {
		t.Fatalf("valid globals: %v", err)
	}
	for _, tc := range []struct {
		name string
		pass supportPass
	}{
		{"simd disabled", supportPass{m: &wasm.Module{Globals: valid.Globals[1:2]}, feat: Features{}}},
		{"reference disabled", supportPass{m: &wasm.Module{Globals: valid.Globals[2:]}, feat: Features{}}},
		{"invalid type", supportPass{m: &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.ValType{Kind: wasm.ValBot}}, Init: wasm.Expr{BodyBytes: []byte{0x41, 0x00, 0x0b}}}}}, feat: AllFeatures()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.pass.globals(); err == nil {
				t.Fatal("unsupported global accepted")
			}
		})
	}
}

func TestMemorySupportValidation(t *testing.T) {
	valid := supportPass{m: &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 65535}}}}}
	if err := valid.memories(); err != nil {
		t.Fatalf("valid memory: %v", err)
	}
	for _, tc := range []struct {
		name string
		pass supportPass
	}{
		{"shared", supportPass{m: &wasm.Module{Memories: []wasm.MemType{{Shared: true}}}}},
		{"memory64", supportPass{m: &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Addr64: true}}}}}},
		{"oversized", supportPass{m: &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 65536}}}}}},
		{"multiple local", supportPass{m: &wasm.Module{Memories: []wasm.MemType{{}, {}}}}},
		{"import plus local", supportPass{m: &wasm.Module{Imports: []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternMem}}}, Memories: []wasm.MemType{{}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.pass.memories(); err == nil {
				t.Fatal("unsupported memory shape accepted")
			}
		})
	}
}

func TestTableSupportValidation(t *testing.T) {
	funcRef := wasm.AbsRef(wasm.HeapFunc)
	externRef := wasm.AbsRef(wasm.HeapExtern)
	init := wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}
	valid := supportPass{m: &wasm.Module{Tables: []wasm.Table{
		{Type: wasm.TableType{Ref: funcRef}},
		{Type: wasm.TableType{Ref: externRef}},
		{Type: wasm.TableType{Ref: funcRef}, Init: &init},
	}}, feat: AllFeatures()}
	if err := valid.tables(); err != nil {
		t.Fatalf("valid tables: %v", err)
	}
	for _, tc := range []struct {
		name string
		pass supportPass
	}{
		{"externref disabled", supportPass{m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: externRef}}}}, feat: Features{}}},
		{"invalid reference", supportPass{m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false)}}}}, feat: AllFeatures()}},
		{"address64", supportPass{m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: funcRef, Limits: wasm.Limits{Addr64: true}}}}}, feat: AllFeatures()}},
		{"initializer disabled", supportPass{m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: funcRef}, Init: &init}}}, feat: Features{}}},
		{"invalid initializer", supportPass{m: &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: funcRef}, Init: &wasm.Expr{BodyBytes: []byte{0x41, 0x00, 0x0b}}}}}, feat: AllFeatures()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.pass.tables(); err == nil {
				t.Fatal("unsupported table accepted")
			}
		})
	}
}

func TestProgrammaticInstructionSupportGates(t *testing.T) {
	full := supportPass{feat: AllFeatures()}
	for _, in := range []wasm.Instruction{
		{Kind: wasm.InstrI32Add},
		{Kind: wasm.InstrI32Extend8S},
		{Kind: wasm.InstrMemoryCopy},
		{Kind: wasm.InstrRefFunc},
		{Kind: wasm.InstrI32TruncSatF32S},
	} {
		if err := full.instr(in, "test"); err != nil {
			t.Fatalf("supported %s rejected: %v", in.Kind, err)
		}
	}

	disabled := supportPass{}
	for _, k := range []wasm.InstrKind{
		wasm.InstrI32Extend8S,
		wasm.InstrMemoryCopy,
		wasm.InstrRefFunc,
		wasm.InstrI32TruncSatF32S,
		wasm.InstrStructNew,
		wasm.InstrCallRef,
		wasm.InstrInvalid,
	} {
		if err := disabled.instr(wasm.Instruction{Kind: k}, "test"); err == nil {
			t.Fatalf("unsupported %s accepted", k)
		}
	}

	for _, in := range []wasm.Instruction{
		{Kind: wasm.InstrMemorySize, Index: 1},
		{Kind: wasm.InstrMemoryInit, Index2: 1},
		{Kind: wasm.InstrMemoryCopy, Index: 1},
		{Kind: wasm.InstrMemoryFill, Index: 1},
	} {
		if err := full.instr(in, "test"); err == nil {
			t.Fatalf("unsupported memory form %s accepted", in.Kind)
		}
	}
	if err := disabled.instr(wasm.Instruction{Kind: wasm.InstrCallIndirect, Index2: 1}, "test"); err == nil {
		t.Fatal("multi-table call_indirect accepted without reference types")
	}
}

func TestProgrammaticTableGrowAndConstExpressionSupport(t *testing.T) {
	if instrsUseTableGrow([]wasm.Instruction{{Kind: wasm.InstrI32Add}}) ||
		!instrsUseTableGrow([]wasm.Instruction{{Kind: wasm.InstrTableGrow}}) {
		t.Fatal("programmatic table.grow scanner changed")
	}

	full := supportPass{feat: AllFeatures()}
	for _, body := range [][]byte{
		{0x23, 0x00, 0x0b}, // global.get 0
		{0x41, 0x00, 0x0b}, // i32.const 0
		{0x42, 0x00, 0x0b}, // i64.const 0
		{0x43, 0, 0, 0, 0, 0x0b},
		{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b},
		{0xd0, 0x70, 0x0b}, // ref.null func
		{0xd0, 0x6f, 0x0b}, // ref.null extern
		{0xd0, 0x73, 0x0b}, // ref.null nofunc
		{0xd0, 0x72, 0x0b}, // ref.null noextern
		{0xd2, 0x00, 0x0b}, // ref.func 0
		append([]byte{0xfd, 0x0c}, append(make([]byte, 16), 0x0b)...),
	} {
		if err := full.constExpr(wasm.Expr{BodyBytes: body}, "test"); err != nil {
			t.Fatalf("supported const expression %x rejected: %v", body, err)
		}
	}

	for _, body := range [][]byte{
		{0xd0, 0x70, 0x0b},
		{0xd2, 0x00, 0x0b},
		append([]byte{0xfd, 0x0c}, append(make([]byte, 16), 0x0b)...),
	} {
		if err := (supportPass{}).constExpr(wasm.Expr{BodyBytes: body}, "test"); err == nil {
			t.Fatalf("disabled-feature const expression %x accepted", body)
		}
	}
	for _, body := range [][]byte{
		{0xd0, 0x7f, 0x0b},
		{0xfd, 0x00, 0x0b},
		{0x41, 0x00},
		{0x41, 0x00, 0x0b, 0x0b},
	} {
		if err := full.constExpr(wasm.Expr{BodyBytes: body}, "test"); err == nil {
			t.Fatalf("invalid const expression %x accepted", body)
		}
	}
}

func TestValueAndGlobalTypeSupportHelpers(t *testing.T) {
	base := supportPass{}
	for _, vt := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
		if err := base.valType(vt, "test"); err != nil {
			t.Fatalf("numeric value type %s rejected: %v", vt, err)
		}
		if err := base.globalType(vt, "test"); err != nil {
			t.Fatalf("numeric type %s rejected", vt)
		}
	}
	if err := base.valTypes([]wasm.ValType{wasm.I32, wasm.I64}, "test"); err != nil || !base.supportedValTypes([]wasm.ValType{wasm.F32}) {
		t.Fatal("numeric value-type helpers changed")
	}
	if err := base.valType(wasm.V128, "test"); err == nil || base.supportedValType(wasm.V128) {
		t.Fatal("SIMD type accepted without SIMD")
	}
	full := supportPass{feat: AllFeatures()}
	for _, vt := range []wasm.ValType{wasm.V128, wasm.FuncRef, wasm.ExternRef} {
		if err := full.valType(vt, "test"); err != nil {
			t.Fatalf("enabled value type %s rejected: %v", vt, err)
		}
		if err := full.globalType(vt, "test"); err != nil {
			t.Fatalf("enabled type %s rejected", vt)
		}
	}
	unsupportedRef := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapEq), false))
	if err := full.globalType(unsupportedRef, "test"); err == nil || valTypeName(unsupportedRef) == "" || refTypeName(unsupportedRef.Ref) == "" {
		t.Fatal("unsupported reference global accepted")
	}
	if err := base.globalType(wasm.FuncRef, "test"); err == nil || base.supportedValTypes([]wasm.ValType{wasm.FuncRef}) {
		t.Fatal("reference type accepted without reference-types")
	}
}
