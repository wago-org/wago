//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"strconv"
)

// SourceEventContract is an immutable, bounded copy of original source metadata.
// It authorizes no machine definition or encoded operand correspondence.
type SourceEventContract struct {
	Event           wasm.SourceEvent
	Inputs, Outputs [sourcePlanMaxOperands]wasm.SourceValue
}

func SourcePlanEventContract(t SourcePlanToken, e SourceEventRef) (SourceEventContract, bool) {
	p := t.p
	if !sourcePlanAlive(p) {
		return SourceEventContract{}, false
	}
	if e.p != p || e.index != p.next || e.index < 0 || e.index >= p.attempt.ledger.EventCount() {
		return SourceEventContract{}, p.fail(regalloccheck.InvalidGraph)
	}
	l := p.attempt.ledger
	event := l.Event(e.index)
	if event.InputCount > sourcePlanMaxOperands || event.OutputCount > sourcePlanMaxOperands {
		return SourceEventContract{}, p.fail(regalloccheck.ResourceLimit)
	}
	if !p.charge(1+event.InputCount+event.OutputCount, 0) {
		return SourceEventContract{}, false
	}
	c := SourceEventContract{Event: event}
	for i := 0; i < event.InputCount; i++ {
		c.Inputs[i] = l.Value(l.Input(e.index, i))
	}
	for i := 0; i < event.OutputCount; i++ {
		c.Outputs[i] = l.Value(l.Output(e.index, i))
	}
	return c, true
}

func SourceNodeContract(t SourcePlanToken, n SourceNodeRef) (wasm.SourceValue, bool) {
	if !sourcePlanAlive(t.p) {
		return wasm.SourceValue{}, false
	}
	if !t.p.nodeValid(n) {
		return wasm.SourceValue{}, t.p.fail(regalloccheck.InvalidGraph)
	}
	if !t.p.charge(1, 0) {
		return wasm.SourceValue{}, false
	}
	return t.p.attempt.ledger.Value(n.id), true
}

// Adapter credits share historical worker quotas. Allocations and every visited
// physical node must be charged before use, including skipped deferred children.
func ChargeSourcePlanAdapter(t SourcePlanToken, work, storage int) bool {
	return sourcePlanAlive(t.p) && t.p.charge(work, storage)
}

// Failure can only make accounting incomplete. The first reason is sticky.
func FailSourcePlanAdapter(t SourcePlanToken, reason regalloccheck.FailureReason) {
	if !sourcePlanAlive(t.p) {
		return
	}
	switch reason {
	case regalloccheck.InvalidGraph, regalloccheck.UnsupportedOperation, regalloccheck.ResourceLimit:
		t.p.fail(reason)
	default:
		t.p.fail(regalloccheck.InvalidGraph)
	}
}
func SourcePlanStatus(t SourcePlanToken) SourcePlanResult { return t.p.result() }

// IntegerSourcePlan owns one bounded original-source attempt for a real fallback
// adapter. Completion is source accounting only, never machine verification.
type IntegerSourcePlan struct {
	attempt        *SourceAttempt
	Token          SourcePlanToken
	Locals, Events int
	result         SourcePlanResult
}

