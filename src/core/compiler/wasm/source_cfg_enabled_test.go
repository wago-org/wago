//go:build wago_regalloccheck

package wasm

import (
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

func sourceEventKind(t *testing.T, l *SourceLedger, kind InstrKind, occurrence int) int {
	t.Helper()
	for i := 0; i < l.EventCount(); i++ {
		if l.Event(i).Kind == kind {
			if occurrence == 0 {
				return i
			}
			occurrence--
		}
	}
	t.Fatalf("missing source event %v", kind)
	return -1
}
func checkSourceCFG(t *testing.T, l *SourceLedger) {
	t.Helper()
	if l.BlockCount() < 2 || !l.Block(0).Reachable || !l.Block(l.ExitBlock()).FunctionExit {
		t.Fatal("invalid source entry/exit")
	}
	for i := 1; i < l.BlockCount(); i++ {
		p := l.Block(i)
		for j := 0; j < p.LocalCount+p.StackCount; j++ {
			id := l.BlockParameter(i, j)
			if l.Value(id).Kind != SourceBlockParameter {
				t.Fatal("block parameter confused with function input")
			}
		}
	}
	for i := 0; i < l.EdgeCount(); i++ {
		e := l.Edge(i)
		p := l.Block(e.To)
		if e.ArgumentCount != p.LocalCount+p.StackCount || !l.Block(e.From).Reachable || !p.Reachable || l.Event(e.Event).PC != e.PC {
			t.Fatalf("invalid source edge %+v", e)
		}
		for j := 0; j < e.ArgumentCount; j++ {
			if l.Value(l.EdgeArgument(i, j)).Type != l.Value(l.BlockParameter(e.To, j)).Type {
				t.Fatal("edge type mismatch")
			}
		}
	}
}

func TestSourceCFGLoopLocalSwap(t *testing.T) {
	m := sourceModule(t, []ValType{I32, I32, I32}, []ValType{I32}, Instruction{Kind: InstrLoop, ext: &instrExt{Body: Expr{Instrs: []Instruction{
		{Kind: InstrLocalGet, Index: 1}, {Kind: InstrLocalGet, Index: 0}, {Kind: InstrLocalSet, Index: 1}, {Kind: InstrLocalSet, Index: 0},
		{Kind: InstrLocalGet, Index: 2}, {Kind: InstrBrIf, Index: 0},
	}}}}, Instruction{Kind: InstrLocalGet, Index: 0})
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	loop := sourceEventKind(t, l, InstrLoop, 0)
	header := -1
	backedge := -1
	for i := 0; i < l.EdgeCount(); i++ {
		e := l.Edge(i)
		if e.Event == loop && e.Arm == SourceFallthrough {
			header = e.To
		}
		if e.Arm == SourceBranchIfTrue {
			backedge = i
		}
	}
	if header < 0 || backedge < 0 || l.Edge(backedge).To != header {
		t.Fatal("missing independent loop preheader/backedge")
	}
	a, b := l.BlockParameter(header, 0), l.BlockParameter(header, 1)
	if a == l.EntryLocal(0) || b == l.EntryLocal(1) || l.EdgeArgument(backedge, 0) != b || l.EdgeArgument(backedge, 1) != a {
		t.Fatal("loop swap is not an independent simultaneous assignment")
	}
	if l.Output(sourceEventKind(t, l, InstrLocalGet, 0), 0) != b || l.Output(sourceEventKind(t, l, InstrLocalGet, 1), 0) != a {
		t.Fatal("loop local aliases wrong")
	}
	if !l.Block(l.ExitBlock()).Reachable {
		t.Fatal("br_if fallthrough lost")
	}
	for _, wrong := range []bool{false, true} {
		graph := sourceObservedEdgeGraph(t, l, wrong)
		r := graph.Verify(regalloccheck.Limits{})
		want := regalloccheck.Verified
		if wrong {
			want = regalloccheck.Rejected
		}
		if r.Verdict != want {
			t.Fatalf("observed CFG wrong=%v: %+v", wrong, r)
		}
	}
}

func TestSourceCFGIfLocalVersionsAndNoElse(t *testing.T) {
	m := sourceModule(t, []ValType{I32}, []ValType{I32}, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrIf, ext: &instrExt{
		Then: []Instruction{{Kind: InstrI32Const, I32: 11}, {Kind: InstrLocalSet, Index: 0}},
		Else: []Instruction{{Kind: InstrI32Const, I32: 22}, {Kind: InstrLocalSet, Index: 0}},
	}}, Instruction{Kind: InstrLocalGet, Index: 0})
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	then := l.Output(sourceEventKind(t, l, InstrI32Const, 0), 0)
	otherwise := l.Output(sourceEventKind(t, l, InstrI32Const, 1), 0)
	join := -1
	haveThen, haveElse := false, false
	for i := 0; i < l.EdgeCount(); i++ {
		e := l.Edge(i)
		if l.Block(e.To).LocalCount != 1 || e.Arm != SourceFallthrough {
			continue
		}
		arg := l.EdgeArgument(i, 0)
		if arg == then {
			join = e.To
			haveThen = true
		}
		if arg == otherwise {
			if join != -1 && join != e.To {
				t.Fatal("arms have different joins")
			}
			join = e.To
			haveElse = true
		}
	}
	if !haveThen || !haveElse || join < 0 || l.Output(sourceEventKind(t, l, InstrLocalGet, 1), 0) != l.BlockParameter(join, 0) {
		t.Fatal("then/else local versions mixed")
	}
	// No else must preserve the original inputs and entry local version along
	// its false path, even when the then arm writes that local.
	m = sourceModule(t, []ValType{I32}, []ValType{I32}, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrIf, ext: &instrExt{Then: []Instruction{{Kind: InstrI32Const, I32: 9}, {Kind: InstrLocalSet, Index: 0}}}}, Instruction{Kind: InstrLocalGet, Index: 0})
	l = sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	falseBlock := -1
	for i := 0; i < l.EdgeCount(); i++ {
		if e := l.Edge(i); e.Arm == SourceElse {
			falseBlock = e.To
			if l.EdgeArgument(i, 0) != l.EntryLocal(0) {
				t.Fatal("false path seeded from then state")
			}
		}
	}
	if falseBlock < 0 {
		t.Fatal("missing false path")
	}
	found := false
	for i := 0; i < l.EdgeCount(); i++ {
		if e := l.Edge(i); e.From == falseBlock {
			found = true
			if l.EdgeArgument(i, 0) != l.BlockParameter(falseBlock, 0) {
				t.Fatal("no-else passthrough changed local")
			}
		}
	}
	if !found {
		t.Fatal("false path did not reach join")
	}
}

