package wasm

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func editTestModule(bodies ...[]byte) *Module {
	m := &Module{Types: []RecType{{SubTypes: []SubType{{Comp: CompType{Kind: CompFunc, Results: []ValType{I32}}}}}}, FuncTypes: make([]TypeIdx, len(bodies)), Code: make([]Func, len(bodies))}
	for i, body := range bodies {
		m.Code[i].BodyBytes = bytes.Clone(body)
	}
	return m
}
func editOracle(t *testing.T, m *Module, edits []BodyEdit, f ValidationFeatures, l ValidationLimits) (*Module, ValidatedModuleAnalysis, error) {
	t.Helper()
	out := cloneBodyEditModule(m)
	for _, e := range edits {
		out.Code[e.LocalFunction] = Func{BodyBytes: bytes.Clone(e.BodyBytes), Locals: Locals{Runs: cloneBodySlice(e.Locals.Runs)}}
	}
	var a ValidatedModuleAnalysis
	err := ValidateModuleWithAnalysis(out, f, 1, l, &a)
	return out, a, err
}
func assertEditParity(t *testing.T, m *Module, edits []BodyEdit, f ValidationFeatures, l ValidationLimits) {
	t.Helper()
	owner, err := NewOwnedBodySource(m, ValidationFeatures{}, 1, ValidationLimits{})
	if err != nil {
		t.Fatal(err)
	}
	want, wa, we := editOracle(t, m, edits, f, l)
	got, ga, ge := owner.Apply(edits, f, 1, l)
	if (we == nil) != (ge == nil) {
		t.Fatalf("full=%v edited=%v", we, ge)
	}
	if we != nil {
		var x, y *ValidationError
		if errors.As(we, &x) {
			if !errors.As(ge, &y) || x.Code != y.Code || x.Func != y.Func {
				t.Fatalf("error full=%v edited=%v", we, ge)
			}
		} else if reflect.TypeOf(we) != reflect.TypeOf(ge) || we.Error() != ge.Error() {
			t.Fatalf("decode full=%v edited=%v", we, ge)
		}
		if ga.valid {
			t.Fatal("partial valid facts after failure")
		}
		return
	}
	if wa.ValidFor(want) != ga.ValidFor(got) || !reflect.DeepEqual(wa.funcs, ga.funcs) || wa.flags != ga.flags || wa.dataStateCount != ga.dataStateCount || wa.elemStateCount != ga.elemStateCount {
		t.Fatalf("facts full=%+v edited=%+v", wa, ga)
	}
	if ga.ValidFor(m) {
		t.Fatal("fresh facts accepted original identity")
	}
}
func TestOwnedBodyEditFactsAndErrors(t *testing.T) {
	for _, body := range [][]byte{{0x41, 9, 0xb}, {0x02, 0x7f, 0x41, 3, 0xb, 0xb}, {0x20, 8, 0xb}, {0x41, 1}, {0x42, 1, 0xb}, {0x41, 1, 0xb, 0x01}} {
		assertEditParity(t, editTestModule([]byte{0x41, 7, 0xb}, []byte{0x41, 8, 0xb}), []BodyEdit{{LocalFunction: 0, BodyBytes: body}}, ValidationFeatures{}, ValidationLimits{})
	}
	m := editTestModule([]byte{0x02, 0x7f, 0x41, 1, 0xb, 0xb}, []byte{0x41, 8, 0xb})
	assertEditParity(t, m, []BodyEdit{{BodyBytes: []byte{0x41, 2, 0xb}}}, ValidationFeatures{}, ValidationLimits{})
	m = editTestModule([]byte{0x41, 7, 0xb}, []byte{0x41, 8, 0xb})
	m.Imports = []Import{{Module: "x", Name: "f", Type: NewFuncExternType(TypeIdx{Index: 0})}}
	assertEditParity(t, m, []BodyEdit{{LocalFunction: 1, BodyBytes: []byte{0x20, 9, 0xb}}, {LocalFunction: 0, BodyBytes: []byte{0x20, 8, 0xb}}}, ValidationFeatures{}, ValidationLimits{})
}
func TestOwnedBodyEditLocalsAndPolicy(t *testing.T) {
	m := editTestModule([]byte{0x41, 1, 0xb}, []byte{0x20, 0, 0xb})
	m.Code[1].Locals.Runs = []LocalRun{{Count: 2, Type: I32}}
	for _, limit := range []uint32{0, DefaultMaxFunctionLocals, 1, 3} {
		assertEditParity(t, m, []BodyEdit{{BodyBytes: []byte{0x20, 0, 0xb}, Locals: Locals{Runs: []LocalRun{{Count: 1, Type: I32}}}}}, ValidationFeatures{}, ValidationLimits{MaxFunctionLocals: limit})
	}
	assertEditParity(t, m, []BodyEdit{{BodyBytes: []byte{0x20, 0, 0xb}}}, ValidationFeatures{}, ValidationLimits{})
	assertEditParity(t, m, []BodyEdit{{BodyBytes: []byte{0x41, 3, 0xb}}}, ValidationFeatures{MultiMemory: true}, ValidationLimits{})
}
func TestOwnedBodyEditSegmentParity(t *testing.T) {
	drop := func(prefix, index byte) []byte { return []byte{0xfc, prefix, index, 0x41, 0, 0xb} }
	for _, kind := range []string{"data", "elem"} {
		for _, which := range []struct{ old0, old1, new0 byte }{{7, 3, 1}, {3, 7, 1}, {7, 3, 7}} {
			prefix := byte(9)
			if kind == "elem" {
				prefix = 13
			}
			m := editTestModule(drop(prefix, which.old0), drop(prefix, which.old1))
			if kind == "data" {
				count := uint32(8)
				m.DataCount = &count
				m.Data = make([]Data, 8)
				for i := range m.Data {
					m.Data[i].Mode.Kind = DataPassive
				}
			} else {
				m.Elements = make([]Elem, 8)
				for i := range m.Elements {
					m.Elements[i].Mode.Kind = ElemPassive
				}
			}
			assertEditParity(t, m, []BodyEdit{{BodyBytes: drop(prefix, which.new0)}}, ValidationFeatures{}, ValidationLimits{})
		}
	}
}
func TestOwnedBodyEditOwnership(t *testing.T) {
	m := editTestModule([]byte{0x41, 7, 0xb}, []byte{0x41, 8, 0xb})
	owner, err := NewOwnedBodySource(m, ValidationFeatures{}, 1, ValidationLimits{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, analysis, err := owner.Snapshot()
	if err != nil || !analysis.ValidFor(snapshot) {
		t.Fatal("snapshot facts", err)
	}
	m.Code[0].BodyBytes[0] = 0xff
	m.Types[0].SubTypes[0].Comp.Results[0] = I64
	snapshot.Code[0].BodyBytes[0] = 0xfe
	snapshot.Types[0].SubTypes[0].Comp.Results[0] = F64
	body := []byte{0x20, 0, 0xb}
	runs := []LocalRun{{Count: 1, Type: I32}}
	out, a, err := owner.Apply([]BodyEdit{{LocalFunction: 1, BodyBytes: body, Locals: Locals{Runs: runs}}}, ValidationFeatures{}, 1, ValidationLimits{})
	if err != nil || !a.ValidFor(out) {
		t.Fatal(err)
	}
	body[0] = 0xff
	runs[0].Type = F64
	if out.Code[0].BodyBytes[0] != 0x41 || out.Types[0].SubTypes[0].Comp.Results[0] != I32 || out.Code[1].BodyBytes[0] != 0x20 || out.Code[1].Locals.Runs[0].Type != I32 || out.Code[1].LocalDeclBytes != 3 {
		t.Fatal("caller storage aliases output")
	}
	if _, _, err := owner.Apply(nil, ValidationFeatures{}, 1, ValidationLimits{}); !errors.Is(err, ErrBodySourceConsumed) {
		t.Fatal("owner reused", err)
	}
	if _, _, err := owner.Snapshot(); !errors.Is(err, ErrBodySourceConsumed) {
		t.Fatal("snapshot after consumption", err)
	}
}

func TestApplyValidatedBodyEditsBorrowsPrivateStorage(t *testing.T) {
	m := editTestModule([]byte{0x41, 7, 0xb}, []byte{0x41, 8, 0xb})
	var sourceAnalysis ValidatedModuleAnalysis
	if err := ValidateModuleWithAnalysis(m, ValidationFeatures{}, 1, ValidationLimits{}, &sourceAnalysis); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplyValidatedBodyEdits(m, &ValidatedModuleAnalysis{}, []BodyEdit{{BodyBytes: []byte{0x41, 9, 0xb}}}, ValidationFeatures{}, 1, ValidationLimits{}); !errors.Is(err, ErrInvalidBodyEdit) {
		t.Fatalf("invalid analysis error = %v", err)
	}
	if _, _, err := ApplyValidatedBodyEdits(m, &sourceAnalysis, []BodyEdit{{BodyBytes: []byte{0xff}}}, ValidationFeatures{}, 1, ValidationLimits{}); err == nil {
		t.Fatal("invalid borrowed edit accepted")
	}
	body := []byte{0x20, 0, 0xb}
	runs := []LocalRun{{Count: 1, Type: I32}}
	out, analysis, err := ApplyValidatedBodyEdits(m, &sourceAnalysis, []BodyEdit{{LocalFunction: 1, BodyBytes: body, Locals: Locals{Runs: runs}}}, ValidationFeatures{}, 1, ValidationLimits{})
	if err != nil || !analysis.ValidFor(out) {
		t.Fatal("borrowed edit", err)
	}
	if &out.Code[0].BodyBytes[0] != &m.Code[0].BodyBytes[0] || &out.Code[1].BodyBytes[0] != &body[0] || &out.Code[1].Locals.Runs[0] != &runs[0] {
		t.Fatal("borrowed storage was copied")
	}
	if m.Code[1].BodyBytes[0] != 0x41 || len(m.Code[1].Locals.Runs) != 0 || out.Code[1].LocalDeclBytes != 3 {
		t.Fatal("source changed or edited local declaration missing")
	}
}
func TestOwnedBodyEditRejectsShapeAndHints(t *testing.T) {
	for _, edits := range [][]BodyEdit{{{LocalFunction: 1, BodyBytes: []byte{0xb}}}, {{BodyBytes: []byte{0xb}}, {BodyBytes: []byte{0xb}}}, {{}}} {
		owner, err := NewOwnedBodySource(editTestModule([]byte{0x41, 1, 0xb}), ValidationFeatures{}, 1, ValidationLimits{})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := owner.Apply(edits, ValidationFeatures{}, 1, ValidationLimits{}); !errors.Is(err, ErrInvalidBodyEdit) {
			t.Fatal("invalid shape", err)
		}
	}
	for _, raw := range []bool{false, true} {
		m := editTestModule([]byte{0x41, 1, 0xb})
		if raw {
			m.Customs = []CustomSec{{Name: "metadata.code.branch_hint", Data: []byte{0}}}
		} else {
			m.BranchHints = []FuncBranchHints{{FuncIndex: 0, Hints: []BranchHint{{Offset: 0}}}}
		}
		owner, err := NewOwnedBodySource(m, ValidationFeatures{}, 1, ValidationLimits{})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = owner.Apply([]BodyEdit{{BodyBytes: []byte{0x41, 1, 0xb}}}, ValidationFeatures{}, 1, ValidationLimits{}); !errors.Is(err, ErrBodyEditHints) {
			t.Fatal("hints accepted", err)
		}
	}
	if _, err := NewOwnedBodySource(editTestModule([]byte{0xff}), ValidationFeatures{}, 1, ValidationLimits{}); err == nil {
		t.Fatal("invalid source accepted")
	}
}

func TestOwnedBodyEditDefinedLocal(t *testing.T) {
	for _, changedPolicy := range []bool{false, true} {
		m := editTestModule([]byte{0x41, 1, 0xb})
		local := RefVal(Ref(true, DefinedHeap(&DefType{Rec: m.Types[0], GroupIndex: 0, Index: 0}), false))
		features := ValidationFeatures{}
		if changedPolicy {
			features.MultiMemory = true
		}
		assertEditParity(t, m, []BodyEdit{{BodyBytes: []byte{0x41, 2, 0xb}, Locals: Locals{Runs: []LocalRun{{Count: 1, Type: local}}}}}, features, ValidationLimits{})
	}
}
func TestOwnedBodyEditAscendingErrorOrder(t *testing.T) {
	m := editTestModule([]byte{0x41, 1, 0xb}, []byte{0x41, 2, 0xb})
	assertEditParity(t, m, []BodyEdit{{LocalFunction: 1, BodyBytes: []byte{0x41, 3, 0xb}, Locals: Locals{Runs: []LocalRun{{Count: 1, Type: ValType{lo: 7}}}}}, {LocalFunction: 0, BodyBytes: []byte{0xff}}}, ValidationFeatures{}, ValidationLimits{})
}

func TestOwnedBodyEditTreeAndGenericGuards(t *testing.T) {
	m := editTestModule([]byte{0x41, 1, 0xb}, []byte{0x41, 2, 0xb})
	m.Code[0].BodyBytes = nil
	m.Code[0].Body = Expr{Instrs: []Instruction{{Kind: InstrI32Const, I32: 1}}}
	assertEditParity(t, m, []BodyEdit{{LocalFunction: 1, BodyBytes: []byte{0x41, 4, 0xb}}}, ValidationFeatures{}, ValidationLimits{})
	m = editTestModule([]byte{0x41, 1, 0xb})
	local := RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 1}), false))
	m.Types = append(m.Types, RecType{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompStruct}}}})
	assertEditParity(t, m, []BodyEdit{{BodyBytes: []byte{0x20, 0, 0x1a, 0x41, 1, 0xb}, Locals: Locals{Runs: []LocalRun{{Count: 1, Type: local}}}}}, ValidationFeatures{}, ValidationLimits{})
	// An undeclared ref.func remains rejected on the edited body.
	assertEditParity(t, editTestModule([]byte{0x41, 1, 0xb}, []byte{0x41, 2, 0xb}), []BodyEdit{{BodyBytes: []byte{0xd2, 1, 0x1a, 0x41, 0, 0xb}}}, ValidationFeatures{}, ValidationLimits{})
	// Recursive declaration context is rebuilt even though no type is edited.
	m = editTestModule([]byte{0x41, 1, 0xb})
	ref := RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 0, Rec: true}), false))
	m.Types = append(m.Types, RecType{SubTypes: []SubType{{Comp: CompType{Kind: CompStruct, Fields: []FieldType{NewFieldType(StorageVal(ref), Const)}}}}})
	assertEditParity(t, m, []BodyEdit{{BodyBytes: []byte{0x41, 3, 0xb}}}, ValidationFeatures{}, ValidationLimits{})
}
func TestOwnedBodyEditPreservesCachesAndNestedStorage(t *testing.T) {
	m := editTestModule([]byte{0x41, 1, 0xb})
	if _, ok := m.structuralIndexedFuncTypeKey(0); !ok {
		t.Fatal("cache fixture")
	}
	cache := m.structuralTypeCache
	count := len(cache.keys)
	types := m.typeIndex()
	imports := m.importIndex()
	owner, err := NewOwnedBodySource(m, ValidationFeatures{}, 1, ValidationLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = owner.Apply([]BodyEdit{{BodyBytes: []byte{0x41, 2, 0xb}}}, ValidationFeatures{}, 1, ValidationLimits{}); err != nil {
		t.Fatal(err)
	}
	if m.structuralTypeCache != cache || len(cache.keys) != count || m.typeIndexDirectory != types || m.importIndexDirectory != imports {
		t.Fatal("original cache changed")
	}
	nested := &Module{Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrBlock, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const, I32: 4}}}, Then: []Instruction{{Kind: InstrNop}}, Else: []Instruction{{Kind: InstrNop}}, Indices: []uint32{1}, ValTypes: []ValType{I32}, Bytes: []byte{9}, Catches: []Catch{{Kind: CatchAll}}}}}}}}, NameSec: &NameSec{LocalNames: IndirectNameMap{{Names: NameMap{{Name: "local"}}}}}, StringRefs: [][]byte{{5}}, Data: []Data{{Init: []byte{6}}}}
	copy := cloneBodyEditModule(nested)
	nested.Code[0].Body.Instrs[0].ext.Body.Instrs[0].I32 = 8
	nested.Code[0].Body.Instrs[0].ext.Indices[0] = 7
	nested.Data[0].Init[0] = 1
	nested.StringRefs[0][0] = 2
	nested.NameSec.LocalNames[0].Names[0].Name = "changed"
	if copy.Code[0].Body.Instrs[0].ext.Body.Instrs[0].I32 != 4 || copy.Code[0].Body.Instrs[0].ext.Indices[0] != 1 || copy.Data[0].Init[0] != 6 || copy.StringRefs[0][0] != 5 || copy.NameSec.LocalNames[0].Names[0].Name != "local" {
		t.Fatal("nested alias")
	}
}
