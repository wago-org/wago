package railssa

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func buildSemanticTest(t *testing.T, m *wasm.Module) (*StackFunc, *CFG, *ValueFlow, *SemanticFunc) {
	t.Helper()
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildCFG(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := BuildLocalSSA(f, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := BuildValueFlow(f, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := BuildSemanticFunc(f, cfg, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f, cfg, flow, semantic
}

func TestSemanticInstIsCompact(t *testing.T) {
	if got := unsafe.Sizeof(SemanticInst{}); got != 24 {
		t.Fatalf("SemanticInst size = %d, want 24", got)
	}
}

func TestSemanticFuncOmitsAdministrativeLocals(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x41, 0x01,
		0x6a,
		0x22, 0x00,
		0x1a,
		0x20, 0x00,
		0x0b,
	})
	_, _, flow, semantic := buildSemanticTest(t, m)
	if len(semantic.Insts) != 2 || semantic.Insts[0].Op != wasm.InstrI32Const || semantic.Insts[1].Op != wasm.InstrI32Add {
		t.Fatalf("semantic instructions = %#v", semantic.Insts)
	}
	args := semantic.Operands(1)
	if len(args) != 2 || args[0] != flow.InstructionValues[0] || args[1] != semantic.Insts[0].Result {
		t.Fatalf("add operands = %v", args)
	}
	for _, source := range []int{0, 3, 4, 5, 6} {
		if semantic.InstructionMap[source] != 0 {
			t.Fatalf("administrative instruction %d mapped to %d", source, semantic.InstructionMap[source])
		}
	}
}

func TestSemanticFuncPreservesControlOperandAndDump(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	_, _, flow, semantic := buildSemanticTest(t, m)
	id := semantic.InstructionMap[1]
	if id == 0 {
		t.Fatal("if has no semantic instruction")
	}
	instruction := semantic.Insts[id-1]
	args := semantic.Operands(id - 1)
	if instruction.Op != wasm.InstrIf || len(args) != 1 || args[0] != flow.InstructionValues[0] {
		t.Fatalf("if instruction=%#v args=%v", instruction, args)
	}
	dump := DumpSemantic(semantic)
	if !strings.Contains(dump, "If v") || !strings.Contains(dump, "I32Const") || !strings.Contains(dump, "@1") {
		t.Fatalf("dump:\n%s", dump)
	}
}

func TestEvalSemanticIntegerIfResult(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	f, cfg, flow, semantic := buildSemanticTest(t, m)
	for _, test := range []struct {
		param uint64
		want  uint64
	}{{0, 2}, {1, 1}, {9, 1}} {
		got, err := EvalSemanticInteger(f, cfg, flow, semantic, []uint64{test.param})
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("eval(%d) = %d, want %d", test.param, got, test.want)
		}
	}
}

func TestEvalSemanticIntegerLoopCarriesLocal(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x03, 0x40,
		0x20, 0x00,
		0x41, 0x01,
		0x6b,
		0x22, 0x00,
		0x0d, 0x00,
		0x0b,
		0x20, 0x00,
		0x0b,
	})
	f, cfg, flow, semantic := buildSemanticTest(t, m)
	got, err := EvalSemanticInteger(f, cfg, flow, semantic, []uint64{5})
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("loop result = %d, want 0", got)
	}
}

func TestEvalSemanticIntegerBrTable(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x02, 0x7f,
		0x02, 0x7f,
		0x41, 0x0b,
		0x20, 0x00,
		0x0e, 0x01, 0x00, 0x01,
		0x0b,
		0x41, 0x01,
		0x6a,
		0x0b,
		0x0b,
	})
	f, cfg, flow, semantic := buildSemanticTest(t, m)
	for _, test := range []struct {
		selector uint64
		want     uint64
	}{{0, 12}, {1, 11}, {^uint64(0), 11}} {
		got, err := EvalSemanticInteger(f, cfg, flow, semantic, []uint64{test.selector})
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("br_table(%d) = %d, want %d", test.selector, got, test.want)
		}
	}
}
