package wago

import (
	"math"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAnalyzeModuleRequirementsFusesModuleFacts(t *testing.T) {
	for _, tc := range []struct {
		name string
		code wasm.Func
	}{
		{
			name: "byte-backed",
			code: wasm.Func{BodyBytes: []byte{
				0xfc, 0x0f, 0x01, // table.grow 1
				0x40, 0x00, // memory.grow 0
				0xd2, 0x00, // ref.func 0
				0x0b,
			}},
		},
		{
			name: "instruction-backed",
			code: wasm.Func{Body: wasm.Expr{Instrs: []wasm.Instruction{
				{Kind: wasm.InstrTableGrow, Index: 1},
				{Kind: wasm.InstrMemoryGrow, Index: 0},
				{Kind: wasm.InstrRefFunc, Index: 0},
			}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &wasm.Module{
				Tables:   []wasm.Table{{}, {}},
				Memories: []wasm.MemType{{}},
				Exports: []wasm.Export{
					{Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
					{Index: wasm.ExternIdx{Kind: wasm.ExternMem, Index: 0}},
				},
				Code: []wasm.Func{tc.code},
			}
			want, err := frontend.AnalyzeModuleFacts(m)
			if err != nil {
				t.Fatal(err)
			}
			got := analyzeModuleRequirements(m).moduleFacts
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("fused module facts = %+v, want %+v", got, want)
			}
		})
	}
}

func TestAnalyzeModuleRequirementsFusesAtomicWaitHelpers(t *testing.T) {
	byteBacked := &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{
		0xfe, 0x00, 0x00, 0x00, // memory.atomic.notify align=0 offset=0
		0x0b,
	}}}}
	if !analyzeModuleRequirements(byteBacked).atomicWaitHelpers {
		t.Fatal("byte-backed atomic notify did not select wait helpers")
	}

	instructionBacked := &wasm.Module{Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrMemoryAtomicWait32},
	}}}}}
	if !analyzeModuleRequirements(instructionBacked).atomicWaitHelpers {
		t.Fatal("instruction-backed atomic wait did not select wait helpers")
	}

	directOnly := &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{
		0xfe, 0x10, 0x00, 0x00, // i32.atomic.load align=0 offset=0
		0x0b,
	}}}}
	if analyzeModuleRequirements(directOnly).atomicWaitHelpers {
		t.Fatal("direct atomic instruction selected wait helpers")
	}
}

func TestAnalyzeModuleRequirementsFusesIndexedFunctionRefOps(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		Code: []wasm.Func{{BodyBytes: []byte{
			0xfb, 0x14, 0x00, // ref.test (ref null 0)
			0xfb, 0x16, 0x00, // ref.cast (ref null 0)
			0x0b,
		}}},
	}
	requirements := analyzeModuleRequirements(m)
	if !requirements.indexedFuncRefTest || !requirements.indexedFuncRefCast || requirements.arm64GCRefTestHelper {
		t.Fatalf("function ref requirements = test:%v cast:%v helper:%v, want true/true/false", requirements.indexedFuncRefTest, requirements.indexedFuncRefCast, requirements.arm64GCRefTestHelper)
	}

	abstract := &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{
		0xfb, 0x14, 0x70, // ref.test funcref
		0x0b,
	}}}}
	requirements = analyzeModuleRequirements(abstract)
	if requirements.indexedFuncRefTest || requirements.indexedFuncRefCast {
		t.Fatalf("abstract ref op selected indexed function metadata: %+v", requirements)
	}

	gcHeap := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompStruct}}}}},
		Code:  []wasm.Func{{BodyBytes: []byte{0xfb, 0x14, 0x00, 0x0b}}},
	}
	requirements = analyzeModuleRequirements(gcHeap)
	if !requirements.arm64GCRefTestHelper || requirements.indexedFuncRefTest || requirements.indexedFuncRefCast {
		t.Fatalf("gc ref.test requirements = test:%v cast:%v helper:%v, want false/false/true", requirements.indexedFuncRefTest, requirements.indexedFuncRefCast, requirements.arm64GCRefTestHelper)
	}
}

func TestAnalyzeModuleRequirementsFusesDeclarationRefFunc(t *testing.T) {
	for _, init := range []wasm.Expr{
		{BodyBytes: []byte{0xd2, 0x00, 0x0b}},
		{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc, Index: 0}}},
	} {
		m := &wasm.Module{Globals: []wasm.Global{{Init: init}}}
		want, err := frontend.AnalyzeModuleFacts(m)
		if err != nil {
			t.Fatal(err)
		}
		got := analyzeModuleRequirements(m).moduleFacts
		if !reflect.DeepEqual(got, want) || !got.UsesRefFunc {
			t.Fatalf("fused declaration facts = %+v, want %+v", got, want)
		}
	}
}

func TestAnalyzeModuleRequirementsIgnoresInvalidFactIndexesBeforeValidation(t *testing.T) {
	m := &wasm.Module{
		Tables:   []wasm.Table{{}},
		Memories: []wasm.MemType{{}},
		Exports: []wasm.Export{
			{Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: math.MaxUint32}},
			{Index: wasm.ExternIdx{Kind: wasm.ExternMem, Index: math.MaxUint32}},
		},
		Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{
			{Kind: wasm.InstrTableGrow, Index: math.MaxUint32},
			{Kind: wasm.InstrMemoryGrow, Index: math.MaxUint32},
		}}}},
	}
	facts := analyzeModuleRequirements(m).moduleFacts
	if facts.TableExported[0] || facts.MemoryExported[0] || facts.TableGrowUsed[0] || facts.MemoryGrowUsed[0] {
		t.Fatalf("invalid indexes polluted fused facts: %+v", facts)
	}
}
