//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// This layer accounts for original source events. Its records are untrusted
// lowering claims: neither readiness nor a node handle is a machine Define.
type SourcePlanReadiness uint8

const (
	SourcePlanIncomplete SourcePlanReadiness = iota
	SourceMappingReady
)

type SourcePlanResult struct {
	Readiness                      SourcePlanReadiness
	Reason                         regalloccheck.FailureReason
	Events, Recipes, Work, Storage int
}

// The first catalogue supports bounded straight-line numeric body shapes with
// at most eight inputs/outputs per event. CFG,
// fusion, SIMD operations, calls and memory require separately audited rules.
type SourceRecipeRule uint8

const (
	SourceRuleAlias SourceRecipeRule = iota + 1
	SourceRuleLiteral
	SourceRuleDrop
	SourceRuleNop
	SourceRuleIntegerBinary
	SourceRuleExit
)

const sourcePlanMaxOperands = 8

type SourcePlanLimits struct{ Events, Values, Recipes, Slots, Transactions int }
type SourcePlanToken struct{ p *sourceRecipePlan }
type SourceEventRef struct {
	p     *sourceRecipePlan
	index int
}
type SourceNodeRef struct {
	p     *sourceRecipePlan
	id    wasm.SourceValueID
	birth uint64
}
type SourceRecipeToken struct {
	p      *sourceRecipePlan
	serial uint64
	index  int
}
type SourceSlotRef struct {
	p          *sourceRecipePlan
	slot       uint32
	generation uint64
}
type SourcePlanMark struct {
	p      *sourceRecipePlan
	serial uint64
	depth  int
}

// Fixed arrays and by-value access keep detached records pointer-free. The
// later physical consumer must check every operand and byte before defining
// any Outputs in the allocation graph. Spans alone prove no correspondence.
type SourceRecipeRecord struct {
	Event                   int
	Rule                    SourceRecipeRule
	Inputs, Outputs         [sourcePlanMaxOperands]wasm.SourceValueID
	InputCount, OutputCount int
	Start, End              int
}

type SourceRecipeRecords struct{ records []SourceRecipeRecord }

func (r SourceRecipeRecords) Len() int                        { return len(r.records) }
func (r SourceRecipeRecords) Record(i int) SourceRecipeRecord { return r.records[i] }

type sourcePlanNode struct {
	birth     uint64
	available bool
}
type sourcePlanRecipe struct {
	record            SourceRecipeRecord
	serial            uint64
	active, committed bool
}
type sourceNodeChange struct {
	id       wasm.SourceValueID
	previous sourcePlanNode
}
type sourceSlotBinding struct {
	node       SourceNodeRef
	generation uint64
	epoch      uint64
}
type sourceRecipeMutation struct {
	index, start, end int
	committed         bool
}
type sourcePlanCheckpoint struct {
	mark                              SourcePlanMark
	next, recipes, changes, mutations int
	journal                           *regalloccheck.EmissionJournal
	journalMark                       regalloccheck.JournalMark
	cursor                            int
}
type sourceRecipePlan struct {
	attempt        *SourceAttempt
	limits         SourcePlanLimits
	nodes          []sourcePlanNode
	recipes        []sourcePlanRecipe // historical entries are never reused on rollback
	changes        []sourceNodeChange
	mutations      []sourceRecipeMutation
	slots          map[uint32]sourceSlotBinding
	marks          []sourcePlanCheckpoint
	next           int
	serial         uint64
	slotEpoch      uint64
	reason         regalloccheck.FailureReason
	work, storage  int
	sealed, closed bool
}

func sourcePlanLimits(r SourcePlanLimits) (SourcePlanLimits, bool) {
	d := SourcePlanLimits{4096, 8192, 8192, 4096, 128}
	for _, pair := range [][2]*int{{&r.Events, &d.Events}, {&r.Values, &d.Values}, {&r.Recipes, &d.Recipes}, {&r.Slots, &d.Slots}, {&r.Transactions, &d.Transactions}} {
		if *pair[0] < 0 {
			return d, false
		}
		if *pair[0] > 0 {
			*pair[1] = min(*pair[0], *pair[1])
		}
	}
	return d, true
}

