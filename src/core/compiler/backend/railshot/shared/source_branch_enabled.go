//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// SourceBranch owns one bounded alias-if or alias-loop attempt. Its ledger names
// are independent of pins and merge registers. Neither entering a block nor a
// reload defines a semantic value: only proven simultaneous edges add aliases.
type SourceBranch struct {
	owner      *ScalarState
	attempt    *SourceAttempt
	ledger     *wasm.SourceLedger
	journal    *regalloccheck.EmissionJournal
	oldLen     int
	done, loop bool
	result     regalloccheck.Result
	// In the loop family, then is the header, otherwise is its false successor,
	// and join is the block following the loop.
	entry, then, otherwise, join, exit int
}

func BeginSourceBranch(s *ScalarState, m *wasm.Module, function int, admit bool) *SourceBranch {
	if s == nil {
		return nil
	}
	a, features, ok := codegen.ValidatedSourceContext(s.sourceContext, m)
	if !ok || function < 0 || function >= len(m.Code) {
		return nil
	}
	unavailable := sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "no admitted framed source recipe")
	codegen.RecordSourceResult(s.sourceContext, function, unavailable)
	if s.sourceWork < 32 {
		codegen.RecordSourceResult(s.sourceContext, function, sourceLeafUnavailable(regalloccheck.ResourceLimit, "source compilation work exhausted"))
		return nil
	}
	s.sourceWork -= 32
	if !admit || len(m.Code[function].Locals.Runs) != 0 || !sourceBranchBody(m.Code[function].BodyBytes) {
		return nil
	}
	// Historical pool-entry credits cover the bounded source/journal/model pools
	// plus all 4096 analysis fact credits, including abandoned attempts.
	if s.sourceStorage < 8192 || s.sourceWork < 1 {
		codegen.RecordSourceResult(s.sourceContext, function, sourceLeafUnavailable(regalloccheck.ResourceLimit, "framed source storage exhausted"))
		return nil
	}
	s.sourceStorage -= 8192
	t := BeginSourceAttempt(s, m, function)
	if t == nil {
		return nil
	}
	b := &SourceBranch{owner: s, attempt: t, result: unavailable}
	keep := false
	defer func() {
		if !keep {
			b.Close()
		}
	}()
	l, r, err := wasm.BuildSourceLedger(m, a, function, features, wasm.SourceLedgerLimits{BodyBytes: 11, Metadata: 256, Locals: 2, Events: 7, Values: 10, Operands: 32, Stack: 2, Blocks: 5, Edges: 5, ControlDepth: 2, Work: min(4096, s.sourceWork)})
	s.sourceWork -= r.Work
	if err != nil || r.Coverage != wasm.SourceContractsComplete {
		l.Close()
		b.result = sourceLeafUnavailable(r.Reason, "framed source contracts incomplete")
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
	if !valid || len(ft.Params) != 2 || len(ft.Results) != 1 || ft.Params[0] != wasm.I32 || ft.Params[1] != wasm.I32 || ft.Results[0] != wasm.I32 {
		return nil
	}
	if l.EventCount() != 7 || l.ValueCount() != 10 || l.BlockCount() != 5 || l.EdgeCount() != 5 || l.LocalCount() != 2 {
		return nil
	}
	b.entry, b.then, b.otherwise, b.join, b.exit = l.Event(0).Block, l.Event(2).Block, l.Event(4).Block, l.Event(6).Block, l.ExitBlock()
	var seen [5]bool
	for _, index := range []int{b.entry, b.then, b.otherwise, b.join, b.exit} {
		if index < 0 || index >= 5 || seen[index] {
			return nil
		}
		seen[index] = true
	}
	body := m.Code[function].BodyBytes
	if l.Event(0).Index != uint32(body[1]) || l.Event(2).Index != uint32(body[5]) || l.Event(4).Index != uint32(body[8]) ||
		l.Input(1, 0) != l.EntryLocal(int(body[1])) || l.Output(2, 0) != l.BlockParameter(b.then, int(body[5])) || l.Output(4, 0) != l.BlockParameter(b.otherwise, int(body[8])) ||
		l.Input(6, 0) != l.BlockParameter(b.join, 2) || !l.Event(6).FunctionEnd {
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
			if e.Arm != wasm.SourceThen || e.Condition != l.EntryLocal(int(body[1])) || e.ArgumentCount != 2 {
				return nil
			}
		case e.From == b.entry && e.To == b.otherwise:
			bit = 2
			if e.Arm != wasm.SourceElse || e.Condition != l.EntryLocal(int(body[1])) || e.ArgumentCount != 2 {
				return nil
			}
		case (e.From == b.then || e.From == b.otherwise) && e.To == b.join:
			bit = 4
			if e.From == b.otherwise {
				bit = 8
			}
			if e.Arm != wasm.SourceFallthrough || e.ArgumentCount != 3 {
				return nil
			}
		case e.From == b.join && e.To == b.exit:
			bit = 16
			if e.Arm != wasm.SourceFallthrough || e.ArgumentCount != 1 {
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
		b.result = sourceLeafUnavailable(regalloccheck.ResourceLimit, "framed journal work exhausted")
		return nil
	}
	s.sourceWork -= 128
	b.journal = regalloccheck.NewEmissionJournal(regalloccheck.JournalLimits{Events: 64, Spans: 1, Transactions: 1, Work: 128})
	b.journal.BeginEmission(0)
	keep = true
	return b
}

func (b *SourceBranch) ObserveEffect(e regalloccheck.Effect) { b.journal.ObserveEffect(e) }
func (b *SourceBranch) ObserveGPWrites(mask uint32)          { b.journal.ObserveGPWrites(mask) }
func (b *SourceBranch) EndEmission(length int)               { b.oldLen = length; b.journal.EndEmission(length) }
func (b *SourceBranch) Close() {
	if b == nil || b.owner == nil {
		return
	}
	owner, t, result := b.owner, b.attempt, b.result
	if b.journal != nil {
		b.journal.Close()
	}
	// FinishWorker can retire this token before scratch reuse. The journal still
	// belongs here; a later worker/context must never receive this old report.
	if t != nil && t.owner == owner && owner.sourceAttempt == t && t.ledger == b.ledger {
		function := t.function
		EndSourceAttempt(t)
		codegen.RecordSourceResult(owner.sourceContext, function, result)
	}
	*b = SourceBranch{}
}

func (b *SourceBranch) Verify(code []byte, arm bool) regalloccheck.Result {
	if b == nil || b.owner == nil || b.attempt == nil || b.attempt.owner != b.owner || b.owner.sourceAttempt != b.attempt || b.ledger == nil || b.attempt.ledger != b.ledger {
		return sourceLeafUnavailable(regalloccheck.InvalidGraph, "framed source attempt closed")
	}
	if b.done {
		return b.result
	}
	b.done = true
	r := sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "unsupported framed native recipe")
	defer func() { b.result = r }()
	if len(code) != b.oldLen || len(code) > 128 {
		return r
	}
	decodeWork := 1024
	if b.loop {
		decodeWork = 4096
	}
	if b.owner.sourceWork < decodeWork {
		r = sourceLeafUnavailable(regalloccheck.ResourceLimit, "framed native work exhausted")
		return r
	}
	b.owner.sourceWork -= decodeWork
	jr := b.journal.Finalize(b.oldLen, len(code), nil)
	if jr.State != regalloccheck.JournalReady {
		r = sourceLeafUnavailable(jr.Reason, jr.Message)
		return r
	}
	var decoded sourceBranchRecipe
	var ok bool
	switch {
	case b.loop && !arm:
		decoded, ok = decodeSourceLoopAMD64(code)
	case b.loop:
		return r
	case arm:
		decoded, ok = decodeSourceBranchARM64(code)
	default:
		decoded, ok = decodeSourceBranchAMD64(code)
	}
	if !ok {
		return r
	}
	observed := 0
	for _, in := range decoded.instructions {
		if in.copy {
			e, found := b.journal.Event(observed)
			observed++
			if !found || e.Kind != regalloccheck.JournalEffect || e.Effect != in.effect {
				return r
			}
		}
		writes := in.writes
		if in.journalWrites != 0 {
			writes = in.journalWrites
		}
		if writes != 0 {
			e, found := b.journal.Event(observed)
			observed++
			if !found || e.Kind != regalloccheck.JournalGPWrites || e.GPWrites != writes {
				return r
			}
		}
	}
	if observed != jr.Events {
		return r
	}
	// Complete opcode/observer coverage precedes semantic failure. Branch sites
	// themselves are independently read from final bytes, not callback Len.
	if decoded.invalid != "" {
		r = regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch, Message: decoded.invalid}
		return r
	}
	l := b.ledger
	g := regalloccheck.Graph{Widths: make([]uint8, l.ValueCount()), Blocks: make([]regalloccheck.Block, l.BlockCount()), Entry: b.entry,
		Inputs: make([]regalloccheck.Binding, l.LocalCount())}
	for i := range g.Inputs {
		g.Inputs[i] = regalloccheck.Binding{Location: leafReg(uint8(i)), Value: regalloccheck.ValueID(l.EntryLocal(i))}
	}
	conditionEvent := 1
	if b.loop {
		conditionEvent = 6
	}
	for i := range g.Widths {
		g.Widths[i] = 4
	}
	for _, in := range decoded.instructions {
		block := b.entry
		switch in.section {
		case 1:
			block = b.then
		case 2:
			block = b.otherwise
		case 3:
			block = b.join
		case 4:
			block = b.exit
		}
		ops := &g.Blocks[block].Operations
		if in.copy {
			e := in.effect
			if e.Dst.Bank == regalloccheck.GP && e.Size == 4 {
				e.ClearTo = 8
			}
			*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
		} else if in.condition {
			*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Use, Location: in.effect.Src, Value: regalloccheck.ValueID(l.Input(conditionEvent, 0)), Where: "native source condition"})
		} else if in.returns {
			*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Use, Location: leafReg(0), Value: regalloccheck.ValueID(l.BlockParameter(b.exit, 0)), Where: "framed native return"})
		}
		// Copy already accounts for destination writes and partial widths.
		if !in.copy && in.writes != 0 {
			for reg := uint8(0); reg < 32; reg++ {
				if in.writes&(1<<reg) != 0 {
					*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: leafReg(reg), Size: 8}})
				}
			}
		}
	}
	for i := 0; i < l.EdgeCount(); i++ {
		e := l.Edge(i)
		if b.loop {
			arguments := 1
			if e.To == b.then {
				arguments = 3
			}
			edge := regalloccheck.Edge{To: e.To, Parameters: make([]regalloccheck.Parameter, arguments)}
			for j := 0; j < arguments; j++ {
				location := decoded.loopLocals[j]
				if e.From == b.join {
					location = leafReg(0)
				}
				edge.Parameters[j] = regalloccheck.Parameter{From: regalloccheck.ValueID(l.EdgeArgument(i, j)), To: regalloccheck.ValueID(l.BlockParameter(e.To, j)), Location: location}
			}
			g.Blocks[e.From].Edges = append(g.Blocks[e.From].Edges, edge)
			continue
		}
		// The exact source skeleton has one live operand per outgoing arm and
		// one live result at the join. Other local phis are dead; preserving
		// their original pins would forbid valid register reuse. Use only the
		// authoritative selected source IDs and the final decoded carriers.
		argument, location := 0, leafReg(0)
		switch {
		case e.From == b.entry && e.To == b.then:
			argument, location = int(l.Event(2).Index), decoded.thenSource
		case e.From == b.entry && e.To == b.otherwise:
			argument, location = int(l.Event(4).Index), decoded.elseSource
		case e.From == b.then:
			argument, location = 2, decoded.thenResult
		case e.From == b.otherwise:
			argument, location = 2, decoded.elseResult
		}
		edge := regalloccheck.Edge{To: e.To, Parameters: []regalloccheck.Parameter{{From: regalloccheck.ValueID(l.EdgeArgument(i, argument)), To: regalloccheck.ValueID(l.BlockParameter(e.To, argument)), Location: location}}}
		g.Blocks[e.From].Edges = append(g.Blocks[e.From].Edges, edge)
	}
	if b.owner.sourceWork < 1 {
		r = sourceLeafUnavailable(regalloccheck.ResourceLimit, "framed graph work exhausted")
		return r
	}
	r = g.Verify(regalloccheck.Limits{Blocks: 5, Values: 13, Operations: 128, Facts: 4096, Work: min(65536, b.owner.sourceWork)})
	b.owner.sourceWork -= r.Work
	return r
}

// sourceBranchBody bounds the family before building any source metadata.
func sourceBranchBody(body []byte) bool {
	return len(body) == 11 && body[0] == 0x20 && body[1] < 2 && body[2] == 0x04 && body[3] == 0x7f &&
		body[4] == 0x20 && body[5] < 2 && body[6] == 0x05 && body[7] == 0x20 && body[8] < 2 && body[9] == 0x0b && body[10] == 0x0b
}
