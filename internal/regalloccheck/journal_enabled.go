//go:build wago_regalloccheck

package regalloccheck

// JournalState describes observation storage, never a machine-code verdict.
type JournalState uint8

const (
	JournalRecording JournalState = iota
	JournalReady                  // finalized observations; does not assert complete coverage
	JournalIncomplete
	JournalInvalid
	JournalClosed
)

type JournalKind uint8

const (
	JournalEffect JournalKind = iota
	JournalGPWrites
	JournalGap
)

// JournalEvent is returned by value. Start/End bracket the explicitly admitted
// encoder operation or composite recipe, not the individual callback. Encoder
// callbacks can run before or after bytes are appended. Callback order matters:
// GP-write notifications and Copy effects may describe the same instruction.
// Consumers must reconcile them under an admitted recipe, not blindly turn
// each GP notification into a Kill or silently ignore it.
type JournalEvent struct {
	Kind       JournalKind
	Effect     Effect
	GPWrites   uint32
	Start, End int
}

type JournalLimits struct {
	Events, Spans, Transactions, Work int
}

func DefaultJournalLimits() JournalLimits {
	return JournalLimits{Events: 262144, Spans: 262144, Transactions: 1024, Work: 8388608}
}

type JournalResult struct {
	State                     JournalState
	Reason                    FailureReason
	Message                   string
	Work, Events, Spans       int
	EventHistory, SpanHistory int
}

type journalObservation struct {
	kind   JournalKind
	effect Effect
	writes uint32
	span   int
}

type journalSpan struct{ start, end int }

// JournalMark is an opaque, journal-specific LIFO transaction token.
type JournalMark struct {
	owner  *EmissionJournal
	serial uint64
	depth  int
}

type journalCheckpoint struct {
	mark                        JournalMark
	cursor, events, spans, gaps int
}

// EmissionJournal owns checked-only observations for one function. It is not
// safe for concurrent use. No compiler owners, source identities or frame-origin
// assumptions enter this storage layer. Ready does not establish that every
// emitted byte was instrumented or that a final rewrite preserved its effects.
// A complete source/recipe/ABI coverage contract is a separate requirement.
//
// Transactions cover append-only speculative emission followed by truncation.
// Matching byte lengths cannot prove restoration of edits to a retained prefix.
// Resource/history charges and structural errors survive every rollback.
type EmissionJournal struct {
	limits                                        JournalLimits
	events                                        []journalObservation
	spans                                         []journalSpan
	transactions                                  []journalCheckpoint
	cursor, gaps, work, eventHistory, spanHistory int
	serial                                        uint64
	activeEvents, activeGaps                      int
	active, sealed, finalizing, closed            bool
	reason                                        FailureReason
	message                                       string
}

func NewEmissionJournal(l JournalLimits) *EmissionJournal {
	j := &EmissionJournal{}
	d := DefaultJournalLimits()
	for _, p := range [][2]*int{{&l.Events, &d.Events}, {&l.Spans, &d.Spans}, {&l.Transactions, &d.Transactions}, {&l.Work, &d.Work}} {
		if *p[0] < 0 {
			j.fail(InvalidGraph, "invalid journal limits")
		} else if *p[0] == 0 {
			*p[0] = *p[1]
		} else {
			*p[0] = min(*p[0], *p[1])
		}
	}
	j.limits = l
	return j
}

func (j *EmissionJournal) fail(reason FailureReason, message string) {
	if j.reason == NoFailure {
		j.reason, j.message = reason, message
	}
}

func (j *EmissionJournal) recording() bool {
	if j.closed || j.sealed || j.finalizing {
		j.fail(InvalidGraph, "journal is closed or finalized")
	}
	return j.reason == NoFailure && !j.closed && !j.sealed && !j.finalizing
}

func (j *EmissionJournal) charge(n int) bool {
	if n > j.limits.Work-j.work {
		j.fail(ResourceLimit, "journal work limit")
		return false
	}
	j.work += n
	return true
}

// BeginEmission must precede all callbacks for one admitted encoder operation.
// Spans may include unobserved bytes; their presence is not a coverage proof.
// Start must equal the retained cursor, including after a rollback.
func (j *EmissionJournal) BeginEmission(start int) bool {
	if !j.recording() || !j.charge(1) {
		return false
	}
	if j.active || start != j.cursor || start < 0 {
		j.fail(InvalidGraph, "invalid emission start")
		return false
	}
	if j.spanHistory == j.limits.Spans {
		j.fail(ResourceLimit, "journal span history limit")
		return false
	}
	j.spanHistory++
	j.spans = append(j.spans, journalSpan{start, start})
	j.activeEvents, j.activeGaps = len(j.events), j.gaps
	j.active = true
	return true
}

