package railssa

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLocalSSACreatesLoopBlockArgument(t *testing.T) {
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
	ssa, err := BuildLocalSSA(f, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ssa.Params) != 1 {
		t.Fatalf("block params = %#v", ssa.Params)
	}
	param := ssa.Params[0]
	if cfg.Blocks[param.Block].Flags&BlockLoopHeader == 0 || param.Local != 0 {
		t.Fatalf("loop parameter = %#v", param)
	}
	if len(ssa.EdgeArgs) != 2 {
		t.Fatalf("loop edge args = %#v", ssa.EdgeArgs)
	}
	if got := ssa.InstructionValues[2]; got != param.Value {
		t.Fatalf("loop local.get value = %d, want param %d", got, param.Value)
	}
}

func TestLocalSSACreatesIfMergeArgument(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, nil, []byte{
		0x20, 0x00,
		0x04, 0x40,
		0x41, 0x01, 0x21, 0x00,
		0x05,
		0x41, 0x02, 0x21, 0x00,
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
	ssa, err := BuildLocalSSA(f, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ssa.Params) != 1 || ssa.Params[0].Local != 0 {
		t.Fatalf("merge params = %#v", ssa.Params)
	}
	param := ssa.Params[0]
	if got := ssa.InstructionValues[8]; got != param.Value {
		t.Fatalf("post-if local.get value = %d, want param %d", got, param.Value)
	}
	if len(ssa.EdgeArgs) != 2 || ssa.EdgeArgs[0].Argument == ssa.EdgeArgs[1].Argument {
		t.Fatalf("merge edge args = %#v", ssa.EdgeArgs)
	}
}

func TestLocalSSABudgetIsStrict(t *testing.T) {
	f := &StackFunc{Locals: make([]wasm.ValType, 4096), Instrs: []StackInstr{{Kind: wasm.InstrNop}}}
	cfg := &CFG{Blocks: make([]Block, maxLocalSSACells/4096+1)}
	if _, err := BuildLocalSSA(f, cfg, nil); err == nil {
		t.Fatal("oversized local SSA matrix accepted")
	}
}

func TestLocalSSABudgetAdmitsRubyScaleMatrix(t *testing.T) {
	// Ruby's largest function has 1,092 locals across 991 CFG blocks. This is
	// just beyond the former one-million-cell guard, but remains inside the same
	// transient-byte ceiling after eliminating the redundant live-out matrix.
	const locals, blocks = 1092, 991
	if cells := locals * blocks; cells <= 1<<20 || cells > maxLocalSSACells {
		t.Fatalf("Ruby-scale cells = %d, budget = %d", cells, maxLocalSSACells)
	}
	f := &StackFunc{Locals: make([]wasm.ValType, locals), Instrs: []StackInstr{{Kind: wasm.InstrNop}}}
	cfg := &CFG{Blocks: make([]Block, blocks)}
	if _, err := BuildLocalSSA(f, cfg, nil); err != nil {
		t.Fatalf("Ruby-scale local SSA matrix rejected: %v", err)
	}
}

func TestLocalSSARemovesDeadMergeArguments(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, nil, []byte{
		0x20, 0x00,
		0x04, 0x40,
		0x41, 0x01, 0x21, 0x00,
		0x05,
		0x41, 0x02, 0x21, 0x00,
		0x0b,
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
	ssa, err := BuildLocalSSA(f, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ssa.Params) != 0 || len(ssa.EdgeArgs) != 0 {
		t.Fatalf("dead merge retained params=%#v args=%#v", ssa.Params, ssa.EdgeArgs)
	}
}