func sourcePlanAlive(p *sourceRecipePlan) bool {
	return p != nil && !p.closed && !p.sealed && sourcePlanAttemptValid(p.attempt) && p.attempt.plan == p
}
func sourcePlanAttemptValid(t *SourceAttempt) bool {
	if t == nil || t.owner == nil || t.owner.sourceAttempt != t || t.ledger == nil {
		return false
	}
	_, features, ok := codegen.ValidatedSourceContext(t.owner.sourceContext, t.module)
	return ok && t.ledger.ValidFor(t.module, t.function, features)
}
func (p *sourceRecipePlan) fail(reason regalloccheck.FailureReason) bool {
	if p.reason == regalloccheck.NoFailure {
		p.reason = reason
	}
	return false
}

// Fixed-size metadata uses conservative historical pool-entry credits. Work
// and storage are charged from the existing worker history before allocation;
// transactions, retries and close never replenish those credits.
func (p *sourceRecipePlan) charge(work, storage int) bool {
	if !sourcePlanAlive(p) || p.reason != regalloccheck.NoFailure {
		return false
	}
	s := p.attempt.owner
	if work < 0 || storage < 0 || work > s.sourceWork || storage > s.sourceStorage {
		return p.fail(regalloccheck.ResourceLimit)
	}
	s.sourceWork -= work
	s.sourceStorage -= storage
	p.work += work
	p.storage += storage
	return true
}
func (p *sourceRecipePlan) nextSerial() (uint64, bool) {
	if p.serial == ^uint64(0) {
		return 0, p.fail(regalloccheck.ResourceLimit)
	}
	p.serial++
	return p.serial, true
}

// BeginSourcePlan borrows only the ledger exclusively owned by this attempt.
// Failure preserves a pre-existing plan and grants no cleanup authority.
func BeginSourcePlan(t *SourceAttempt, request SourcePlanLimits) (SourcePlanToken, SourcePlanResult) {
	invalid := SourcePlanResult{Reason: regalloccheck.InvalidGraph}
	limits, ok := sourcePlanLimits(request)
	if !ok || !sourcePlanAttemptValid(t) || t.plan != nil {
		return SourcePlanToken{}, invalid
	}
	if t.owner.sourceWork < 1 {
		return SourcePlanToken{}, SourcePlanResult{Reason: regalloccheck.ResourceLimit}
	}
	t.owner.sourceWork-- // failed shape/limit admission is historical work too
	l := t.ledger
	if l.EventCount() > limits.Events || l.ValueCount() > limits.Values {
		return SourcePlanToken{}, SourcePlanResult{Reason: regalloccheck.ResourceLimit, Work: 1}
	}
	// Reserve before constructing even the owner/token or its value index.
	work, storage := l.ValueCount()+l.LocalCount(), l.ValueCount()+1
	if work > t.owner.sourceWork || storage > t.owner.sourceStorage {
		return SourcePlanToken{}, SourcePlanResult{Reason: regalloccheck.ResourceLimit, Work: 1}
	}
	t.owner.sourceWork -= work
	t.owner.sourceStorage -= storage
	p := &sourceRecipePlan{attempt: t, limits: limits, work: work + 1, storage: storage}
	t.plan = p
	p.nodes = make([]sourcePlanNode, l.ValueCount())
	for i := 0; i < l.LocalCount(); i++ {
		id := l.EntryLocal(i)
		p.nodes[int(id)-1].available = true
	}
	return SourcePlanToken{p}, p.result()
}
func (p *sourceRecipePlan) result() SourcePlanResult {
	if p == nil {
		return SourcePlanResult{Reason: regalloccheck.InvalidGraph}
	}
	return SourcePlanResult{Reason: p.reason, Events: p.next, Recipes: len(p.recipes), Work: p.work, Storage: p.storage}
}