// AbortEmission follows actual truncation of a failed/incomplete admitted
// operation to its starting byte length. It does not erase structural errors.
func (j *EmissionJournal) AbortEmission(truncatedCursor int) bool {
	if !j.recording() || !j.charge(1) {
		return false
	}
	if !j.active || truncatedCursor != j.cursor {
		j.fail(InvalidGraph, "invalid emission abort cursor")
		return false
	}
	if !j.charge(len(j.events) - j.activeEvents + 1) {
		return false
	}
	clear(j.events[j.activeEvents:])
	j.events = j.events[:j.activeEvents]
	j.spans[len(j.spans)-1] = journalSpan{}
	j.spans = j.spans[:len(j.spans)-1]
	j.gaps, j.active = j.activeGaps, false
	return true
}

func (j *EmissionJournal) EndEmission(end int) bool {
	if !j.recording() || !j.charge(1) {
		return false
	}
	if !j.active || end <= j.cursor {
		j.fail(InvalidGraph, "invalid or empty emission span")
		return false
	}
	j.spans[len(j.spans)-1].end = end
	j.cursor, j.active = end, false
	return true
}

func (j *EmissionJournal) append(e journalObservation) bool {
	if !j.recording() || !j.charge(1) {
		return false
	}
	if !j.active {
		j.fail(InvalidGraph, "observation outside an emission span")
		return false
	}
	if j.eventHistory == j.limits.Events {
		j.fail(ResourceLimit, "journal event history limit")
		return false
	}
	j.eventHistory++
	e.span = len(j.spans) - 1
	j.events = append(j.events, e)
	return true
}

func (j *EmissionJournal) ObserveEffect(e Effect) {
	if !machineEffectValid(e) {
		j.fail(InvalidGraph, "invalid observed effect")
		return
	}
	j.append(journalObservation{kind: JournalEffect, effect: e})
}

func (j *EmissionJournal) ObserveGPWrites(mask uint32) {
	if mask == 0 {
		j.fail(InvalidGraph, "empty GP-write notification")
		return
	}
	j.append(journalObservation{kind: JournalGPWrites, writes: mask})
}

// ObserveGap records explicitly unsupported emission in the current span.
// A gap belonging to discarded speculative bytes disappears on rollback.
// Raw or unmodeled instructions need this marker until independently admitted
// by a target adapter; accepting an effect does not authenticate its encoding.
func (j *EmissionJournal) ObserveGap() {
	if j.append(journalObservation{kind: JournalGap}) {
		j.gaps++
	}
}

func (j *EmissionJournal) Checkpoint(cursor int) JournalMark {
	if !j.recording() || !j.charge(1) {
		return JournalMark{}
	}
	if j.active || cursor != j.cursor || cursor < 0 {
		j.fail(InvalidGraph, "invalid journal checkpoint")
		return JournalMark{}
	}
	if len(j.transactions) == j.limits.Transactions {
		j.fail(ResourceLimit, "journal transaction depth limit")
		return JournalMark{}
	}
	j.serial++ // unique while Work bounds the number of checkpoints
	m := JournalMark{j, j.serial, len(j.transactions) + 1}
	j.transactions = append(j.transactions, journalCheckpoint{m, cursor, len(j.events), len(j.spans), j.gaps})
	return m
}

func (j *EmissionJournal) top(m JournalMark) (journalCheckpoint, bool) {
	if !j.recording() || !j.charge(1) {
		return journalCheckpoint{}, false
	}
	if j.active || len(j.transactions) == 0 || m.owner != j || m.depth != len(j.transactions) || j.transactions[len(j.transactions)-1].mark != m {
		j.fail(InvalidGraph, "stale, foreign or non-LIFO journal token")
		return journalCheckpoint{}, false
	}
	return j.transactions[len(j.transactions)-1], true
}

func (j *EmissionJournal) Commit(m JournalMark) bool {
	if _, ok := j.top(m); !ok {
		return false
	}
	j.transactions[len(j.transactions)-1] = journalCheckpoint{}
	j.transactions = j.transactions[:len(j.transactions)-1]
	return true
}

// Abandon releases one owned LIFO mark on unwind without claiming actual byte
// truncation. The journal remains invalid, even if an enclosing mark survives.
// Unlike ordinary recording, cleanup must also work after quota exhaustion.
// Checkpoint's caller reserves cleanup work; this operation allocates nothing.
func (j *EmissionJournal) Abandon(m JournalMark) bool {
	if j == nil || j.closed {
		return false
	}
	if j.finalizing || len(j.transactions) == 0 || m.owner != j || m.depth != len(j.transactions) || j.transactions[len(j.transactions)-1].mark != m {
		j.fail(InvalidGraph, "foreign or non-LIFO journal abandonment")
		return false
	}
	j.transactions[len(j.transactions)-1] = journalCheckpoint{}
	j.transactions = j.transactions[:len(j.transactions)-1]
	j.active = false // any active span began inside this owned checkpoint
	j.fail(InvalidGraph, "abandoned journal transaction")
	return true
}

