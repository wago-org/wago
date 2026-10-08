//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// SourceLeaf admits exactly two integer parameters, two local.gets, one
// commutative integer operation, and the implicit function end. It owns a
// whole-leaf composite journal, not individual callback instruction coordinates.
// Final bytes independently supply every operand and transfer; their COMPLETE
// ordered effect/write sequence must agree with the encoder journal.
// Storage uses conservative pool-entry credits, not bytes. Each attempt reserves
// 2048 historical credits for bounded validator metadata, ledger pools, journal,
// decoded recipes, operations and both commutative graph fact passes. Budgets
// bound compilation work/storage history; individual struct byte sizes differ.
type SourceLeaf struct {
	owner               *ScalarState
	attempt             *SourceAttempt
	journal             *regalloccheck.EmissionJournal
	width               uint8
	kind                wasm.InstrKind
	left, right, result regalloccheck.ValueID
	widths              []uint8
	oldLen              int
	resultReport        regalloccheck.Result
	done                bool
}

func sourceLeafUnavailable(reason regalloccheck.FailureReason, message string) regalloccheck.Result {
	return regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: reason, Message: message}
}

// BeginSourceLeaf never changes ordinary or shared-pilot admission. The backend
// supplies whether it has an internal register-ABI fallback entry, without a
// wrapper, traps or extensions. Source identity comes exclusively from validation.
func BeginSourceLeaf(s *ScalarState, m *wasm.Module, function int, admit bool) *SourceLeaf {
	if s == nil {
		return nil
	}
	a, features, ok := codegen.ValidatedSourceContext(s.sourceContext, m)
	if !ok || function < 0 || function >= len(m.Code) {
		return nil
	}
	unavailable := sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "no admitted source/machine recipe")
	codegen.RecordSourceResult(s.sourceContext, function, unavailable)
	// Charge even failed admission. Zero must never reach APIs where it selects
	// a default budget. Reserved storage is historical, and is never refunded.
	if s.sourceWork < 32 {
		codegen.RecordSourceResult(s.sourceContext, function, sourceLeafUnavailable(regalloccheck.ResourceLimit, "source compilation work exhausted"))
		return nil
	}
	s.sourceWork -= 32
	body := m.Code[function].BodyBytes
	ft, valid := m.LocalFuncType(function)
	if !admit || !valid || len(ft.Params) != 2 || len(ft.Results) != 1 ||
		ft.Params[0] != ft.Params[1] || ft.Params[0] != ft.Results[0] ||
		(ft.Params[0] != wasm.I32 && ft.Params[0] != wasm.I64) ||
		len(body) != 6 || body[0] != 0x20 || body[1] != 0 || body[2] != 0x20 || body[3] != 1 || body[5] != 0x0b ||
		len(m.Code[function].Locals.Runs) != 0 {
		return nil
	}
	if s.sourceStorage < 2048 || s.sourceWork < 1 {
		codegen.RecordSourceResult(s.sourceContext, function, sourceLeafUnavailable(regalloccheck.ResourceLimit, "source compilation storage exhausted"))
		return nil
	}
	s.sourceStorage -= 2048
	t := BeginSourceAttempt(s, m, function)
	if t == nil {
		return nil
	}
	leaf := &SourceLeaf{owner: s, attempt: t, resultReport: unavailable}
	// The caller already installed function unwind cleanup. This local scope
	// handles failures before ownership is returned to the backend.
	keep := false
	defer func() {
		if !keep {
			leaf.Close()
		}
	}()
	ledger, res, err := wasm.BuildSourceLedger(m, a, function, features, wasm.SourceLedgerLimits{
		BodyBytes: 6, Metadata: 256, Locals: 2, Events: 4, Values: 4, Operands: 16, Stack: 2,
		Blocks: 2, Edges: 2, ControlDepth: 1, Work: min(4096, s.sourceWork)})
	s.sourceWork -= res.Work
	if err != nil || res.Coverage != wasm.SourceContractsComplete {
		ledger.Close()
		leaf.resultReport = sourceLeafUnavailable(res.Reason, "source contracts incomplete")
		return nil
	}
	if !AttachSourceLedger(t, ledger) {
		ledger.Close()
		return nil
	}
	if ledger.LocalCount() != 2 || ledger.EventCount() != 4 || ledger.ValueCount() != 4 {
		return nil
	}
	e0, e1, e2, e3 := ledger.Event(0), ledger.Event(1), ledger.Event(2), ledger.Event(3)
	if e0.Kind != wasm.InstrLocalGet || e0.Index != 0 || e0.InputCount != 0 || e0.OutputCount != 1 ||
		e1.Kind != wasm.InstrLocalGet || e1.Index != 1 || e1.InputCount != 0 || e1.OutputCount != 1 ||
		e2.InputCount != 2 || e2.OutputCount != 1 || !e3.FunctionEnd || !e3.Terminal || e3.InputCount != 1 || e3.OutputCount != 0 ||
		e0.Unreachable || e1.Unreachable || e2.Unreachable || e3.Unreachable {
		return nil
	}
	switch e2.Kind {
	case wasm.InstrI32Add, wasm.InstrI32And, wasm.InstrI32Or, wasm.InstrI32Xor,
		wasm.InstrI64Add, wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Xor:
	default:
		return nil
	}
	leaf.width = 4
	if ft.Params[0] == wasm.I64 {
		leaf.width = 8
	}
	leaf.left, leaf.right, leaf.result = regalloccheck.ValueID(ledger.EntryLocal(0)), regalloccheck.ValueID(ledger.EntryLocal(1)), regalloccheck.ValueID(ledger.Output(2, 0))
	if ledger.Output(0, 0) != ledger.EntryLocal(0) || ledger.Output(1, 0) != ledger.EntryLocal(1) ||
		regalloccheck.ValueID(ledger.Input(2, 0)) != leaf.left || regalloccheck.ValueID(ledger.Input(2, 1)) != leaf.right ||
		regalloccheck.ValueID(ledger.Input(3, 0)) != leaf.result {
		return nil
	}
	leaf.kind = e2.Kind
	leaf.widths = make([]uint8, ledger.ValueCount())
	for i := range leaf.widths {
		v := ledger.Value(wasm.SourceValueID(i + 1))
		if v.Type != ft.Params[0] {
			return nil
		}
		leaf.widths[i] = leaf.width
	}
	if s.sourceWork < 128 {
		leaf.resultReport = sourceLeafUnavailable(regalloccheck.ResourceLimit, "source journal work exhausted")
		return nil
	}
	// Fixed reservation bounds journal allocation/work before observations. The
	// remainder is not refunded even for an abandoned or retried function.
	s.sourceWork -= 128
	leaf.journal = regalloccheck.NewEmissionJournal(regalloccheck.JournalLimits{Events: 64, Spans: 1, Transactions: 1, Work: 128})
	leaf.journal.BeginEmission(0)
	keep = true
	return leaf
}

