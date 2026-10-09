//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// BeginSourceLoop admits one alias-only backedge family. It uses the same
// historical budgets, exclusive attempt, journal and cleanup as SourceBranch.
// All three header locals remain live; exit-only local aliases may be trimmed.
func BeginSourceLoop(s *ScalarState, m *wasm.Module, function int, admit bool) *SourceBranch {
	if s == nil {
		return nil
	}
	a, features, ok := codegen.ValidatedSourceContext(s.sourceContext, m)
	if !ok || function < 0 || function >= len(m.Code) {
		return nil
	}
	unavailable := sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "no admitted loop source recipe")
	codegen.RecordSourceResult(s.sourceContext, function, unavailable)
	if s.sourceWork < 32 {
		codegen.RecordSourceResult(s.sourceContext, function, sourceLeafUnavailable(regalloccheck.ResourceLimit, "source compilation work exhausted"))
		return nil
	}
	s.sourceWork -= 32
	if !admit || len(m.Code[function].Locals.Runs) != 0 || !sourceLoopBody(m.Code[function].BodyBytes) {
		return nil
	}
	// Historical pool-entry credits cover the bounded source/journal/model pools
	// plus all 4096 analysis fact credits, including abandoned attempts.
	if s.sourceStorage < 8192 || s.sourceWork < 1 {
		codegen.RecordSourceResult(s.sourceContext, function, sourceLeafUnavailable(regalloccheck.ResourceLimit, "loop source storage exhausted"))
		return nil
	}
	s.sourceStorage -= 8192
	t := BeginSourceAttempt(s, m, function)
	if t == nil {
		return nil
	}
	b := &SourceBranch{owner: s, attempt: t, result: unavailable, loop: true}
	keep := false
	defer func() {
		if !keep {
			b.Close()
		}
	}()
	l, r, err := wasm.BuildSourceLedger(m, a, function, features, wasm.SourceLedgerLimits{BodyBytes: 18, Metadata: 256, Locals: 3, Events: 10, Values: 13, Operands: 48, Stack: 2, Blocks: 5, Edges: 5, ControlDepth: 2, Work: min(4096, s.sourceWork)})
	s.sourceWork -= r.Work
	if err != nil || r.Coverage != wasm.SourceContractsComplete {
		l.Close()
		b.result = sourceLeafUnavailable(r.Reason, "loop source contracts incomplete")
		return nil
	}
	if !AttachSourceLedger(t, l) {
		l.Close()
		return nil
	}
	b.ledger = l
	// Bound metadata before a signature lookup can populate a cold module type
	// directory. Normal validation warms it, but this helper does not require it.
	ft, valid := m.LocalFuncType(function)
	if !valid || len(ft.Params) != 3 || len(ft.Results) != 1 || ft.Params[0] != wasm.I32 || ft.Params[1] != wasm.I32 || ft.Params[2] != wasm.I32 || ft.Results[0] != wasm.I32 {
		return nil
	}
	if l.EventCount() != 10 || l.ValueCount() != 13 || l.BlockCount() != 5 || l.EdgeCount() != 5 || l.LocalCount() != 3 {
		return nil
	}
	b.entry, b.then, b.otherwise, b.join, b.exit = l.Event(0).Block, l.Event(1).Block, l.Event(7).Block, l.Event(8).Block, l.ExitBlock()
	var seen [5]bool
	for _, index := range []int{b.entry, b.then, b.otherwise, b.join, b.exit} {
		if index < 0 || index >= 5 || seen[index] {
			return nil
		}
		seen[index] = true
	}
	// Original entry IDs and new loop-header phis stay distinct. The two local
	// setters swap source names; the unchanged third local is the loop condition.
	if l.Output(1, 0) != l.BlockParameter(b.then, 1) || l.Output(2, 0) != l.BlockParameter(b.then, 0) ||
		l.Input(3, 0) != l.BlockParameter(b.then, 0) || l.Input(4, 0) != l.BlockParameter(b.then, 1) ||
		l.Input(6, 0) != l.BlockParameter(b.then, 2) || l.Output(8, 0) != l.BlockParameter(b.join, 0) ||
		l.Input(9, 0) != l.BlockParameter(b.join, 0) || !l.Event(9).FunctionEnd {
		return nil
	}
	for i := 1; i <= l.ValueCount(); i++ {
		if l.Value(wasm.SourceValueID(i)).Type != wasm.I32 {
			return nil
		}
	}
	var edges uint8
	for i := 0; i < l.EdgeCount(); i++ {
		e := l.Edge(i)
		var bit uint8
		switch {
		case e.From == b.entry && e.To == b.then:
			bit = 1
			if e.Arm != wasm.SourceFallthrough || e.Condition != 0 || e.ArgumentCount != 3 {
				return nil
			}
			for j := 0; j < 3; j++ {
				if l.EdgeArgument(i, j) != l.EntryLocal(j) {
					return nil
				}
			}
		case e.From == b.then && (e.To == b.then || e.To == b.otherwise):
			bit = 2
			arm := wasm.SourceBranchIfTrue
			if e.To == b.otherwise {
				bit = 4
				arm = wasm.SourceBranchIfFalse
			}
			if e.Arm != arm || e.Condition != l.BlockParameter(b.then, 2) || e.ArgumentCount != 3 ||
				l.EdgeArgument(i, 0) != l.BlockParameter(b.then, 1) || l.EdgeArgument(i, 1) != l.BlockParameter(b.then, 0) || l.EdgeArgument(i, 2) != l.BlockParameter(b.then, 2) {
				return nil
			}
		case e.From == b.otherwise && e.To == b.join:
			bit = 8
			if e.Arm != wasm.SourceFallthrough || e.Condition != 0 || e.ArgumentCount != 3 {
				return nil
			}
		case e.From == b.join && e.To == b.exit:
			bit = 16
			if e.Arm != wasm.SourceFallthrough || e.Condition != 0 || e.ArgumentCount != 1 {
				return nil
			}
		default:
			return nil
		}
		if edges&bit != 0 {
			return nil
		}
		edges |= bit
	}
	if edges != 31 {
		return nil
	}
	if s.sourceWork < 128 {
		b.result = sourceLeafUnavailable(regalloccheck.ResourceLimit, "loop journal work exhausted")
		return nil
	}
	s.sourceWork -= 128
	b.journal = regalloccheck.NewEmissionJournal(regalloccheck.JournalLimits{Events: 64, Spans: 1, Transactions: 1, Work: 128})
	b.journal.BeginEmission(0)
	keep = true
	return b
}

func sourceLoopBody(body []byte) bool {
	return len(body) == 18 && string(body) == "\x03\x40\x20\x01\x20\x00\x21\x01\x21\x00\x20\x02\x0d\x00\x0b\x20\x00\x0b"
}
