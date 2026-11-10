//go:build wago_regalloccheck

package shared

import (
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func sourcePlanFixture(t *testing.T, body []byte, params, results []wasm.ValType, locals []wasm.LocalRun, limits SourcePlanLimits) (SourcePlanToken, *SourceAttempt) {
	t.Helper()
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: results}}}}}, FuncTypes: []wasm.TypeIdx{{}}, Code: []wasm.Func{{BodyBytes: body, Locals: wasm.Locals{Runs: locals}}}}
	a := new(wasm.ValidatedModuleAnalysis)
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	options := codegen.SourceOptions(codegen.Options{}, m, a, wasm.ValidationFeatures{})
	t.Cleanup(func() { codegen.CloseSourceContext(options, m) })
	s := new(ScalarState)
	SetSourceContext(s, codegen.SourceContextFor(options, m))
	t.Cleanup(s.FinishWorker)
	attempt := BeginSourceAttempt(s, m, 0)
	l := sourceWorkerLedger(t, m, a)
	if !AttachSourceLedger(attempt, l) {
		t.Fatal("ledger ownership")
	}
	plan, result := BeginSourcePlan(attempt, limits)
	if plan.p == nil || result.Reason != regalloccheck.NoFailure {
		t.Fatal("plan admission", result)
	}
	return plan, attempt
}
func sourcePlanGetEvent(t *testing.T, p SourcePlanToken, index int) SourceEventRef {
	t.Helper()
	e, ok := SourcePlanEvent(p, index)
	if !ok {
		t.Fatal("event unavailable", index)
	}
	return e
}
func sourcePlanStart(t *testing.T, p SourcePlanToken, rule SourceRecipeRule, index int, nodes ...SourceNodeRef) SourceRecipeToken {
	t.Helper()
	r, ok := BeginSourceRecipe(p, rule, sourcePlanGetEvent(t, p, index), nodes)
	if !ok {
		t.Fatal("recipe rejected", index, p.p.result())
	}
	return r
}
func sourcePlanOutput(t *testing.T, r SourceRecipeToken) SourceNodeRef {
	t.Helper()
	n, ok := SourceRecipeOutput(r, 0)
	if !ok {
		t.Fatal("output unavailable")
	}
	return n
}

func TestSourcePlanOriginalAliasesAndDeferredRecipes(t *testing.T) {
	p, a := sourcePlanFixture(t, []byte{0x20, 0, 0x20, 1, 0x6a, 0x22, 2, 0x20, 0, 0x73, 0x0b}, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []wasm.LocalRun{{Count: 1, Type: wasm.I32}}, SourcePlanLimits{})
	left, _ := SourceEntryNode(p, 0)
	right, _ := SourceEntryNode(p, 1)
	r0 := sourcePlanStart(t, p, SourceRuleAlias, 0)
	r1 := sourcePlanStart(t, p, SourceRuleAlias, 1)
	if sourcePlanOutput(t, r0) != left || sourcePlanOutput(t, r1) != right {
		t.Fatal("original entry alias changed")
	}
	add := sourcePlanStart(t, p, SourceRuleIntegerBinary, 2, left, right)
	sum := sourcePlanOutput(t, add)
	tee := sourcePlanStart(t, p, SourceRuleAlias, 3, sum)
	if sourcePlanOutput(t, tee) != sum {
		t.Fatal("tee invented source definition")
	}
	r4 := sourcePlanStart(t, p, SourceRuleAlias, 4)
	xor := sourcePlanStart(t, p, SourceRuleIntegerBinary, 5, sum, sourcePlanOutput(t, r4))
	out := sourcePlanOutput(t, xor)
	exit := sourcePlanStart(t, p, SourceRuleExit, 6, out)
	// Desired nodes may precede physical materialization; each pending producer
	// still must be closed and spans are only claims for the next proof layer.
	for _, r := range []SourceRecipeToken{r0, r1, tee, r4, exit} {
		if !CommitSourceRecipe(r, 0, 0) {
			t.Fatal("alias closure")
		}
	}
	if !CommitSourceRecipe(add, 8, 11) || !CommitSourceRecipe(xor, 11, 14) {
		t.Fatal("deferred materialization")
	}
	records, result := SealSourcePlan(p)
	if result.Readiness != SourceMappingReady || records.Len() != 7 || records.Record(2).Inputs[0] != a.ledger.EntryLocal(0) || records.Record(2).Inputs[1] != a.ledger.EntryLocal(1) || records.Record(5).Inputs[0] != records.Record(2).Outputs[0] {
		t.Fatal("original recipe accounting lost", result)
	}
	copy := records.Record(2)
	copy.Inputs[0] = 0
	if records.Record(2).Inputs[0] == 0 {
		t.Fatal("record accessor shares mutable storage")
	}
	EndSourceAttempt(a)
	if records.Record(2).Start != 8 || p.p.attempt != nil || p.p.nodes != nil || p.p.recipes != nil {
		t.Fatal("detached records/attempt cleanup")
	}
}