func BeginIntegerSourcePlan(s *ScalarState, m *wasm.Module, function int, admit bool) *IntegerSourcePlan {
	if s == nil {
		return nil
	}
	a, features, ok := codegen.ValidatedSourceContext(s.sourceContext, m)
	if !ok || function < 0 || function >= len(m.Code) {
		return nil
	}
	report := func(reason regalloccheck.FailureReason) {
		codegen.RecordSourceAccountingResult(s.sourceContext, function, sourceLeafUnavailable(reason, "integer source accounting unavailable; physical verification pending"))
	}
	if s.sourceWork < 32 {
		report(regalloccheck.ResourceLimit)
		return nil
	}
	s.sourceWork -= 32
	if !admit || s.sourceAttempt != nil {
		return nil
	}
	// Fixed conservative credits bound ledger/validator pools before allocation.
	// Counts below are pool-entry credits, not a byte-size or runtime claim.
	const reserve = 65536
	if s.sourceWork < 1 || s.sourceStorage < reserve {
		report(regalloccheck.ResourceLimit)
		return nil
	}
	s.sourceStorage -= reserve
	attempt := BeginSourceAttempt(s, m, function)
	if attempt == nil {
		return nil
	}
	keep := false
	defer func() {
		if !keep {
			EndSourceAttempt(attempt)
		}
	}()
	l, r, err := wasm.BuildSourceLedger(m, a, function, features, wasm.SourceLedgerLimits{
		BodyBytes: 2048, Metadata: 4096, Locals: 128, Events: 1024, Values: 4096, Operands: 8192,
		Stack: 128, Blocks: 2, Edges: 2, ControlDepth: 1, Work: min(32768, s.sourceWork),
	})
	s.sourceWork -= r.Work
	if err != nil || r.Coverage != wasm.SourceContractsComplete {
		l.Close()
		report(r.Reason)
		return nil
	}
	if !AttachSourceLedger(attempt, l) {
		l.Close()
		report(regalloccheck.InvalidGraph)
		return nil
	}
	if l.EventCount()+l.ValueCount() > s.sourceWork {
		report(regalloccheck.ResourceLimit)
		return nil
	}
	s.sourceWork -= l.EventCount() + l.ValueCount()
	for i := 1; i <= l.ValueCount(); i++ {
		v := l.Value(wasm.SourceValueID(i))
		if v.Type != wasm.I32 && v.Type != wasm.I64 {
			report(regalloccheck.UnsupportedOperation)
			return nil
		}
	}
	for i := 0; i < l.EventCount(); i++ {
		e := l.Event(i)
		if e.InputCount > sourcePlanMaxOperands || e.OutputCount > sourcePlanMaxOperands {
			report(regalloccheck.ResourceLimit)
			return nil
		}
		admitted := false
		for _, rule := range [...]SourceRecipeRule{SourceRuleAlias, SourceRuleLiteral, SourceRuleDrop, SourceRuleNop, SourceRuleIntegerBinary, SourceRuleExit} {
			if sourceRuleMatches(rule, e, l, i) {
				admitted = true
				break
			}
		}
		if !admitted {
			report(regalloccheck.UnsupportedOperation)
			return nil
		}
	}
	token, result := BeginSourcePlan(attempt, SourcePlanLimits{Events: 1024, Values: 4096, Recipes: 1024, Slots: 4096, Transactions: 1})
	if token.p == nil {
		report(result.Reason)
		return nil
	}
	p := &IntegerSourcePlan{attempt: attempt, Token: token, Locals: l.LocalCount(), Events: l.EventCount(), result: result}
	keep = true
	return p
}
func (p *IntegerSourcePlan) Seal() SourcePlanResult {
	if p == nil || p.attempt == nil {
		return SourcePlanResult{Reason: regalloccheck.InvalidGraph}
	}
	_, p.result = SealSourcePlan(p.Token)
	return p.result
}
func (p *IntegerSourcePlan) Close() {
	if p == nil || p.attempt == nil {
		return
	}
	attempt := p.attempt
	owner, function := attempt.owner, attempt.function
	r := p.result
	if r.Readiness != SourceMappingReady {
		r = SourcePlanStatus(p.Token)
	}
	message := "integer source accounting incomplete; physical verification pending"
	if r.Readiness == SourceMappingReady {
		message = "integer source accounting complete; physical verification pending"
	}
	reason := r.Reason
	var physicalFailure regalloccheck.Result
	hasPhysicalFailure := false
	if attempt.plan != nil && attempt.plan.materialization != nil {
		mr := SourceMaterializationStatus(attempt.plan.materialization)
		state := "incomplete"
		if mr.Readiness == SourceMaterializationRecordingClosed {
			state = "recording closed"
		}
		message += "; materialization receipts " + state + " (" + strconv.Itoa(mr.Receipts) + ")"
		if mr.Reason == regalloccheck.ResourceLimit {
			reason = mr.Reason
		}
		if physical := attempt.plan.materialization.physical; physical != nil {
			pr := physical.Result()
			message += "; " + pr.Message
			if pr.Reason == regalloccheck.ResourceLimit {
				reason = pr.Reason
			}
			if pr.Verdict == regalloccheck.Rejected {
				physicalFailure, hasPhysicalFailure = pr, true
			}
		}
	}
	if reason == regalloccheck.NoFailure {
		reason = regalloccheck.UnsupportedOperation
	}
	EndSourceAttempt(attempt)
	*p = IntegerSourcePlan{}
	if hasPhysicalFailure {
		codegen.RecordSourceResult(owner.sourceContext, function, physicalFailure)
	}
	codegen.RecordSourceAccountingResult(owner.sourceContext, function, sourceLeafUnavailable(reason, message))
}