// Rollback follows actual byte truncation. The caller supplies its new byte
// length, which must match the checkpoint. No retained prefix rewrite is proved.
func (j *EmissionJournal) Rollback(m JournalMark, truncatedCursor int) bool {
	c, ok := j.top(m)
	if !ok {
		return false
	}
	if truncatedCursor != c.cursor {
		j.fail(InvalidGraph, "rollback byte cursor mismatch")
		return false
	}
	if !j.charge(len(j.events) - c.events + len(j.spans) - c.spans) {
		return false
	}
	clear(j.events[c.events:])
	clear(j.spans[c.spans:])
	j.events, j.spans = j.events[:c.events], j.spans[:c.spans]
	j.cursor, j.gaps = c.cursor, c.gaps
	j.transactions[len(j.transactions)-1] = journalCheckpoint{}
	j.transactions = j.transactions[:len(j.transactions)-1]
	return true
}

// Finalize accepts a pure old-boundary -> final-boundary mapper. Each distinct
// boundary is mapped once. Nil uses identity. A collapsed nonempty span cannot
// silently remove its observations. Mapper errors/panics leave a sticky failure
// and no accessible partial snapshot; the mapper is never retained.
// Offset coherence alone does not prove final branch targets or rewritten bytes.
func (j *EmissionJournal) Finalize(oldLen, finalLen int, mapper func(int) (int, bool)) (result JournalResult) {
	if !j.recording() || !j.charge(1) {
		return j.Result()
	}
	if j.active || len(j.transactions) != 0 || oldLen != j.cursor || finalLen < 0 {
		j.fail(InvalidGraph, "unfinished journal or final length mismatch")
		return j.Result()
	}
	// Prevent mapper reentrancy. An unwinding panic cannot leave a reusable
	// partial remap; resource failures keep their more specific reason.
	j.finalizing = true
	complete := false
	defer func() {
		j.finalizing = false
		if !complete {
			j.fail(InvalidGraph, "incomplete journal offset mapping")
		}
		result = j.Result()
	}()
	previousOld, previousNew := -1, -1
	mapBoundary := func(old int) (int, bool) {
		if old == previousOld {
			return previousNew, true
		}
		if old < previousOld || !j.charge(1) {
			return 0, false
		}
		next, ok := old, true
		if mapper != nil {
			next, ok = mapper(old)
		}
		if j.reason != NoFailure || !ok || next < previousNew || next < 0 || next > finalLen {
			return 0, false
		}
		previousOld, previousNew = old, next
		return next, true
	}
	for i, s := range j.spans {
		start, ok := mapBoundary(s.start)
		if !ok {
			return j.Result()
		}
		end, ok := mapBoundary(s.end)
		if !ok || end <= start {
			return j.Result()
		}
		j.spans[i] = journalSpan{start, end}
	}
	last, ok := mapBoundary(oldLen)
	if !ok || last != finalLen {
		return j.Result()
	}
	if j.reason != NoFailure {
		return j.Result()
	}
	j.reason, j.message, j.sealed = NoFailure, "", true
	complete = true
	return j.Result()
}

func (j *EmissionJournal) Result() JournalResult {
	r := JournalResult{Reason: j.reason, Message: j.message, Work: j.work, Events: len(j.events), Spans: len(j.spans), EventHistory: j.eventHistory, SpanHistory: j.spanHistory}
	switch {
	case j.closed:
		r.State = JournalClosed
	case j.reason == ResourceLimit:
		r.State = JournalIncomplete
	case j.reason != NoFailure:
		r.State = JournalInvalid
	case j.sealed && j.gaps != 0:
		r.State, r.Reason, r.Message = JournalIncomplete, UnsupportedOperation, "observed unsupported emission"
	case j.sealed:
		r.State = JournalReady
	default:
		r.State = JournalRecording
	}
	return r
}

func (j *EmissionJournal) Event(i int) (JournalEvent, bool) {
	if j.closed || !j.sealed || j.reason != NoFailure || i < 0 || i >= len(j.events) {
		return JournalEvent{}, false
	}
	e := j.events[i]
	s := j.spans[e.span]
	return JournalEvent{e.kind, e.effect, e.writes, s.start, s.end}, true
}

func (j *EmissionJournal) Close() {
	if j.finalizing {
		j.fail(InvalidGraph, "journal close during offset mapping")
		return
	}
	// Drop every retained pool and transaction's owner reference. Encoder
	// observer composition/restoration belongs to the enclosing adapter scope.
	j.events, j.spans, j.transactions = nil, nil, nil
	j.active, j.sealed, j.closed = false, false, true
	j.cursor, j.gaps = 0, 0
}