func TestSourceCFGIndexedLoopStackArguments(t *testing.T) {
	m := sourceModule(t, []ValType{I32, I32}, []ValType{I32}, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLoop, ext: &instrExt{
		BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 1}}, Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 1}, {Kind: InstrBrIf, Index: 0}}},
	}})
	m.Types = append(m.Types, ft([]ValType{I32}, []ValType{I32}))
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	loop := sourceEventKind(t, l, InstrLoop, 0)
	header := -1
	for i := 0; i < l.EdgeCount(); i++ {
		if e := l.Edge(i); e.Event == loop && e.Arm == SourceFallthrough {
			header = e.To
		}
	}
	if header < 0 || l.Block(header).StackCount != 1 {
		t.Fatal("indexed loop input shape lost")
	}
	stackParam := l.BlockParameter(header, l.Block(header).LocalCount)
	for i := 0; i < l.EdgeCount(); i++ {
		if e := l.Edge(i); e.Arm == SourceBranchIfTrue {
			if e.To != header || l.EdgeArgument(i, e.ArgumentCount-1) != stackParam {
				t.Fatal("br_if did not preserve payload alias")
			}
		}
	}
}

func TestSourceCFGTypedNoElseAndReturningThen(t *testing.T) {
	m := sourceModule(t, []ValType{I32, I32}, []ValType{I32}, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalGet, Index: 1},
		Instruction{Kind: InstrIf, ext: &instrExt{BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 1}}, Then: []Instruction{{Kind: InstrNop}}}})
	m.Types = append(m.Types, ft([]ValType{I32}, []ValType{I32}))
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	falseBlock := -1
	for i := 0; i < l.EdgeCount(); i++ {
		if e := l.Edge(i); e.Arm == SourceElse {
			falseBlock = e.To
			if l.EdgeArgument(i, e.ArgumentCount-1) != l.EntryLocal(0) {
				t.Fatal("typed no-else lost original input")
			}
		}
	}
	if falseBlock < 0 || l.Block(falseBlock).StackCount != 1 {
		t.Fatal("typed false path shape lost")
	}
	m = sourceModule(t, []ValType{I32}, []ValType{I32}, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrIf, ext: &instrExt{
		Then: []Instruction{{Kind: InstrI32Const, I32: 7}, {Kind: InstrReturn}},
	}}, Instruction{Kind: InstrLocalGet, Index: 0})
	l = sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	returns, fallthroughs := 0, 0
	for i := 0; i < l.EdgeCount(); i++ {
		e := l.Edge(i)
		if e.To == l.ExitBlock() {
			if e.Arm == SourceReturn {
				returns++
			} else {
				fallthroughs++
			}
		}
	}
	if returns != 1 || fallthroughs != 1 {
		t.Fatal("then return suppressed independently reachable false return")
	}
}

