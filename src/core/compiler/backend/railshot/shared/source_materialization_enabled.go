//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// These are actual-emission claims, not machine facts. The final decoder still
// must cover the whole image and validate every operand before any Define.
type SourceMaterializationReadiness uint8

const (
	SourceMaterializationOpen SourceMaterializationReadiness = iota
	SourceMaterializationIncomplete
	SourceMaterializationRecordingClosed
)

type SourceMaterializationResult struct {
	Readiness                       SourceMaterializationReadiness
	Reason                          regalloccheck.FailureReason
	Receipts, Work, Storage, Length int
}

// Receipts are pointer-free and expose original IDs only after the owning plan
// checked producer birth, source operator/type and the live operand identities.
// Coordinates and operand role claims are untrusted until final-byte decoding.
type SourceMaterializationReceipt struct {
	SourceEvent int
	Kind        wasm.InstrKind
	Width       uint8
	Inputs      [2]wasm.SourceValueID
	Output      wasm.SourceValueID
	Start, End  int
}
type SourceMaterializationProducer struct {
	owner  *SourceMaterializationJournal
	serial uint64
	index  int
}
type sourceMaterializationProducer struct {
	output  SourceNodeRef
	inputs  [2]SourceNodeRef
	kind    wasm.InstrKind
	event   int
	width   uint8
	serial  uint64
	emitted bool
}
type SourceMaterializationToken struct {
	owner  *SourceMaterializationJournal
	serial uint64
	index  int
}
type sourceMaterializationEntry struct {
	producer  int
	receipt   SourceMaterializationReceipt
	serial    uint64
	committed bool
}
type SourceMaterializationJournal struct {
	physical      *SourceIntegerPhysical
	plan          *sourceRecipePlan
	entries       []sourceMaterializationEntry
	producers     []sourceMaterializationProducer
	limit         int
	serial        uint64
	pending       bool
	length        int
	work, storage int
	reason        regalloccheck.FailureReason
	ended, closed bool
}

func sourceMaterializationOwnerValid(j *SourceMaterializationJournal) bool {
	if j == nil {
		return false
	}
	p := j.plan
	return !j.closed && p != nil && !p.closed && sourcePlanAttemptValid(p.attempt) && p.attempt.plan == p && p.materialization == j
}
func (j *SourceMaterializationJournal) fail(r regalloccheck.FailureReason) bool {
	if j.reason == regalloccheck.NoFailure {
		j.reason = r
	}
	return false
}
func (j *SourceMaterializationJournal) charge(work, storage int) bool {
	if !sourceMaterializationOwnerValid(j) || j.reason != regalloccheck.NoFailure {
		return false
	}
	// A sealed plan is immutable, but its live owner still owns historical worker
	// credits. This does not reopen recipes, nodes, slots or rollback authority.
	s := j.plan.attempt.owner
	if work < 0 || storage < 0 || work > s.sourceWork || storage > s.sourceStorage {
		return j.fail(regalloccheck.ResourceLimit)
	}
	s.sourceWork -= work
	s.sourceStorage -= storage
	j.work += work
	j.storage += storage
	return true
}
func BeginSourceMaterializationJournal(t SourcePlanToken, limit int) *SourceMaterializationJournal {
	p := t.p
	if !sourcePlanAlive(p) || p.reason != regalloccheck.NoFailure || p.materialization != nil {
		return nil
	}
	if limit <= 0 || limit > 1024 || len(p.marks) != 0 {
		p.fail(regalloccheck.UnsupportedOperation)
		return nil
	}
	if !p.charge(1, 1) {
		return nil
	}
	j := &SourceMaterializationJournal{plan: p, limit: limit, work: 1, storage: 1}
	p.materialization = j
	return j
}
func FailSourceMaterialization(j *SourceMaterializationJournal, reason regalloccheck.FailureReason) {
	if !sourceMaterializationOwnerValid(j) {
		return
	}
	switch reason {
	case regalloccheck.InvalidGraph, regalloccheck.UnsupportedOperation, regalloccheck.ResourceLimit:
		j.fail(reason)
	default:
		j.fail(regalloccheck.InvalidGraph)
	}
}