func SourceEntryNode(t SourcePlanToken, local int) (SourceNodeRef, bool) {
	p := t.p
	if !sourcePlanAlive(p) || local < 0 || local >= p.attempt.ledger.LocalCount() || !p.charge(1, 0) {
		return SourceNodeRef{}, false
	}
	id := p.attempt.ledger.EntryLocal(local)
	n := p.nodes[int(id)-1]
	return SourceNodeRef{p, id, n.birth}, true
}
func SourcePlanEvent(t SourcePlanToken, index int) (SourceEventRef, bool) {
	p := t.p
	if !sourcePlanAlive(p) || index < 0 || index >= p.attempt.ledger.EventCount() || !p.charge(1, 0) {
		return SourceEventRef{}, false
	}
	return SourceEventRef{p, index}, true
}
func (p *sourceRecipePlan) nodeValid(n SourceNodeRef) bool {
	if n.p != p || n.id == 0 || int(n.id) > len(p.nodes) {
		return false
	}
	state := p.nodes[int(n.id)-1]
	return state.available && state.birth == n.birth
}

func sourceRuleMatches(rule SourceRecipeRule, e wasm.SourceEvent, l *wasm.SourceLedger, index int) bool {
	if e.Unreachable {
		return false
	} // dead/control omission requires its own audited rule
	switch rule {
	case SourceRuleAlias:
		return e.Kind == wasm.InstrLocalGet || e.Kind == wasm.InstrLocalSet || e.Kind == wasm.InstrLocalTee
	case SourceRuleLiteral:
		if e.OutputCount != 1 {
			return false
		}
		switch e.Kind {
		case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const, wasm.InstrV128Const:
			return l.Value(l.Output(index, 0)).Kind == wasm.SourceConstant
		}
	case SourceRuleDrop:
		return e.Kind == wasm.InstrDrop
	case SourceRuleNop:
		return e.Kind == wasm.InstrNop
	case SourceRuleIntegerBinary:
		switch e.Kind {
		case wasm.InstrI32Add, wasm.InstrI32And, wasm.InstrI32Or, wasm.InstrI32Xor, wasm.InstrI64Add, wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Xor:
			if e.InputCount != 2 || e.OutputCount != 1 {
				return false
			}
			v := l.Value(l.Output(index, 0))
			return v.Kind == wasm.SourceResult && (v.Type == wasm.I32 || v.Type == wasm.I64) &&
				l.Value(l.Input(index, 0)).Type == v.Type && l.Value(l.Input(index, 1)).Type == v.Type
		}
	case SourceRuleExit:
		return e.FunctionEnd && e.Terminal && e.Control == wasm.SourceControlEnd
	}
	return false
}