func TestSourceCFGBranchDiscardsOnlyInnerSuffix(t *testing.T) {
	m := sourceModule(t, nil, []ValType{I32}, Instruction{Kind: InstrI32Const, I32: 99}, Instruction{Kind: InstrBlock, ext: &instrExt{
		BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const, I32: 11}, {Kind: InstrI32Const, I32: 42}, {Kind: InstrBr, Index: 0}}},
	}}, Instruction{Kind: InstrI32Add})
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	prefix := l.Output(sourceEventKind(t, l, InstrI32Const, 0), 0)
	payload := l.Output(sourceEventKind(t, l, InstrI32Const, 2), 0)
	found := false
	for i := 0; i < l.EdgeCount(); i++ {
		if e := l.Edge(i); e.Arm == SourceBranch {
			found = true
			if e.ArgumentCount != 2 || l.EdgeArgument(i, 0) != prefix || l.EdgeArgument(i, 1) != payload {
				t.Fatal("branch kept discarded suffix or lost outer prefix")
			}
		}
	}
	if !found {
		t.Fatal("missing branch edge")
	}
}

func TestSourceCFGBranchesReviveOuterExit(t *testing.T) {
	m := sourceModule(t, nil, []ValType{I32}, Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{
		{Kind: InstrBlock, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const, I32: 42}, {Kind: InstrBr, Index: 1}, {Kind: InstrNop}}}}},
		{Kind: InstrUnreachable},
	}}}})
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	branch := sourceEventKind(t, l, InstrBr, 0)
	target := -1
	for i := 0; i < l.EdgeCount(); i++ {
		if e := l.Edge(i); e.Event == branch {
			target = e.To
			if l.Value(l.EdgeArgument(i, e.ArgumentCount-1)).Bits != 42 {
				t.Fatal("outer branch payload lost")
			}
		}
	}
	if target < 0 || !l.Block(target).Reachable || !l.Block(l.ExitBlock()).Reachable {
		t.Fatal("reachable outer exit discarded")
	}
	nop := l.Event(sourceEventKind(t, l, InstrNop, 0))
	if !nop.Unreachable || nop.OutputCount != 0 || nop.InputCount != 0 {
		t.Fatal("dead lexical code invented values")
	}
	for i := 0; i < l.EdgeCount(); i++ {
		if l.Edge(i).Event == sourceEventKind(t, l, InstrUnreachable, 0) {
			t.Fatal("dead trap fabricated successor")
		}
	}
}