func (l *SourceLeaf) ObserveEffect(e regalloccheck.Effect) { l.journal.ObserveEffect(e) }
func (l *SourceLeaf) ObserveGPWrites(mask uint32)          { l.journal.ObserveGPWrites(mask) }
func (l *SourceLeaf) EndEmission(length int)               { l.oldLen = length; l.journal.EndEmission(length) }

// Close follows restoration of nested and source observers. It releases all
// exclusive pools before recording a by-value report, including every unwind.
func (l *SourceLeaf) Close() {
	if l == nil || l.owner == nil {
		return
	}
	owner, t := l.owner, l.attempt
	if l.journal != nil {
		l.journal.Close()
	}
	function := t.function
	EndSourceAttempt(t)
	codegen.RecordSourceResult(owner.sourceContext, function, l.resultReport)
	*l = SourceLeaf{}
}

type leafInstruction struct {
	effect              regalloccheck.Effect
	copy                bool
	writes              uint32
	width               uint8
	kind                wasm.InstrKind
	dst, left, right    regalloccheck.Location
	arithmetic, returns bool
	neutralFrame        bool // known same-length ARM zero-frame rewrite to NOP
}

// Verify consumes a bounded, independently decoded whole final image. Decoder
// work is reserved before reading bytes, with at most 32 instructions/128 bytes.
// This first recipe admits only identity-length finalizations and a zero frame.
func (l *SourceLeaf) Verify(code []byte, arm bool) regalloccheck.Result {
	if l == nil || l.owner == nil {
		return sourceLeafUnavailable(regalloccheck.InvalidGraph, "source attempt closed")
	}
	if l.done {
		return l.resultReport
	}
	l.done = true
	result := sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "unsupported final native recipe")
	defer func() { l.resultReport = result }()
	if len(code) != l.oldLen || len(code) > 128 || l.owner.sourceWork < 1024 {
		if l.owner.sourceWork < 1024 {
			result = sourceLeafUnavailable(regalloccheck.ResourceLimit, "native recipe work exhausted")
		}
		return result
	}
	l.owner.sourceWork -= 1024
	jr := l.journal.Finalize(l.oldLen, len(code), nil)
	if jr.State != regalloccheck.JournalReady {
		result = sourceLeafUnavailable(jr.Reason, jr.Message)
		return result
	}
	var storage [32]leafInstruction
	var instructions []leafInstruction
	var ok bool
	if arm {
		instructions, ok = decodeSourceLeafARM64(code, storage[:0])
	} else {
		instructions, ok = decodeSourceLeafAMD64(code, storage[:0])
	}
	if !ok {
		return result
	}
	opIndex, arithmetic, returns := 0, 0, 0
	match := func(e regalloccheck.Effect, writes uint32, effect bool) bool {
		observed, found := l.journal.Event(opIndex)
		if !found {
			return false
		}
		opIndex++
		if effect {
			return observed.Kind == regalloccheck.JournalEffect && observed.Effect == e
		}
		return observed.Kind == regalloccheck.JournalGPWrites && observed.GPWrites == writes
	}
	for _, in := range instructions {
		if in.copy && !match(in.effect, 0, true) {
			return result
		}
		if in.writes != 0 && !match(regalloccheck.Effect{}, in.writes, false) {
			return result
		}
		if in.arithmetic {
			arithmetic++
		}
		if in.returns {
			returns++
		}
	}
	// Establish whole-image and observer coverage before classifying a failed
	// semantic contract. Extra arithmetic belongs to an unsupported recipe.
	if opIndex != jr.Events || arithmetic != 1 || returns != 1 {
		return result
	}
	model := regalloccheck.Graph{Widths: l.widths, Blocks: []regalloccheck.Block{{}}}
	// The internal ABI uses AMD64 RAX/RCX or ARM64 X0/X1; never wrapper homes.
	model.Inputs = []regalloccheck.Binding{{Location: leafReg(0), Value: l.left}, {Location: leafReg(1), Value: l.right}}
	uses := -1
	for _, in := range instructions {
		ops := &model.Blocks[0].Operations
		if in.arithmetic {
			if in.kind != l.kind || in.width != l.width {
				result = regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch, Message: "native operator or width differs from source"}
				return result
			}
			uses = len(*ops)
			*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Use, Value: l.left, Location: in.left, Where: "native arithmetic left"}, regalloccheck.Operation{Kind: regalloccheck.Use, Value: l.right, Location: in.right, Where: "native arithmetic right"},
				regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: in.dst, Size: 8}}, regalloccheck.Operation{Kind: regalloccheck.Define, Value: l.result, Location: in.dst})
		} else if in.copy {
			// A 32-bit architectural move clears the high register half even
			// where the historical Copy observer only describes its low bytes.
			e := in.effect
			if e.Dst.Bank == regalloccheck.GP && e.Size == 4 {
				e.ClearTo = 8
			}
			*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
		} else if in.returns {
			*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Use, Value: l.result, Location: leafReg(0), Where: "native return"})
		} else if !in.neutralFrame {
			for r := uint8(0); r < 32; r++ {
				if in.writes&(uint32(1)<<r) != 0 {
					*ops = append(*ops, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Register(regalloccheck.GP, r), Size: 8}})
				}
			}
		}
	}
	if l.owner.sourceWork < 1 {
		result = sourceLeafUnavailable(regalloccheck.ResourceLimit, "graph work exhausted")
		return result
	}
	limits := regalloccheck.Limits{Blocks: 1, Values: 4, Operations: 128, Facts: 256, Work: min(4096, l.owner.sourceWork)}
	result = model.Verify(limits)
	l.owner.sourceWork -= result.Work
	if result.Verdict == regalloccheck.Rejected && l.owner.sourceWork > 0 {
		// All admitted operators commute. Only the two independent input Uses
		// exchange identities; duplicate/wrong input carriers still fail both.
		ops := model.Blocks[0].Operations
		ops[uses].Value, ops[uses+1].Value = l.right, l.left
		limits.Work = min(4096, l.owner.sourceWork)
		priorWork := result.Work
		result = model.Verify(limits)
		l.owner.sourceWork -= result.Work
		result.Work += priorWork
	}
	return result
}