// BeginSourceRecipe consumes one audited original event in lexical order.
// Multiple producers may remain pending while lowering defers materialization.
// Outputs expose desired source identities only, never physical proof facts.
func BeginSourceRecipe(t SourcePlanToken, rule SourceRecipeRule, event SourceEventRef, inputs []SourceNodeRef) (SourceRecipeToken, bool) {
	p := t.p
	if !sourcePlanAlive(p) {
		return SourceRecipeToken{}, false
	}
	if !p.charge(1+len(inputs), 0) {
		return SourceRecipeToken{}, false
	}
	l := p.attempt.ledger
	if event.p != p || event.index != p.next || p.next >= l.EventCount() {
		return SourceRecipeToken{}, p.fail(regalloccheck.InvalidGraph)
	}
	e := l.Event(event.index)
	if len(inputs) > sourcePlanMaxOperands || e.OutputCount > sourcePlanMaxOperands {
		return SourceRecipeToken{}, p.fail(regalloccheck.ResourceLimit)
	}
	if !sourceRuleMatches(rule, e, l, event.index) {
		return SourceRecipeToken{}, p.fail(regalloccheck.UnsupportedOperation)
	}
	if e.InputCount != len(inputs) {
		return SourceRecipeToken{}, p.fail(regalloccheck.InvalidGraph)
	}
	for i, n := range inputs {
		if !p.nodeValid(n) || n.id != l.Input(event.index, i) {
			return SourceRecipeToken{}, p.fail(regalloccheck.InvalidGraph)
		}
	}
	if len(p.recipes) >= p.limits.Recipes || !p.charge(e.OutputCount+1, e.OutputCount+1) {
		return SourceRecipeToken{}, p.fail(regalloccheck.ResourceLimit)
	}
	serial, ok := p.nextSerial()
	if !ok {
		return SourceRecipeToken{}, false
	}
	record := SourceRecipeRecord{Event: event.index, Rule: rule, InputCount: len(inputs), OutputCount: e.OutputCount}
	for i, n := range inputs {
		record.Inputs[i] = n.id
	}
	for i := 0; i < e.OutputCount; i++ {
		id := l.Output(event.index, i)
		record.Outputs[i] = id
		state := p.nodes[int(id)-1]
		if rule == SourceRuleAlias && !state.available {
			return SourceRecipeToken{}, p.fail(regalloccheck.InvalidGraph)
		}
		if !state.available {
			p.changes = append(p.changes, sourceNodeChange{id, state})
			p.nodes[int(id)-1] = sourcePlanNode{serial, true}
		}
	}
	p.recipes = append(p.recipes, sourcePlanRecipe{record: record, serial: serial, active: true})
	p.next++
	return SourceRecipeToken{p, serial, len(p.recipes) - 1}, true
}
func sourceRecipeValid(t SourceRecipeToken) bool {
	return sourcePlanAlive(t.p) && t.index >= 0 && t.index < len(t.p.recipes) && t.p.recipes[t.index].serial == t.serial && t.p.recipes[t.index].active
}
func SourceRecipeOutput(t SourceRecipeToken, index int) (SourceNodeRef, bool) {
	if !sourceRecipeValid(t) || !t.p.charge(1, 0) {
		return SourceNodeRef{}, false
	}
	r := t.p.recipes[t.index].record
	if index < 0 || index >= r.OutputCount {
		return SourceNodeRef{}, t.p.fail(regalloccheck.InvalidGraph)
	}
	id := r.Outputs[index]
	return SourceNodeRef{t.p, id, t.p.nodes[int(id)-1].birth}, true
}
func CommitSourceRecipe(t SourceRecipeToken, start, end int) bool {
	if !sourceRecipeValid(t) {
		return false
	}
	p := t.p
	if !p.charge(1, 0) {
		return false
	}
	r := &p.recipes[t.index]
	if r.committed || start < 0 || end < start {
		return p.fail(regalloccheck.InvalidGraph)
	}
	if len(p.marks) != 0 {
		if len(p.mutations) >= p.limits.Recipes || !p.charge(1, 1) {
			return p.fail(regalloccheck.ResourceLimit)
		}
		p.mutations = append(p.mutations, sourceRecipeMutation{t.index, r.record.Start, r.record.End, r.committed})
	}
	// Empty spans explicitly permit deferred/eliminated source claims. They
	// cannot satisfy any future physical Use without checked materialization.
	r.record.Start = start
	r.record.End = end
	r.committed = true
	return true
}

