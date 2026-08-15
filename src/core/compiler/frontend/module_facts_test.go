package frontend

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestNewModuleFactsVectorsAreDisjoint(t *testing.T) {
	facts := NewModuleFacts(2, 3)
	facts.TableGrowUsed[0] = true
	facts.TableExported[1] = true
	facts.MemoryGrowUsed[2] = true
	facts.MemoryExported[0] = true

	if !reflect.DeepEqual(facts.TableGrowUsed, []bool{true, false}) ||
		!reflect.DeepEqual(facts.TableExported, []bool{false, true}) ||
		!reflect.DeepEqual(facts.MemoryGrowUsed, []bool{false, false, true}) ||
		!reflect.DeepEqual(facts.MemoryExported, []bool{true, false, false}) {
		t.Fatalf("fact vectors alias: %+v", facts)
	}
}

func moduleFactsFixture(byteBacked bool) *wasm.Module {
	m := &wasm.Module{
		Tables:   []wasm.Table{{}, {}},
		Memories: []wasm.MemType{{}},
		Exports: []wasm.Export{
			{Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
			{Index: wasm.ExternIdx{Kind: wasm.ExternMem, Index: 0}},
		},
	}
	if byteBacked {
		m.Code = []wasm.Func{{BodyBytes: []byte{0xfc, 0x0f, 0x01, 0x40, 0x00, 0xd2, 0x00, 0x0b}}}
	} else {
		m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{
			{Kind: wasm.InstrTableGrow, Index: 1},
			{Kind: wasm.InstrMemoryGrow, Index: 0},
			{Kind: wasm.InstrRefFunc, Index: 0},
		}}}}
	}
	return m
}

func TestAnalyzeModuleFactsMixedMemory64MemargDoesNotInventFacts(t *testing.T) {
	m := &wasm.Module{
		Memories: []wasm.MemType{
			{Limits: wasm.Limits{Min: 1}},
			{Limits: wasm.Limits{Min: 1, Addr64: true}},
		},
		Code: []wasm.Func{{BodyBytes: []byte{
			0x42, 0x00, // i64.const 0
			0x28, 0x40, 0x01, 0x80, 0x80, 0x80, 0x80, 0x10, // i32.load memory 1, offset 1<<32
			0x1a, 0x0b, // drop; end
		}}},
	}
	facts, err := AnalyzeModuleFacts(m)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(facts.MemoryGrowUsed, []bool{false, false}) || facts.UsesRefFunc {
		t.Fatalf("mixed-width memarg invented module facts: %+v", facts)
	}
}

func TestAnalyzeModuleFactsMatchesByteAndInstructionForms(t *testing.T) {
	byteFacts, err := AnalyzeModuleFacts(moduleFactsFixture(true))
	if err != nil {
		t.Fatal(err)
	}
	astFacts, err := AnalyzeModuleFacts(moduleFactsFixture(false))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(byteFacts, astFacts) {
		t.Fatalf("byte facts = %+v, AST facts = %+v", byteFacts, astFacts)
	}
	if !reflect.DeepEqual(astFacts.TableGrowUsed, []bool{false, true}) || !reflect.DeepEqual(astFacts.TableExported, []bool{true, false}) || !reflect.DeepEqual(astFacts.MemoryGrowUsed, []bool{true}) || !reflect.DeepEqual(astFacts.MemoryExported, []bool{true}) || !astFacts.UsesRefFunc {
		t.Fatalf("unexpected module facts: %+v", astFacts)
	}
}

func TestRejectUnsupportedWithFeaturesAndFactsUsesCallerAnalysis(t *testing.T) {
	m := &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1}}}}}
	facts, err := AnalyzeModuleFacts(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := RejectUnsupportedWithFeaturesAndFacts(m, Features{ReferenceTypes: true}, facts); err != nil {
		t.Fatalf("valid supplied facts: %v", err)
	}
	bad := *facts
	bad.TableGrowUsed = nil
	if err := RejectUnsupportedWithFeaturesAndFacts(m, Features{ReferenceTypes: true}, &bad); err == nil {
		t.Fatal("support pass ignored malformed caller-supplied facts and rescanned the module")
	}
}

func TestAnalyzeModuleFactsFastScalarPathsRejectTruncatedImmediates(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{name: "local.get", body: []byte{0x20}},
		{name: "ref.func", body: []byte{0xd2}},
		{name: "i32.const", body: []byte{0x41}},
		{name: "f32.const", body: []byte{0x43, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &wasm.Module{Code: []wasm.Func{{BodyBytes: tc.body}}}
			if _, err := AnalyzeModuleFacts(m); err == nil {
				t.Fatal("AnalyzeModuleFacts accepted truncated immediate")
			}
		})
	}
}

func BenchmarkAnalyzeModuleFactsScalarBody(b *testing.B) {
	body := bytes.Repeat([]byte{0x41, 1, 0x41, 2, 0x6a, 0x1a}, 16<<10)
	body = append(body, 0x0b)
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: body}}}
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := AnalyzeModuleFacts(m); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAnalyzeModuleFactsManyTables(b *testing.B) {
	m := &wasm.Module{Tables: make([]wasm.Table, 256)}
	instrs := make([]wasm.Instruction, 4096)
	for i := range instrs {
		instrs[i] = wasm.Instruction{Kind: wasm.InstrTableGrow, Index: uint32(i % len(m.Tables))}
	}
	m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: instrs}}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := AnalyzeModuleFacts(m); err != nil {
			b.Fatal(err)
		}
	}
}
