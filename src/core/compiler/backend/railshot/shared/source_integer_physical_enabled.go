//go:build wago_regalloccheck

package shared

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// SourceIntegerPhysical verifies a whole private function image against its
// original integer DAG. A successful function proof is provisional: it does not
// authenticate subsequent module layout, sharing, entry maps or relocations.
// It never publishes an original-source MachineVerified result by itself.
type SourceIntegerPhysical struct {
	materialization *SourceMaterializationJournal
	journal         *regalloccheck.EmissionJournal
	result          regalloccheck.Result
	admitted, done  bool
}

func BeginSourceIntegerPhysical(j *SourceMaterializationJournal, admit bool) *SourceIntegerPhysical {
	if !sourceMaterializationOwnerValid(j) || j.physical != nil || !sourcePlanAlive(j.plan) {
		return nil
	}
	if !j.charge(1, 1) {
		return nil
	}
	p := &SourceIntegerPhysical{materialization: j, result: sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "function physical recipe unavailable")}
	j.physical = p
	if !admit {
		return p
	}
	ft, ok := j.plan.attempt.module.LocalFuncType(j.plan.attempt.function)
	l := j.plan.attempt.ledger
	if !ok || len(ft.Params) > 8 || len(ft.Results) != 1 || l.LocalCount() != len(ft.Params) {
		return p
	}
	if ft.Results[0] != wasm.I32 && ft.Results[0] != wasm.I64 {
		return p
	}
	// Literals and declared-local zero need separate encoded-bit recipes. Do not
	// accuse an otherwise correct function merely because those are unimplemented.
	if !j.charge(41*l.EventCount()+l.ValueCount()+1, 0) {
		return p
	}
	for i := 1; i <= l.ValueCount(); i++ {
		typ := l.Value(wasm.SourceValueID(i)).Type
		if typ != wasm.I32 && typ != wasm.I64 {
			return p
		}
	}
	for i := 0; i < l.EventCount(); i++ {
		e := l.Event(i)
		if e.Kind == wasm.InstrI32Const || e.Kind == wasm.InstrI64Const {
			return p
		}
		matched := false
		for _, rule := range [...]SourceRecipeRule{SourceRuleAlias, SourceRuleDrop, SourceRuleNop, SourceRuleIntegerBinary, SourceRuleExit} {
			if sourceRuleMatches(rule, e, l, i) {
				matched = true
				break
			}
		}
		if !matched {
			return p
		}
	}
	// Reserve all bounded observation pools/work before their first allocation.
	if !j.charge(4096, 2048) {
		return p
	}
	p.journal = regalloccheck.NewEmissionJournal(regalloccheck.JournalLimits{Events: 1024, Spans: 1, Transactions: 1, Work: 4096})
	p.journal.BeginEmission(0)
	p.admitted = true
	return p
}
func (p *SourceIntegerPhysical) ObserveEffect(e regalloccheck.Effect) {
	if p != nil && p.admitted {
		p.journal.ObserveEffect(e)
	}
}

// ObservationReady authorizes installing the bounded forwarding/restore
// closures covered by the admitted journal's fixed pool reservation.
func (p *SourceIntegerPhysical) ObservationReady() bool {
	return p != nil && p.admitted && !p.done && sourceMaterializationOwnerValid(p.materialization)
}
func (p *SourceIntegerPhysical) ObserveGPWrites(w uint32) {
	if p != nil && p.admitted {
		p.journal.ObserveGPWrites(w)
	}
}
func (p *SourceIntegerPhysical) EndEmission(length int) {
	if p != nil && p.admitted {
		p.journal.EndEmission(length)
	}
}
func (p *SourceIntegerPhysical) Result() regalloccheck.Result {
	if p == nil {
		return sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "function physical recipe unavailable")
	}
	return p.result
}
func (p *SourceIntegerPhysical) Close() {
	if p == nil {
		return
	}
	if p.journal != nil {
		p.journal.Close()
	}
	*p = SourceIntegerPhysical{}
}

type sourceIntegerInstruction struct {
	leafInstruction
	start, end int
}