// Slots are logical pool slots supplied by an adapter, never pointer identity.
// Every bind/rebind has a fresh historical generation. A recycled slot cannot
// make an old SourceSlotRef valid even when its desired value is unchanged.
func BindSourceSlot(t SourcePlanToken, slot uint32, node SourceNodeRef) (SourceSlotRef, bool) {
	p := t.p
	if !sourcePlanAlive(p) {
		return SourceSlotRef{}, false
	}
	if !p.nodeValid(node) {
		return SourceSlotRef{}, p.fail(regalloccheck.InvalidGraph)
	}
	if int64(slot) >= int64(p.limits.Slots) || !p.charge(1, 1) {
		return SourceSlotRef{}, p.fail(regalloccheck.ResourceLimit)
	}
	serial, ok := p.nextSerial()
	if !ok {
		return SourceSlotRef{}, false
	}
	if p.slots == nil {
		p.slots = make(map[uint32]sourceSlotBinding)
	}
	p.slots[slot] = sourceSlotBinding{node, serial, p.slotEpoch}
	return SourceSlotRef{p, slot, serial}, true
}
func SourceSlotNode(t SourceSlotRef) (SourceNodeRef, bool) {
	p := t.p
	if !sourcePlanAlive(p) || !p.charge(1, 0) {
		return SourceNodeRef{}, false
	}
	b, ok := p.slots[t.slot]
	if !ok || b.generation != t.generation || b.epoch != p.slotEpoch || !p.nodeValid(b.node) {
		return SourceNodeRef{}, false
	}
	return b.node, true
}
func ReleaseSourceSlot(t SourceSlotRef) bool {
	p := t.p
	if !sourcePlanAlive(p) || !p.charge(1, 0) {
		return false
	}
	b, ok := p.slots[t.slot]
	if !ok || b.generation != t.generation || b.epoch != p.slotEpoch {
		return false
	}
	delete(p.slots, t.slot)
	return true
}

// An optional journal is checkpointed and committed/rolled back by the same
// token. A foreign/LIFO/cursor mismatch is sticky and cannot become ready.
func CheckpointSourcePlan(t SourcePlanToken, journal *regalloccheck.EmissionJournal, cursor int) (SourcePlanMark, bool) {
	p := t.p
	if !sourcePlanAlive(p) {
		return SourcePlanMark{}, false
	}
	if cursor < 0 {
		return SourcePlanMark{}, p.fail(regalloccheck.InvalidGraph)
	}
	// Reserve one work credit for allocation-free abandonment during cleanup.
	if len(p.marks) >= p.limits.Transactions || !p.charge(2, 1) {
		return SourcePlanMark{}, p.fail(regalloccheck.ResourceLimit)
	}
	serial, ok := p.nextSerial()
	if !ok {
		return SourcePlanMark{}, false
	}
	mark := SourcePlanMark{p, serial, len(p.marks) + 1}
	m := sourcePlanCheckpoint{mark: mark, next: p.next, recipes: len(p.recipes), changes: len(p.changes), mutations: len(p.mutations), journal: journal, cursor: cursor}
	if journal != nil {
		m.journalMark = journal.Checkpoint(cursor)
		if journal.Result().State != regalloccheck.JournalRecording {
			return SourcePlanMark{}, p.fail(regalloccheck.InvalidGraph)
		}
	}
	p.marks = append(p.marks, m)
	return mark, true
}
func sourcePlanTop(t SourcePlanToken, m SourcePlanMark) (sourcePlanCheckpoint, bool) {
	p := t.p
	if !sourcePlanAlive(p) {
		return sourcePlanCheckpoint{}, false
	}
	if m.p != p || m.depth != len(p.marks) || len(p.marks) == 0 || p.marks[len(p.marks)-1].mark.serial != m.serial {
		return sourcePlanCheckpoint{}, p.fail(regalloccheck.InvalidGraph)
	}
	return p.marks[len(p.marks)-1], true
}
func CommitSourcePlanMark(t SourcePlanToken, m SourcePlanMark) bool {
	checkpoint, ok := sourcePlanTop(t, m)
	if !ok {
		return false
	}
	work := 1
	if len(t.p.marks) == 1 {
		work += len(t.p.mutations)
	}
	if !t.p.charge(work, 0) {
		return false
	}
	if checkpoint.journal != nil && !checkpoint.journal.Commit(checkpoint.journalMark) {
		return t.p.fail(regalloccheck.InvalidGraph)
	}
	t.p.marks[len(t.p.marks)-1] = sourcePlanCheckpoint{}
	t.p.marks = t.p.marks[:len(t.p.marks)-1]
	if len(t.p.marks) == 0 {
		clear(t.p.mutations)
		t.p.mutations = t.p.mutations[:0]
	}
	return true
}
func RollbackSourcePlan(t SourcePlanToken, m SourcePlanMark, truncatedCursor int) bool {
	checkpoint, ok := sourcePlanTop(t, m)
	if !ok {
		return false
	}
	p := t.p
	if truncatedCursor != checkpoint.cursor {
		return p.fail(regalloccheck.InvalidGraph)
	}
	if !p.charge(len(p.recipes)-checkpoint.recipes+len(p.changes)-checkpoint.changes+len(p.mutations)-checkpoint.mutations+1, 0) {
		return false
	}
	if checkpoint.journal != nil && !checkpoint.journal.Rollback(checkpoint.journalMark, truncatedCursor) {
		return p.fail(regalloccheck.InvalidGraph)
	}
	for i := len(p.mutations) - 1; i >= checkpoint.mutations; i-- {
		m := p.mutations[i]
		r := &p.recipes[m.index]
		r.record.Start, r.record.End, r.committed = m.start, m.end, m.committed
	}
	clear(p.mutations[checkpoint.mutations:])
	p.mutations = p.mutations[:checkpoint.mutations]
	for i := checkpoint.recipes; i < len(p.recipes); i++ {
		p.recipes[i].active = false
	}
	for i := len(p.changes) - 1; i >= checkpoint.changes; i-- {
		c := p.changes[i]
		p.nodes[int(c.id)-1] = c.previous
	}
	p.changes = p.changes[:checkpoint.changes]
	p.next = checkpoint.next
	// Conservative invalidation avoids reviving pre-rewind pool handles. An
	// adapter must explicitly rebind its surviving logical slots afterwards.
	epoch, ok := p.nextSerial()
	if !ok {
		return false
	}
	p.slotEpoch = epoch
	p.marks[len(p.marks)-1] = sourcePlanCheckpoint{}
	p.marks = p.marks[:len(p.marks)-1]
	return true
}
func RecordSourcePlanGap(t SourcePlanToken) {
	if sourcePlanAlive(t.p) {
		t.p.fail(regalloccheck.UnsupportedOperation)
	}
}

