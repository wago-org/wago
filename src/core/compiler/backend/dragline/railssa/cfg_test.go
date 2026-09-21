package railssa

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCFGRecordsStayDense(t *testing.T) {
	if got := unsafe.Sizeof(Block{}); got > 48 {
		t.Fatalf("block size = %d, want at most 48", got)
	}
	if got := unsafe.Sizeof(Edge{}); got > 12 {
		t.Fatalf("edge size = %d, want at most 12", got)
	}
}

func TestBuildCFGStructuredLoop(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, nil, []byte{
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x22, 0x00,
		0x0d, 0x00,
		0x0b,
		0x0b,
		0x20, 0x00, 0x1a,
		0x0b,
	})
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildCFG(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Blocks) != 7 {
		t.Fatalf("blocks = %d, want 7\n%s", len(cfg.Blocks), cfg.Dump())
	}
	loopHeaders := 0
	for _, block := range cfg.Blocks {
		if block.Flags&BlockLoopHeader != 0 {
			loopHeaders++
		}
	}
	if loopHeaders != 1 || cfg.Blocks[len(cfg.Blocks)-1].Flags&BlockExit == 0 {
		t.Fatalf("loopHeaders=%d\n%s", loopHeaders, cfg.Dump())
	}
	if got := cfg.Dump(); !strings.Contains(got, "b2 [2,7) region=1 flags=0x1 b2 b3") {
		t.Fatalf("missing loop backedge/fallthrough:\n%s", got)
	}
	backedge, fallEdge := -1, -1
	for i, edge := range cfg.Edges {
		if edge.From == 2 && edge.To == 2 {
			backedge = i
		}
		if edge.From == 2 && edge.To == 3 {
			fallEdge = i
		}
	}
	if backedge < 0 || cfg.EdgeStacks[backedge].CarriesAll() || cfg.EdgeStacks[backedge].PrefixDepth() != 0 || cfg.EdgeStacks[backedge].ResultArity() != 0 {
		t.Fatalf("loop backedge stack = index %d %#x", backedge, func() EdgeStack {
			if backedge < 0 {
				return 0
			}
			return cfg.EdgeStacks[backedge]
		}())
	}
	if fallEdge < 0 || !cfg.EdgeStacks[fallEdge].CarriesAll() {
		t.Fatalf("br_if fallthrough stack = index %d", fallEdge)
	}
}

func TestBuildCFGIfElseAndReturn(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, nil, []byte{
		0x20, 0x00,
		0x04, 0x40,
		0x41, 0x01, 0x21, 0x00,
		0x05,
		0x41, 0x02, 0x21, 0x00,
		0x0b,
		0x0f,
		0x0b,
	})
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildCFG(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Blocks) < 5 {
		t.Fatalf("blocks = %d\n%s", len(cfg.Blocks), cfg.Dump())
	}
	exit := BlockID(len(cfg.Blocks) - 1)
	foundReturn := false
	for _, edge := range cfg.Edges {
		foundReturn = foundReturn || edge.Kind == EdgeReturn && edge.To == exit
	}
	if !foundReturn {
		t.Fatalf("missing return edge:\n%s", cfg.Dump())
	}
	for index, edge := range cfg.Edges {
		if edge.Kind == EdgeReturn && (cfg.EdgeStacks[index].CarriesAll() || cfg.EdgeStacks[index].PrefixDepth() != 0 || cfg.EdgeStacks[index].ResultArity() != 0) {
			t.Fatalf("return stack contract = %#x", cfg.EdgeStacks[index])
		}
	}
}

func TestVerifyCFGRejectsCorruptCoverage(t *testing.T) {
	f := &StackFunc{Instrs: []StackInstr{{Kind: wasm.InstrNop}}}
	cfg := &CFG{Blocks: []Block{{InstStart: 1, InstCount: 1}, {InstStart: 1, Flags: BlockExit}}}
	if err := VerifyCFG(f, cfg); err == nil {
		t.Fatal("corrupt block coverage accepted")
	}
}

func TestVerifyCFGRejectsCorruptEdgeStack(t *testing.T) {
	f := &StackFunc{Instrs: []StackInstr{{Kind: wasm.InstrNop}}, MaxStack: 1}
	cfg := &CFG{
		Blocks: []Block{{InstStart: 0, InstCount: 1, SuccCount: 1}, {InstStart: 1, PredCount: 1, Flags: BlockExit}},
		Edges:  []Edge{{From: 0, To: 1}}, EdgeStacks: []EdgeStack{prefixEdgeStack(2, 0)},
		Preds: []BlockID{0}, Succs: []BlockID{1},
	}
	if err := VerifyCFG(f, cfg); err == nil {
		t.Fatal("out-of-range edge stack prefix accepted")
	}
}