func TestSourcePlanRejectsForeignDuplicateAndWrongOperands(t *testing.T) {
	for _, mode := range []string{"foreign-event", "foreign-node", "reversed-inputs", "duplicate-event", "unknown-rule", "unsupported-operation"} {
		t.Run(mode, func(t *testing.T) {
			body := []byte{0x20, 0, 0x20, 1, 0x6a, 0x0b}
			if mode == "unsupported-operation" {
				body[4] = 0x6c
			}
			p, _ := sourcePlanFixture(t, body, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
			q, _ := sourcePlanFixture(t, []byte{0x20, 0, 0x0b}, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
			left, _ := SourceEntryNode(p, 0)
			right, _ := SourceEntryNode(p, 1)
			foreign, _ := SourceEntryNode(q, 0)
			var ok bool
			switch mode {
			case "foreign-event":
				_, ok = BeginSourceRecipe(p, SourceRuleAlias, sourcePlanGetEvent(t, q, 0), nil)
			case "unknown-rule":
				_, ok = BeginSourceRecipe(p, SourceRecipeRule(255), sourcePlanGetEvent(t, p, 0), nil)
			default:
				r0 := sourcePlanStart(t, p, SourceRuleAlias, 0)
				CommitSourceRecipe(r0, 0, 0)
				if mode == "duplicate-event" {
					_, ok = BeginSourceRecipe(p, SourceRuleAlias, sourcePlanGetEvent(t, p, 0), nil)
					break
				}
				r1 := sourcePlanStart(t, p, SourceRuleAlias, 1)
				CommitSourceRecipe(r1, 0, 0)
				inputs := []SourceNodeRef{left, right}
				if mode == "reversed-inputs" {
					inputs = []SourceNodeRef{right, left}
				}
				if mode == "foreign-node" {
					inputs[0] = foreign
				}
				_, ok = BeginSourceRecipe(p, SourceRuleIntegerBinary, sourcePlanGetEvent(t, p, 2), inputs)
			}
			if ok {
				t.Fatal("bad correspondence admitted")
			}
			if records, r := SealSourcePlan(p); records.Len() != 0 || r.Readiness == SourceMappingReady || r.Reason == regalloccheck.NoFailure {
				t.Fatal("failure erased", r)
			}
		})
	}
}

func TestSourcePlanRollbackPendingCommitAndSlotGenerations(t *testing.T) {
	p, _ := sourcePlanFixture(t, []byte{0x41, 17, 0x0b}, nil, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
	literal := sourcePlanStart(t, p, SourceRuleLiteral, 0)
	value := sourcePlanOutput(t, literal)
	slot, ok := BindSourceSlot(p, 0, value)
	if !ok {
		t.Fatal("bind")
	}
	outer, ok := CheckpointSourcePlan(p, nil, 8)
	if !ok {
		t.Fatal("checkpoint")
	}
	inner, ok := CheckpointSourcePlan(p, nil, 8)
	if !ok || !CommitSourceRecipe(literal, 8, 11) || !CommitSourcePlanMark(p, inner) {
		t.Fatal("nested pending commit")
	}
	beforeWork, beforeStorage := p.p.attempt.owner.sourceWork, p.p.attempt.owner.sourceStorage
	if !RollbackSourcePlan(p, outer, 8) || p.p.recipes[literal.index].committed {
		t.Fatal("discarded pre-checkpoint producer stayed committed")
	}
	if _, ok := SourceSlotNode(slot); ok {
		t.Fatal("rollback revived pool generation")
	}
	newSlot, ok := BindSourceSlot(p, 0, value)
	if !ok || newSlot.generation == slot.generation {
		t.Fatal("rebind reused generation")
	}
	if _, ok := SourceSlotNode(slot); ok || ReleaseSourceSlot(slot) {
		t.Fatal("stale slot handle accepted")
	}
	if _, ok := SourceSlotNode(newSlot); !ok {
		t.Fatal("stale release removed surviving slot")
	}
	if !CommitSourceRecipe(literal, 8, 12) {
		t.Fatal("rolled-back pending producer could not recommit")
	}
	exit := sourcePlanStart(t, p, SourceRuleExit, 1, value)
	CommitSourceRecipe(exit, 12, 13)
	if _, r := SealSourcePlan(p); r.Readiness != SourceMappingReady {
		t.Fatal("recommitted plan incomplete", r)
	}
	if p.p.attempt.owner.sourceWork >= beforeWork || p.p.attempt.owner.sourceStorage > beforeStorage {
		t.Fatal("rollback refunded history")
	}
}

func TestSourcePlanRollbackNewNodesDoesNotReviveHandles(t *testing.T) {
	p, _ := sourcePlanFixture(t, []byte{0x41, 17, 0x0b}, nil, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
	m, _ := CheckpointSourcePlan(p, nil, 0)
	oldRecipe := sourcePlanStart(t, p, SourceRuleLiteral, 0)
	oldNode := sourcePlanOutput(t, oldRecipe)
	CommitSourceRecipe(oldRecipe, 0, 5)
	if !RollbackSourcePlan(p, m, 0) {
		t.Fatal("rollback")
	}
	r := sourcePlanStart(t, p, SourceRuleLiteral, 0)
	n := sourcePlanOutput(t, r)
	if n.birth == oldNode.birth || p.p.nodeValid(oldNode) || CommitSourceRecipe(oldRecipe, 0, 5) {
		t.Fatal("rolled-back producer generation accepted")
	}
	if !p.p.nodeValid(n) {
		t.Fatal("new original node unavailable")
	}
}

func TestSourcePlanStickyLimitsGapAndOwnership(t *testing.T) {
	for _, mode := range []string{"unconsumed", "pending", "gap", "zero-work", "zero-storage", "recipe-limit", "serial-overflow", "foreign-mark", "cursor", "negative-cursor"} {
		t.Run(mode, func(t *testing.T) {
			p, a := sourcePlanFixture(t, []byte{0x41, 17, 0x0b}, nil, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
			if duplicate, r := BeginSourcePlan(a, SourcePlanLimits{}); duplicate.p != nil || a.plan != p.p || r.Reason != regalloccheck.InvalidGraph {
				t.Fatal("nested begin replaced parent")
			}
			switch mode {
			case "pending":
				sourcePlanStart(t, p, SourceRuleLiteral, 0)
			case "gap":
				RecordSourcePlanGap(p)
			case "zero-work":
				a.owner.sourceWork = 0
				SourcePlanEvent(p, 0)
			case "zero-storage":
				a.owner.sourceStorage = 0
				BeginSourceRecipe(p, SourceRuleLiteral, sourcePlanGetEvent(t, p, 0), nil)
			case "recipe-limit":
				p.p.limits.Recipes = 1
				r := sourcePlanStart(t, p, SourceRuleLiteral, 0)
				n := sourcePlanOutput(t, r)
				CommitSourceRecipe(r, 0, 5)
				BeginSourceRecipe(p, SourceRuleExit, sourcePlanGetEvent(t, p, 1), []SourceNodeRef{n})
			case "serial-overflow":
				p.p.serial = ^uint64(0)
				BeginSourceRecipe(p, SourceRuleLiteral, sourcePlanGetEvent(t, p, 0), nil)
			case "foreign-mark":
				m, _ := CheckpointSourcePlan(p, nil, 0)
				m.serial++
				CommitSourcePlanMark(p, m)
			case "cursor":
				m, _ := CheckpointSourcePlan(p, nil, 3)
				RollbackSourcePlan(p, m, 2)
			case "negative-cursor":
				CheckpointSourcePlan(p, nil, -1)
			}
			if records, r := SealSourcePlan(p); r.Readiness == SourceMappingReady || records.Len() != 0 {
				t.Fatal("incomplete plan sealed", r)
			}
			CloseSourcePlan(p)
			CloseSourcePlan(p)
			if a.plan != nil || p.p.attempt != nil || p.p.slots != nil || p.p.marks != nil {
				t.Fatal("closed plan retained state")
			}
		})
	}
}

func TestSourcePlanPairedJournalAndUnwind(t *testing.T) {
	p, a := sourcePlanFixture(t, []byte{0x0b}, nil, nil, nil, SourcePlanLimits{})
	j := regalloccheck.NewEmissionJournal(regalloccheck.JournalLimits{})
	outer := j.Checkpoint(0)
	mark, ok := CheckpointSourcePlan(p, j, 0)
	if !ok {
		t.Fatal("paired mark")
	}
	j.BeginEmission(0)
	j.ObserveGPWrites(1)
	j.EndEmission(1)
	if !RollbackSourcePlan(p, mark, 0) || !j.Commit(outer) {
		t.Fatal("paired rollback damaged outer owner")
	}
	r := sourcePlanStart(t, p, SourceRuleExit, 0)
	CommitSourceRecipe(r, 0, 0)
	if _, result := SealSourcePlan(p); result.Readiness != SourceMappingReady || j.Finalize(0, 0, nil).State != regalloccheck.JournalReady {
		t.Fatal("paired readiness")
	}
	j.Close()
	EndSourceAttempt(a)
	p, a = sourcePlanFixture(t, []byte{0x0b}, nil, nil, nil, SourcePlanLimits{})
	j = regalloccheck.NewEmissionJournal(regalloccheck.JournalLimits{})
	outer = j.Checkpoint(0)
	CheckpointSourcePlan(p, j, 0)
	j.BeginEmission(0)
	j.ObserveGPWrites(1)
	EndSourceAttempt(a)
	if j.Result().State != regalloccheck.JournalInvalid || !j.Abandon(outer) {
		t.Fatal("unwind did not release owned child mark/preserve outer")
	}
	j.Close()
}

func TestSourcePlanClosedContextAndAdmissionHistory(t *testing.T) {
	p, a := sourcePlanFixture(t, []byte{0x0b}, nil, nil, nil, SourcePlanLimits{})
	CloseSourcePlan(p)
	a.owner.sourceWork = 0
	if next, r := BeginSourcePlan(a, SourcePlanLimits{}); next.p != nil || r.Reason != regalloccheck.ResourceLimit || a.plan != nil {
		t.Fatal("zero history selected a constructor default", r)
	}
	a.owner.sourceWork = 2
	a.owner.sourceStorage = 0
	for i := 0; i < 2; i++ {
		if next, r := BeginSourcePlan(a, SourcePlanLimits{}); next.p != nil || r.Reason != regalloccheck.ResourceLimit || r.Work != 1 {
			t.Fatal("failed admission refunded work", r)
		}
	}
	if a.owner.sourceWork != 0 {
		t.Fatal("constructor failures did not exhaust aggregate history")
	}
	a.ledger.Close()
	if next, r := BeginSourcePlan(a, SourcePlanLimits{}); next.p != nil || r.Reason != regalloccheck.InvalidGraph {
		t.Fatal("closed ledger produced source mapping", r)
	}
}

func TestSourcePlanArityLimitAndUnsupportedCFG(t *testing.T) {
	params := make([]wasm.ValType, 9)
	var body []byte
	for i := range params {
		params[i] = wasm.I32
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x0b)
	p, _ := sourcePlanFixture(t, body, params, params, nil, SourcePlanLimits{})
	var nodes []SourceNodeRef
	for i := range params {
		r := sourcePlanStart(t, p, SourceRuleAlias, i)
		nodes = append(nodes, sourcePlanOutput(t, r))
		CommitSourceRecipe(r, 0, 0)
	}
	if _, ok := BeginSourceRecipe(p, SourceRuleExit, sourcePlanGetEvent(t, p, 9), nodes); ok || p.p.reason != regalloccheck.ResourceLimit {
		t.Fatal("bounded exit arity was admitted or misclassified")
	}
	p, _ = sourcePlanFixture(t, []byte{0x02, 0x40, 0x0b, 0x0b}, nil, nil, nil, SourcePlanLimits{})
	if _, ok := BeginSourceRecipe(p, SourceRuleNop, sourcePlanGetEvent(t, p, 0), nil); ok || p.p.reason != regalloccheck.UnsupportedOperation {
		t.Fatal("structural CFG silently became a numeric recipe")
	}
}

func TestSourcePlanCompleteAccountingStillHonorsStickyFailure(t *testing.T) {
	for _, mode := range []string{"gap", "work", "storage"} {
		t.Run(mode, func(t *testing.T) {
			p, a := sourcePlanFixture(t, []byte{0x0b}, nil, nil, nil, SourcePlanLimits{})
			r := sourcePlanStart(t, p, SourceRuleExit, 0)
			if !CommitSourceRecipe(r, 0, 0) {
				t.Fatal("complete accounting fixture")
			}
			want := regalloccheck.ResourceLimit
			switch mode {
			case "gap":
				RecordSourcePlanGap(p)
				want = regalloccheck.UnsupportedOperation
			case "work":
				a.owner.sourceWork = 0
			case "storage":
				a.owner.sourceStorage = 0
			}
			if records, result := SealSourcePlan(p); records.Len() != 0 || result.Readiness == SourceMappingReady || result.Reason != want {
				t.Fatal("complete accounting erased a sticky gap/resource failure", result)
			}
		})
	}
}
