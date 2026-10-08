//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func sourceMaterializationFixture(t *testing.T) (SourcePlanToken, *SourceAttempt, *SourceMaterializationJournal, SourceNodeRef, [2]SourceNodeRef) {
	t.Helper()
	p, a := sourcePlanFixture(t, []byte{0x20, 0, 0x20, 1, 0x6a, 0x0b}, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
	j := BeginSourceMaterializationJournal(p, 4)
	if j == nil {
		t.Fatal("journal admission")
	}
	g0 := sourcePlanStart(t, p, SourceRuleAlias, 0)
	left := sourcePlanOutput(t, g0)
	if !CommitSourceRecipe(g0, 0, 0) {
		t.Fatal("get0")
	}
	g1 := sourcePlanStart(t, p, SourceRuleAlias, 1)
	right := sourcePlanOutput(t, g1)
	if !CommitSourceRecipe(g1, 0, 0) {
		t.Fatal("get1")
	}
	binary := sourcePlanStart(t, p, SourceRuleIntegerBinary, 2, left, right)
	sum := sourcePlanOutput(t, binary)
	if !CommitSourceRecipe(binary, 0, 0) {
		t.Fatal("binary")
	}
	return p, a, j, sum, [2]SourceNodeRef{left, right}
}
func sourceMaterializationFinish(t *testing.T, p SourcePlanToken, j *SourceMaterializationJournal, sum SourceNodeRef, length int) {
	t.Helper()
	end := sourcePlanStart(t, p, SourceRuleExit, 3, sum)
	if !CommitSourceRecipe(end, 0, length) {
		t.Fatal("end recipe")
	}
	_, r := SealSourcePlan(p)
	if r.Readiness != SourceMappingReady {
		t.Fatal(r)
	}
	if r := EndSourceMaterializationEmission(j, length); r.Readiness != SourceMaterializationRecordingClosed || r.Reason != regalloccheck.NoFailure {
		t.Fatal(r)
	}
}
func TestSourceMaterializationOriginalProducerAndReadOnlySeal(t *testing.T) {
	for _, commuted := range []bool{false, true} {
		p, a, j, sum, inputs := sourceMaterializationFixture(t)
		if commuted {
			inputs[0], inputs[1] = inputs[1], inputs[0]
		}
		tok, ok := sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
		if !ok {
			t.Fatal(SourceMaterializationStatus(j))
		}
		if !CommitSourceMaterialization(j, tok, 9) {
			t.Fatal("commit")
		}
		sourceMaterializationFinish(t, p, j, sum, 10)
		if ChargeSourcePlanAdapter(p, 1, 0) {
			t.Fatal("sealed mutation token reopened")
		}
		before := a.owner.sourceWork
		r, ok := SourceMaterializationReceiptAt(j, 0)
		if !ok || r.SourceEvent != 2 || r.Kind != wasm.InstrI32Add || r.Width != 4 || r.Output != sum.id || r.Inputs[0] != inputs[0].id || r.Inputs[1] != inputs[1].id || r.Start != 7 || r.End != 9 || a.owner.sourceWork != before-1 {
			t.Fatal(r, ok)
		}
		r.Output = 99
		again, _ := SourceMaterializationReceiptAt(j, 0)
		if again.Output != sum.id {
			t.Fatal("mutable receipt escape")
		}
		owner := a.owner
		beforeStorage := owner.sourceStorage
		EndSourceAttempt(a)
		if _, ok := SourceMaterializationReceiptAt(j, 0); ok || SourceMaterializationStatus(j).Reason != regalloccheck.InvalidGraph || j.plan != nil || j.entries != nil {
			t.Fatal("retired receipt authority/retained pools")
		}
		if owner.sourceStorage != beforeStorage {
			t.Fatal("cleanup refunded history")
		}
	}
}
func TestSourceMaterializationSelectionAndLifetimeControls(t *testing.T) {
	for _, mode := range []string{"foreign output", "foreign input", "wrong input", "wrong operator", "stale birth", "duplicate producer", "overlap", "missing commit", "empty span", "foreign commit", "bad end", "quota", "serial limit", "nested constructor", "source transaction", "closed"} {
		t.Run(mode, func(t *testing.T) {
			p, a, j, sum, inputs := sourceMaterializationFixture(t)
			want := regalloccheck.InvalidGraph
			switch mode {
			case "foreign output", "foreign input":
				_, _, _, other, otherInputs := sourceMaterializationFixture(t)
				if mode == "foreign output" {
					sum = other
				} else {
					inputs[0] = otherInputs[0]
				}
				_, _ = sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
			case "wrong input":
				inputs[1] = inputs[0]
				_, _ = sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
				want = regalloccheck.UnsupportedOperation
			case "wrong operator":
				_, _ = sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Xor, 7)
				want = regalloccheck.UnsupportedOperation
			case "stale birth":
				sum.birth++
				_, _ = sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
			case "duplicate producer":
				tok, _ := sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
				CommitSourceMaterialization(j, tok, 9)
				_, _ = sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 10)
				want = regalloccheck.UnsupportedOperation
			case "overlap":
				tok, _ := sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
				CommitSourceMaterialization(j, tok, 9)
				// Repeating this dynamic producer is already unsupported; coordinates
				// never legitimize a second Define of that original source value.
				_, _ = sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 8)
				want = regalloccheck.UnsupportedOperation
			case "missing commit":
				sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
				end := sourcePlanStart(t, p, SourceRuleExit, 3, sum)
				CommitSourceRecipe(end, 0, 10)
				SealSourcePlan(p)
				EndSourceMaterializationEmission(j, 10)
				want = regalloccheck.UnsupportedOperation
			case "empty span":
				tok, _ := sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
				CommitSourceMaterialization(j, tok, 7)
				want = regalloccheck.UnsupportedOperation
			case "foreign commit":
				sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
				_, _, otherJ, other, otherInputs := sourceMaterializationFixture(t)
				tok, _ := sourceMaterializationBeginModel(otherJ, other, otherInputs[:], wasm.InstrI32Add, 7)
				CommitSourceMaterialization(j, tok, 9)
				if SourceMaterializationStatus(otherJ).Reason != regalloccheck.NoFailure {
					t.Fatal("foreign journal changed")
				}
			case "bad end":
				tok, _ := sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
				CommitSourceMaterialization(j, tok, 9)
				end := sourcePlanStart(t, p, SourceRuleExit, 3, sum)
				CommitSourceRecipe(end, 0, 10)
				SealSourcePlan(p)
				EndSourceMaterializationEmission(j, 8)
			case "quota":
				a.owner.sourceWork = 0
				sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
				want = regalloccheck.ResourceLimit
			case "serial limit":
				j.serial = ^uint64(0)
				sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 7)
				want = regalloccheck.ResourceLimit
			case "nested constructor":
				if BeginSourceMaterializationJournal(p, 4) != nil || p.p.materialization != j || SourceMaterializationStatus(j).Reason != regalloccheck.NoFailure {
					t.Fatal("nested constructor damaged owner")
				}
				return
			case "source transaction":
				if _, ok := CheckpointSourcePlan(p, nil, 0); !ok {
					t.Fatal("source checkpoint changed")
				}
				want = regalloccheck.UnsupportedOperation
			case "closed":
				EndSourceAttempt(a)
			}
			r := SourceMaterializationStatus(j)
			if r.Reason != want || r.Readiness != SourceMaterializationIncomplete {
				t.Fatal(mode, r, want)
			}
			FailSourceMaterialization(j, regalloccheck.UnsupportedOperation)
			if SourceMaterializationStatus(j).Reason != want {
				t.Fatal("failure repaired")
			}
		})
	}
}
func TestSourceMaterializationNilAndLateFailure(t *testing.T) {
	if BeginSourceMaterializationJournal(SourcePlanToken{}, 1) != nil {
		t.Fatal("missing plan admitted")
	}
	if _, ok := sourceMaterializationBeginModel(nil, SourceNodeRef{}, nil, wasm.InstrI32Add, 0); ok {
		t.Fatal("nil journal")
	}
	if CommitSourceMaterialization(nil, SourceMaterializationToken{}, 1) {
		t.Fatal("nil commit")
	}
	if EndSourceMaterializationEmission(nil, 1).Reason != regalloccheck.InvalidGraph {
		t.Fatal("nil end")
	}
	p, _, j, sum, inputs := sourceMaterializationFixture(t)
	tok, _ := sourceMaterializationBeginModel(j, sum, inputs[:], wasm.InstrI32Add, 0)
	CommitSourceMaterialization(j, tok, 2)
	sourceMaterializationFinish(t, p, j, sum, 3)
	FailSourceMaterialization(j, regalloccheck.ResourceLimit)
	if SourceMaterializationStatus(j).Reason != regalloccheck.ResourceLimit {
		t.Fatal("late quota was hidden by closed recording")
	}
	if _, ok := SourceMaterializationReceiptAt(j, 0); ok {
		t.Fatal("failed certificate still readable")
	}
}