// BeginSourceMaterialization binds a live original dynamic producer at actual
// ALU emission. Commuting these four nontrapping operators is allowed; arbitrary
// reassociation, rematerialization and source-plan transactions need new rules.
// ExpectSourceMaterializationProducer captures original identity before
// destructive tree lowering. It grants no span or physical definition.
func ExpectSourceMaterializationProducer(j *SourceMaterializationJournal, output SourceNodeRef, inputs []SourceNodeRef, kind wasm.InstrKind) (SourceMaterializationProducer, bool) {
	if !sourceMaterializationOwnerValid(j) {
		return SourceMaterializationProducer{}, false
	}
	if j.ended {
		return SourceMaterializationProducer{}, j.fail(regalloccheck.InvalidGraph)
	}
	p := j.plan
	if !sourcePlanAlive(p) || len(p.marks) != 0 || p.reason != regalloccheck.NoFailure {
		return SourceMaterializationProducer{}, j.fail(regalloccheck.UnsupportedOperation)
	}
	if !p.nodeValid(output) || len(inputs) != 2 {
		return SourceMaterializationProducer{}, j.fail(regalloccheck.InvalidGraph)
	}
	if !j.charge(len(p.recipes)+len(j.producers)+3, 0) {
		return SourceMaterializationProducer{}, false
	}
	for _, n := range inputs {
		if !p.nodeValid(n) {
			return SourceMaterializationProducer{}, j.fail(regalloccheck.InvalidGraph)
		}
	}
	index := -1
	for i, r := range p.recipes {
		if r.serial == output.birth && r.active && r.committed && r.record.Rule == SourceRuleIntegerBinary && r.record.OutputCount == 1 && r.record.Outputs[0] == output.id {
			index = i
			break
		}
	}
	if index < 0 {
		return SourceMaterializationProducer{}, j.fail(regalloccheck.UnsupportedOperation)
	}
	r := p.recipes[index].record
	e := p.attempt.ledger.Event(r.Event)
	if !sourceRuleMatches(SourceRuleIntegerBinary, e, p.attempt.ledger, r.Event) {
		return SourceMaterializationProducer{}, j.fail(regalloccheck.InvalidGraph)
	}
	if e.Kind != kind {
		return SourceMaterializationProducer{}, j.fail(regalloccheck.UnsupportedOperation)
	}
	if !((inputs[0].id == r.Inputs[0] && inputs[1].id == r.Inputs[1]) || (inputs[0].id == r.Inputs[1] && inputs[1].id == r.Inputs[0])) {
		return SourceMaterializationProducer{}, j.fail(regalloccheck.UnsupportedOperation)
	}
	typ := p.attempt.ledger.Value(output.id).Type
	for _, n := range inputs {
		if p.attempt.ledger.Value(n.id).Type != typ {
			return SourceMaterializationProducer{}, j.fail(regalloccheck.InvalidGraph)
		}
	}
	for i, old := range j.producers {
		if old.output == output {
			if old.emitted {
				return SourceMaterializationProducer{}, j.fail(regalloccheck.UnsupportedOperation)
			}
			return SourceMaterializationProducer{j, old.serial, i}, true
		}
	}
	if len(j.producers) >= j.limit || j.serial == ^uint64(0) || !j.charge(1, 1) {
		return SourceMaterializationProducer{}, j.fail(regalloccheck.ResourceLimit)
	}
	width := uint8(4)
	if typ == wasm.I64 {
		width = 8
	}
	j.serial++
	j.producers = append(j.producers, sourceMaterializationProducer{output: output, inputs: [2]SourceNodeRef{inputs[0], inputs[1]}, kind: e.Kind, event: r.Event, width: width, serial: j.serial})
	return SourceMaterializationProducer{j, j.serial, len(j.producers) - 1}, true
}

// The defining emission claim requires the earlier original-producer capture.
// Physical homes remain entirely the final decoder's responsibility.
func BeginSourceMaterialization(j *SourceMaterializationJournal, producer SourceMaterializationProducer, output SourceNodeRef, inputs []SourceNodeRef, kind wasm.InstrKind, start int) (SourceMaterializationToken, bool) {
	if !sourceMaterializationOwnerValid(j) {
		return SourceMaterializationToken{}, false
	}
	if j.ended || j.pending {
		return SourceMaterializationToken{}, j.fail(regalloccheck.InvalidGraph)
	}
	p := j.plan
	if !sourcePlanAlive(p) || len(p.marks) != 0 || p.reason != regalloccheck.NoFailure {
		return SourceMaterializationToken{}, j.fail(regalloccheck.UnsupportedOperation)
	}
	if producer.owner != j || producer.index < 0 || producer.index >= len(j.producers) || j.producers[producer.index].serial != producer.serial {
		return SourceMaterializationToken{}, j.fail(regalloccheck.InvalidGraph)
	}
	original := j.producers[producer.index]
	if !p.nodeValid(output) || output != original.output || len(inputs) != 2 || start < 0 || start > 8192 {
		return SourceMaterializationToken{}, j.fail(regalloccheck.InvalidGraph)
	}
	if !j.charge(len(j.entries)+3, 0) {
		return SourceMaterializationToken{}, false
	}
	for _, n := range inputs {
		if !p.nodeValid(n) {
			return SourceMaterializationToken{}, j.fail(regalloccheck.InvalidGraph)
		}
	}
	if kind != original.kind || !((inputs[0] == original.inputs[0] && inputs[1] == original.inputs[1]) || (inputs[0] == original.inputs[1] && inputs[1] == original.inputs[0])) {
		return SourceMaterializationToken{}, j.fail(regalloccheck.UnsupportedOperation)
	}
	if original.emitted {
		return SourceMaterializationToken{}, j.fail(regalloccheck.UnsupportedOperation)
	}
	for _, old := range j.entries {
		if old.receipt.Output == output.id {
			return SourceMaterializationToken{}, j.fail(regalloccheck.UnsupportedOperation)
		}
		if old.committed && old.receipt.End > start {
			return SourceMaterializationToken{}, j.fail(regalloccheck.InvalidGraph)
		}
	}
	if len(j.entries) >= j.limit || j.serial == ^uint64(0) || !j.charge(1, 1) {
		return SourceMaterializationToken{}, j.fail(regalloccheck.ResourceLimit)
	}
	j.serial++
	receipt := SourceMaterializationReceipt{SourceEvent: original.event, Kind: original.kind, Width: original.width, Inputs: [2]wasm.SourceValueID{inputs[0].id, inputs[1].id}, Output: output.id, Start: start}
	j.entries = append(j.entries, sourceMaterializationEntry{receipt: receipt, serial: j.serial, producer: producer.index})
	j.pending = true
	return SourceMaterializationToken{j, j.serial, len(j.entries) - 1}, true
}
func CommitSourceMaterialization(j *SourceMaterializationJournal, t SourceMaterializationToken, end int) bool {
	if !sourceMaterializationOwnerValid(j) {
		return false
	}
	if j.ended || !j.pending || t.owner != j || t.index != len(j.entries)-1 || t.index < 0 || j.entries[t.index].serial != t.serial {
		return j.fail(regalloccheck.InvalidGraph)
	}
	if !j.charge(1, 0) {
		return false
	}
	r := &j.entries[t.index]
	if r.committed || end <= r.receipt.Start || end-r.receipt.Start > 15 || end > 8192 {
		return j.fail(regalloccheck.UnsupportedOperation)
	}
	r.receipt.End = end
	r.committed = true
	if r.producer < 0 || r.producer >= len(j.producers) || j.producers[r.producer].output.id != r.receipt.Output {
		return j.fail(regalloccheck.InvalidGraph)
	}
	j.producers[r.producer].emitted = true
	j.pending = false
	return true
}