// Only balanced zero-frame SubRsp/body/AddRsp/Ret, register MOV and RR ALU are
// admitted here. Entire-image decoding supplies all carriers and protects the
// private frame origin and callee/module registers independently of the emitter.
func decodeSourceIntegerAMD64(code []byte, out []sourceIntegerInstruction) ([]sourceIntegerInstruction, bool) {
	if len(code) < 15 || len(code) > 2048 || len(out) != 0 || cap(out) < 256 {
		return nil, false
	}
	start, end := code[:7], code[len(code)-8:]
	if start[0] != 0x48 || start[1] != 0x81 || start[2] != 0xec || binary.LittleEndian.Uint32(start[3:]) != 0 || end[0] != 0x48 || end[1] != 0x81 || end[2] != 0xc4 || binary.LittleEndian.Uint32(end[3:]) != 0 || end[7] != 0xc3 {
		return nil, false
	}
	out = append(out, sourceIntegerInstruction{leafInstruction: leafInstruction{writes: 1 << 4}, start: 0, end: 7})
	allowed := func(r uint8) bool { return r < 12 && r != 3 && r != 4 && r != 5 }
	for pc := 7; pc < len(code)-8; {
		if len(out) >= 254 {
			return nil, false
		}
		begin := pc
		rex := byte(0)
		if code[pc] >= 0x40 && code[pc] <= 0x4f {
			rex = code[pc]
			pc++
			if rex&2 != 0 {
				return nil, false
			}
		}
		if pc > len(code)-10 {
			return nil, false
		}
		op, modrm := code[pc], code[pc+1]
		pc += 2
		if modrm&0xc0 != 0xc0 {
			return nil, false
		}
		dst, src := uint8(modrm&7)|(rex&1)<<3, uint8((modrm>>3)&7)|((rex>>2)&1)<<3
		if !allowed(dst) || !allowed(src) {
			return nil, false
		}
		width := uint8(4)
		if rex&8 != 0 {
			width = 8
		}
		in := leafInstruction{writes: 1 << dst, width: width, dst: leafReg(dst)}
		if op == 0x89 {
			in.copy = true
			in.effect = regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: leafReg(dst), Src: leafReg(src), Size: int(width)}
		} else {
			in.kind = leafOperator(op, width == 8)
			if in.kind == wasm.InstrInvalid {
				return nil, false
			}
			in.arithmetic = true
			in.left, in.right = leafReg(dst), leafReg(src)
		}
		out = append(out, sourceIntegerInstruction{leafInstruction: in, start: begin, end: pc})
	}
	out = append(out, sourceIntegerInstruction{leafInstruction: leafInstruction{writes: 1 << 4}, start: len(code) - 8, end: len(code) - 1}, sourceIntegerInstruction{leafInstruction: leafInstruction{writes: 1 << 4, returns: true}, start: len(code) - 1, end: len(code)})
	return out, true
}