func TestSourceCFGBranchTableCasesAndFunctionLabel(t *testing.T) {
	m := sourceModule(t, []ValType{I32, I32}, []ValType{I32}, Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{
		{Kind: InstrLocalGet, Index: 0}, {Kind: InstrLocalGet, Index: 1}, {Kind: InstrBrTable, Index: 1, ext: &instrExt{Indices: []uint32{0, 0, 1}}},
	}}}})
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	table := sourceEventKind(t, l, InstrBrTable, 0)
	cases := map[uint32]SourceEdge{}
	defaults := 0
	for i := 0; i < l.EdgeCount(); i++ {
		e := l.Edge(i)
		if e.Event != table {
			continue
		}
		if e.Arm == SourceTableCase {
			cases[e.Case] = e
		} else if e.Arm == SourceTableDefault {
			defaults++
			if !l.Block(e.To).FunctionExit {
				t.Fatal("default function label lost")
			}
		} else {
			t.Fatal("table has lexical fallthrough")
		}
		if e.Condition != l.Output(sourceEventKind(t, l, InstrLocalGet, 1), 0) {
			t.Fatal("table selector lost")
		}
	}
	if len(cases) != 3 || defaults != 1 || cases[0].To != cases[1].To || cases[2].To != l.ExitBlock() {
		t.Fatal("repeated case/default arms collapsed")
	}
}

func TestSourceCFGInfiniteLoopAndDeadValidation(t *testing.T) {
	m := sourceModule(t, nil, []ValType{I32}, Instruction{Kind: InstrLoop, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrBr, Index: 0}}}}}, Instruction{Kind: InstrI32Const, I32: 123})
	a := sourceAnalysis(t, m, ValidationFeatures{})
	l := sourceComplete(t, m, a, ValidationFeatures{})
	checkSourceCFG(t, l)
	if l.Block(l.ExitBlock()).Reachable {
		t.Fatal("infinite loop manufactured a result path")
	}
	e := l.Event(sourceEventKind(t, l, InstrI32Const, 0))
	if !e.Unreachable || e.OutputCount != 0 {
		t.Fatal("post-loop lexical output became a reachable definition")
	}
	// The identity layer may skip dead definitions, but authoritative validation
	// must still reject malformed instructions there, even with a stale token.
	m.Code[0].BodyBytes = []byte{0x03, 0x40, 0x0c, 0, 0x0b, 0x20, 99, 0x0b}
	if ledger, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{}); ledger != nil || r.Coverage != SourceInvalid || err == nil {
		t.Fatalf("invalid dead source: %+v %v", r, err)
	}
}

func TestSourceCFGResourceLimits(t *testing.T) {
	m := sourceModule(t, []ValType{I32}, nil, Instruction{Kind: InstrLoop, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrBrIf, Index: 0}}}}})
	a := sourceAnalysis(t, m, ValidationFeatures{})
	for _, limits := range []SourceLedgerLimits{{Blocks: 1}, {Edges: 1}, {ControlDepth: 1}, {Values: 2}, {Operands: 2}, {Work: 8}} {
		l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, limits)
		if l != nil || err != nil || r.Coverage != SourceIncomplete || r.Reason != regalloccheck.ResourceLimit {
			t.Fatalf("limits%+v: %+v %v", limits, r, err)
		}
	}
}

func TestSourceCFGDeadIndexedEntryStackLimit(t *testing.T) {
	m := sourceModule(t, nil, nil, Instruction{Kind: InstrUnreachable}, Instruction{Kind: InstrBlock, ext: &instrExt{
		BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 1}}, Body: Expr{Instrs: []Instruction{{Kind: InstrDrop}, {Kind: InstrDrop}, {Kind: InstrDrop}}},
	}})
	m.Types = append(m.Types, ft([]ValType{I32, I32, I32}, nil))
	a := sourceAnalysis(t, m, ValidationFeatures{})
	if l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{Stack: 2}); l != nil || err != nil || r.Coverage != SourceIncomplete || r.Reason != regalloccheck.ResourceLimit || r.PC != 1 {
		t.Fatalf("dead indexed signature: %+v %v", r, err)
	}
}