// Closing emission says only that claims were recorded. Source sealing is a
// prerequisite; it neither verifies bytes nor provides machine Definitions.
func EndSourceMaterializationEmission(j *SourceMaterializationJournal, length int) SourceMaterializationResult {
	if !sourceMaterializationOwnerValid(j) {
		return SourceMaterializationResult{Readiness: SourceMaterializationIncomplete, Reason: regalloccheck.InvalidGraph}
	}
	if j.ended {
		j.fail(regalloccheck.InvalidGraph)
		return SourceMaterializationStatus(j)
	}
	p := j.plan
	if !p.sealed || p.reason != regalloccheck.NoFailure || j.pending || length < 0 || length > 8192 {
		j.fail(regalloccheck.UnsupportedOperation)
		return SourceMaterializationStatus(j)
	}
	if !j.charge(len(j.entries)+len(j.producers)+1, 0) {
		return SourceMaterializationStatus(j)
	}
	for _, r := range j.entries {
		if !r.committed || r.receipt.End > length {
			j.fail(regalloccheck.InvalidGraph)
			return SourceMaterializationStatus(j)
		}
	}
	for _, producer := range j.producers {
		if !producer.emitted {
			j.fail(regalloccheck.UnsupportedOperation)
			return SourceMaterializationStatus(j)
		}
	}
	j.length = length
	j.ended = true
	return SourceMaterializationStatus(j)
}
func SourceMaterializationStatus(j *SourceMaterializationJournal) SourceMaterializationResult {
	if !sourceMaterializationOwnerValid(j) {
		return SourceMaterializationResult{Readiness: SourceMaterializationIncomplete, Reason: regalloccheck.InvalidGraph}
	}
	r := SourceMaterializationResult{Receipts: len(j.entries), Reason: j.reason, Work: j.work, Storage: j.storage, Length: j.length}
	if j.reason != regalloccheck.NoFailure {
		r.Readiness = SourceMaterializationIncomplete
	} else if j.ended {
		r.Readiness = SourceMaterializationRecordingClosed
	}
	return r
}

// Read is owner-live and by value after source sealing. It grants no mutation
// token or ability to mint arbitrary source IDs; retired attempts cannot read.
func SourceMaterializationReceiptAt(j *SourceMaterializationJournal, index int) (SourceMaterializationReceipt, bool) {
	if !sourceMaterializationOwnerValid(j) || !j.ended || j.reason != regalloccheck.NoFailure {
		return SourceMaterializationReceipt{}, false
	}
	if index < 0 || index >= len(j.entries) {
		return SourceMaterializationReceipt{}, j.fail(regalloccheck.InvalidGraph)
	}
	if !j.charge(1, 0) {
		return SourceMaterializationReceipt{}, false
	}
	return j.entries[index].receipt, true
}
func CloseSourceMaterializationJournal(j *SourceMaterializationJournal) {
	if j == nil || j.closed {
		return
	}
	if j.plan != nil && j.plan.materialization == j {
		j.plan.materialization = nil
	}
	j.physical.Close()
	*j = SourceMaterializationJournal{closed: true}
}