func (p *SourceIntegerPhysical) VerifyAMD64(code []byte) (result regalloccheck.Result) {
	result = sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "unsupported whole-function integer physical recipe")
	if p == nil || !sourceMaterializationOwnerValid(p.materialization) {
		return sourceLeafUnavailable(regalloccheck.InvalidGraph, "physical source owner retired")
	}
	if p.done {
		// A caller must not reuse a successful proof for a mutated or different
		// byte slice. No owned final-module byte certificate exists at this stage.
		reused := sourceLeafUnavailable(regalloccheck.InvalidGraph, "function physical proof consumed more than once")
		if p.result.Verdict == regalloccheck.Verified {
			p.result = reused
		}
		// Earlier quota refusals and failures remain historical and may not be
		// hidden by a later misuse when the owner publishes its close report.
		return reused
	}
	p.done = true
	defer func() { p.result = result }()
	j := p.materialization
	mr := SourceMaterializationStatus(j)
	if mr.Reason == regalloccheck.ResourceLimit {
		return sourceLeafUnavailable(mr.Reason, "materialization budget exhausted")
	}
	if !p.admitted || mr.Readiness != SourceMaterializationRecordingClosed || len(code) != mr.Length || len(code) > 2048 {
		return result
	}
	l := j.plan.attempt.ledger
	// Fixed conservative pool-entry credits bound decoder/model and graph facts
	// before allocation. Work for actual graph passes is charged separately.
	if !j.charge(4096+l.ValueCount(), 16384+l.ValueCount()) {
		return sourceLeafUnavailable(regalloccheck.ResourceLimit, "physical construction credits exhausted")
	}
	jr := p.journal.Finalize(mr.Length, len(code), nil)
	if jr.State != regalloccheck.JournalReady {
		return sourceLeafUnavailable(jr.Reason, jr.Message)
	}
	instructions, ok := decodeSourceIntegerAMD64(code, make([]sourceIntegerInstruction, 0, 256))
	if !ok {
		return result
	}
	observed, receipt := 0, 0
	match := func(in sourceIntegerInstruction) bool {
		if in.copy {
			e, ok := p.journal.Event(observed)
			observed++
			if !ok || e.Kind != regalloccheck.JournalEffect || e.Effect != in.effect {
				return false
			}
		}
		if in.writes != 0 {
			e, ok := p.journal.Event(observed)
			observed++
			if !ok || e.Kind != regalloccheck.JournalGPWrites || e.GPWrites != in.writes {
				return false
			}
		}
		return true
	}
	// Authenticate complete byte/observer/receipt coverage before classifying any
	// semantic mismatch. No unmatched arithmetic can become a dynamic Define.
	for _, in := range instructions {
		if !match(in) {
			return result
		}
		if in.arithmetic {
			if receipt >= mr.Receipts {
				return result
			}
			r, ok := SourceMaterializationReceiptAt(j, receipt)
			receipt++
			if !ok {
				return sourceLeafUnavailable(SourceMaterializationStatus(j).Reason, "receipt read unavailable")
			}
			if r.Start != in.start || r.End != in.end {
				return result
			}
		}
	}
	if observed != jr.Events || receipt != mr.Receipts {
		return result
	}
	g := regalloccheck.Graph{Widths: make([]uint8, l.ValueCount()), Inputs: make([]regalloccheck.Binding, l.LocalCount()), Blocks: []regalloccheck.Block{{Operations: make([]regalloccheck.Operation, 0, 1024)}}}
	for i := range g.Widths {
		g.Widths[i] = 4
		if l.Value(wasm.SourceValueID(i+1)).Type == wasm.I64 {
			g.Widths[i] = 8
		}
	}
	// All parameters are integer, so type-position maps independently to these
	// eight private ABI GP carriers. No current allocator home is an entry fact.
	for i, r := range []uint8{0, 1, 2, 8, 9, 10, 11, 7} {
		if i < len(g.Inputs) {
			g.Inputs[i] = regalloccheck.Binding{Location: leafReg(r), Value: regalloccheck.ValueID(l.EntryLocal(i))}
		}
	}
	last := l.EventCount() - 1
	if last < 0 || !l.Event(last).FunctionEnd || l.Event(last).InputCount != 1 {
		return result
	}
	output := regalloccheck.ValueID(l.Input(last, 0))
	receipt = 0
	for _, in := range instructions {
		ops := &g.Blocks[0].Operations
		if in.arithmetic {
			r, ok := SourceMaterializationReceiptAt(j, receipt)
			receipt++
			if !ok {
				return sourceLeafUnavailable(SourceMaterializationStatus(j).Reason, "receipt read unavailable")
			}
			if r.Kind != in.kind || r.Width != in.width {
				return regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch, Message: "final integer operator or width differs from original producer"}
			}
			*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Use, Value: regalloccheck.ValueID(r.Inputs[0]), Location: in.left, Where: "original integer producer left"}, regalloccheck.Operation{Kind: regalloccheck.Use, Value: regalloccheck.ValueID(r.Inputs[1]), Location: in.right, Where: "original integer producer right"}, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: in.dst, Size: 8}}, regalloccheck.Operation{Kind: regalloccheck.Define, Value: regalloccheck.ValueID(r.Output), Location: in.dst})
		} else if in.copy {
			e := in.effect
			if e.Size == 4 {
				e.ClearTo = 8
			}
			*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
		} else if in.returns {
			*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Use, Value: output, Location: leafReg(0), Where: "original whole-function return"})
		}
	}
	s := j.plan.attempt.owner
	if s.sourceWork < 1 {
		return sourceLeafUnavailable(regalloccheck.ResourceLimit, "physical graph credits exhausted")
	}
	result = g.Verify(regalloccheck.Limits{Blocks: 1, Values: 4096, Operations: 1024, Facts: 8192, Work: min(65536, s.sourceWork)})
	s.sourceWork -= result.Work
	if result.Verdict == regalloccheck.Verified {
		result.Message = "whole-function integer byte/provenance proof complete; final-module coverage pending"
	}
	return result
}