// A test-only declared physical mapping places semantic IDs in distinct frame
// homes and lowers each source edge through a separate move block. This tests
// source parameters plus actual encoder transfers and simultaneous copies. It
// does not establish compiler branch/ABI mapping or execute the emitted bytes.
func sourceObservedEdgeGraph(t *testing.T, l *SourceLedger, wrong bool) regalloccheck.Graph {
	t.Helper()
	g := regalloccheck.Graph{Blocks: make([]regalloccheck.Block, l.BlockCount())}
	location := func(id SourceValueID) regalloccheck.Location { return regalloccheck.Slot(int32(id) * 8) }
	for id := 1; id <= l.ValueCount(); id++ {
		if l.Value(SourceValueID(id)).Type != I32 {
			t.Fatal("test mapping only admits i32")
		}
		g.Widths = append(g.Widths, 4)
	}
	for i := 0; i < l.LocalCount(); i++ {
		id := l.EntryLocal(i)
		g.Inputs = append(g.Inputs, regalloccheck.Binding{Location: location(id), Value: regalloccheck.ValueID(id)})
	}
	appendUse := func(block int, id SourceValueID) {
		g.Blocks[block].Operations = append(g.Blocks[block].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: location(id), Value: regalloccheck.ValueID(id)})
	}
	for i := 0; i < l.EventCount(); i++ {
		e := l.Event(i)
		if e.Unreachable {
			continue
		}
		for j := 0; j < e.InputCount; j++ {
			appendUse(e.Block, l.Input(i, j))
		}
		if e.Kind == InstrLocalGet || e.Kind == InstrLocalTee {
			appendUse(e.Block, l.Output(i, 0))
		}
	}
	for i := 0; i < l.EdgeCount(); i++ {
		e := l.Edge(i)
		moveBlock := len(g.Blocks)
		g.Blocks = append(g.Blocks, regalloccheck.Block{})
		g.Blocks[e.From].Edges = append(g.Blocks[e.From].Edges, regalloccheck.Edge{To: moveBlock})
		var a enc.Asm
		a.ObserveRegalloc(func(effect regalloccheck.Effect) {
			g.Blocks[moveBlock].Operations = append(g.Blocks[moveBlock].Operations, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: effect})
		})
		for j := 0; j < e.ArgumentCount; j++ {
			src := l.EdgeArgument(i, j)
			if wrong && e.Arm == SourceBranchIfTrue && j == 0 {
				src = l.EdgeArgument(i, 1)
			}
			a.Load32(enc.RAX, enc.RSP, location(src).Index)
			a.Store32(enc.RSP, int32(l.ValueCount()+1+j)*8, enc.RAX)
		}
		params := make([]regalloccheck.Parameter, e.ArgumentCount)
		for j := 0; j < e.ArgumentCount; j++ {
			from, to := l.EdgeArgument(i, j), l.BlockParameter(e.To, j)
			a.Load32(enc.RAX, enc.RSP, int32(l.ValueCount()+1+j)*8)
			a.Store32(enc.RSP, location(to).Index, enc.RAX)
			params[j] = regalloccheck.Parameter{From: regalloccheck.ValueID(from), To: regalloccheck.ValueID(to), Location: location(to)}
		}
		g.Blocks[moveBlock].Edges = []regalloccheck.Edge{{To: e.To, Parameters: params}}
	}
	exit := l.Block(l.ExitBlock())
	for i := 0; i < exit.StackCount; i++ {
		appendUse(l.ExitBlock(), l.BlockParameter(l.ExitBlock(), i))
	}
	return g
}