func sourceMaterializationBeginModel(j *SourceMaterializationJournal, n SourceNodeRef, inputs []SourceNodeRef, kind wasm.InstrKind, start int) (SourceMaterializationToken, bool) {
	producer, ok := ExpectSourceMaterializationProducer(j, n, inputs, kind)
	if !ok {
		return SourceMaterializationToken{}, false
	}
	return BeginSourceMaterialization(j, producer, n, inputs, kind, start)
}

func TestSourceMaterializationMultipleProducersCommitWork(t *testing.T) {
	p, a := sourcePlanFixture(t, []byte{0x20, 0, 0x20, 1, 0x6a, 0x20, 0, 0x20, 1, 0x73, 0x73, 0x0b}, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
	j := BeginSourceMaterializationJournal(p, 4)
	n0, _ := SourceEntryNode(p, 0)
	n1, _ := SourceEntryNode(p, 1)
	var outputs [2]SourceNodeRef
	for group := 0; group < 2; group++ {
		base := group * 3
		for k := 0; k < 2; k++ {
			r := sourcePlanStart(t, p, SourceRuleAlias, base+k)
			sourcePlanOutput(t, r)
			CommitSourceRecipe(r, 0, 0)
		}
		r := sourcePlanStart(t, p, SourceRuleIntegerBinary, base+2, n0, n1)
		outputs[group] = sourcePlanOutput(t, r)
		CommitSourceRecipe(r, 0, 0)
	}
	for index, n := range outputs {
		kind := wasm.InstrI32Add
		if index == 1 {
			kind = wasm.InstrI32Xor
		}
		producer, ok := ExpectSourceMaterializationProducer(j, n, []SourceNodeRef{n0, n1}, kind)
		if !ok {
			t.Fatal(SourceMaterializationStatus(j))
		}
		token, ok := BeginSourceMaterialization(j, producer, n, []SourceNodeRef{n0, n1}, kind, index*2)
		if !ok {
			t.Fatal(SourceMaterializationStatus(j))
		}
		// Commit has a validated direct producer index and no producer search. One
		// credit is sufficient even for the second receipt; nothing is refunded.
		saved := a.owner.sourceWork
		a.owner.sourceWork = 1
		if !CommitSourceMaterialization(j, token, index*2+2) || a.owner.sourceWork != 0 || !j.producers[producer.index].emitted {
			t.Fatal(index, SourceMaterializationStatus(j))
		}
		a.owner.sourceWork = saved - 1
	}
	if j.entries[0].producer == j.entries[1].producer {
		t.Fatal("different definitions share producer index")
	}
	// Actual distinct-producer overlap is rejected, independently of duplicate
	// dynamic-definition policy. Use a fresh tracker on an independent attempt.
	q, b := sourcePlanFixture(t, []byte{0x20, 0, 0x20, 1, 0x6a, 0x20, 0, 0x20, 1, 0x73, 0x73, 0x0b}, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, nil, SourcePlanLimits{})
	_ = b
	other := BeginSourceMaterializationJournal(q, 4)
	q0, _ := SourceEntryNode(q, 0)
	q1, _ := SourceEntryNode(q, 1)
	for group := 0; group < 2; group++ {
		for k := 0; k < 2; k++ {
			r := sourcePlanStart(t, q, SourceRuleAlias, group*3+k)
			sourcePlanOutput(t, r)
			CommitSourceRecipe(r, 0, 0)
		}
		r := sourcePlanStart(t, q, SourceRuleIntegerBinary, group*3+2, q0, q1)
		out := sourcePlanOutput(t, r)
		CommitSourceRecipe(r, 0, 0)
		kind := wasm.InstrI32Add
		if group == 1 {
			kind = wasm.InstrI32Xor
		}
		producer, _ := ExpectSourceMaterializationProducer(other, out, []SourceNodeRef{q0, q1}, kind)
		start := 7
		if group == 1 {
			start = 8
		}
		token, ok := BeginSourceMaterialization(other, producer, out, []SourceNodeRef{q0, q1}, kind, start)
		if group == 0 {
			if !ok || !CommitSourceMaterialization(other, token, 9) {
				t.Fatal("first receipt")
			}
		} else if ok || SourceMaterializationStatus(other).Reason != regalloccheck.InvalidGraph {
			t.Fatal("distinct producer overlap", SourceMaterializationStatus(other))
		}
	}
}