// Seal transfers detached value records; it never produces a machine verdict.
// Every original event must be accounted for and every pending recipe closed.
func SealSourcePlan(t SourcePlanToken) (SourceRecipeRecords, SourcePlanResult) {
	p := t.p
	if !sourcePlanAlive(p) {
		return SourceRecipeRecords{}, SourcePlanResult{Reason: regalloccheck.InvalidGraph}
	}
	if p.next != p.attempt.ledger.EventCount() || len(p.marks) != 0 {
		p.fail(regalloccheck.UnsupportedOperation)
	}
	if !p.charge(len(p.recipes)+1, 0) {
		return SourceRecipeRecords{}, p.result()
	}
	count := 0
	for _, r := range p.recipes {
		if r.active {
			if !r.committed {
				p.fail(regalloccheck.UnsupportedOperation)
				return SourceRecipeRecords{}, p.result()
			}
			count++
		}
	}
	if !p.charge(count, count) {
		return SourceRecipeRecords{}, p.result()
	}
	records := make([]SourceRecipeRecord, 0, count)
	for _, r := range p.recipes {
		if r.active {
			records = append(records, r.record)
		}
	}
	p.sealed = true
	r := p.result()
	r.Readiness = SourceMappingReady
	return SourceRecipeRecords{records}, r
}
func CloseSourcePlan(t SourcePlanToken) {
	p := t.p
	if p == nil || p.closed {
		return
	}
	// Actual bytes are not known to have been truncated on an unwind. Abandon
	// only our paired marks and make that journal permanently incomplete,
	// preserving enclosing ownership rather than forging successful rollback.
	for i := len(p.marks) - 1; i >= 0; i-- {
		m := p.marks[i]
		if m.journal != nil {
			m.journal.Abandon(m.journalMark)
		}
	}
	if p.attempt != nil && p.attempt.plan == p {
		p.attempt.plan = nil
	}
	*p = sourceRecipePlan{closed: true}
}
