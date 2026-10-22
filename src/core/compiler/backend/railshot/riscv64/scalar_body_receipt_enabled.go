//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"bytes"
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/internal/regalloccheck"
)

// Standalone checked receipt storage, not a target adapter or a graph verdict.
// It authenticates exact bytes/event traces and registered control rewrites.
// Fixed-SP/header/pointer preservation and final framing remain separate proofs.
// Raw callbacks do not authenticate semantic ALU/branch operand reads before
// capture; those remain under the existing semantic-definition contract.
type scalarReceiptLimits struct{ Bytes, Spans, Events, Storage, Work int }

type scalarReceiptEnvelope struct{ Start, Site, End int }
type scalarReceiptSpan struct {
	Start, End  int
	Envelope    scalarReceiptEnvelope
	HasEnvelope bool
	Conditional bool
	Patched     bool
	Target      int64
}

type scalarRewriteTicket struct {
	owner  *scalarBodyReceipt
	serial uint64
	span   int
	target int
}

type scalarBodyReceipt struct {
	self                        *scalarBodyReceipt // populated receipts cannot be copied
	limits                      scalarReceiptLimits
	data                        []byte // exclusively owned; never exposed to callers
	spans                       []scalarReceiptSpan
	summary                     scalarWordAuditSummary
	eventHistory, storage, work int
	serial                      uint64
	pending                     scalarRewriteTicket
	sealed, closed, failed      bool
	failure                     regalloccheck.Result
}

func newScalarBodyReceipt(limits scalarReceiptLimits) *scalarBodyReceipt {
	r := &scalarBodyReceipt{}
	r.self = r
	defaults := scalarReceiptLimits{Bytes: 1 << 20, Spans: 1 << 16, Events: 1 << 18, Storage: 16 << 20, Work: 8 << 20}
	for _, pair := range [][2]*int{{&limits.Bytes, &defaults.Bytes}, {&limits.Spans, &defaults.Spans}, {&limits.Events, &defaults.Events}, {&limits.Storage, &defaults.Storage}, {&limits.Work, &defaults.Work}} {
		if *pair[0] < 0 {
			r.fail(scalarSpanFailure(regalloccheck.InvalidGraph, "invalid scalar receipt budget"))
		} else if *pair[0] == 0 || *pair[0] > *pair[1] {
			*pair[0] = *pair[1]
		}
	}
	r.limits = limits
	return r
}

func (r *scalarBodyReceipt) fail(status regalloccheck.Result) bool {
	if !r.failed || r.failure.Verdict != regalloccheck.Rejected && status.Verdict == regalloccheck.Rejected {
		r.failure = status
	}
	r.failed = true
	return false
}
func (r *scalarBodyReceipt) charge(work, storage int) bool {
	if work < 0 || storage < 0 || work > r.limits.Work-r.work || storage > r.limits.Storage-r.storage {
		return r.fail(scalarSpanFailure(regalloccheck.ResourceLimit, "scalar receipt history budget"))
	}
	r.work += work
	r.storage += storage
	return true
}
func (r *scalarBodyReceipt) owned() bool {
	if r.self != r {
		return r.fail(scalarSpanFailure(regalloccheck.InvalidGraph, "copied or uninitialized scalar receipt"))
	}
	return true
}
func (r *scalarBodyReceipt) writable() bool {
	if !r.owned() {
		return false
	}
	if r.failed {
		return false
	}
	if r.closed || r.sealed || r.pending.owner != nil {
		return r.fail(scalarSpanFailure(regalloccheck.InvalidGraph, "scalar receipt is closed, sealed or rewriting"))
	}
	return true
}

// Growth charges full allocation history before make, including backing arrays
// replaced by growth. Capacity never exceeds the corresponding explicit cap.
func (r *scalarBodyReceipt) growData(needed int) bool {
	if needed <= cap(r.data) {
		return true
	}
	next := min(r.limits.Bytes, max(needed, max(256, 2*cap(r.data))))
	if !r.charge((len(r.data)+3)/4, next) {
		return false
	}
	data := make([]byte, len(r.data), next)
	copy(data, r.data)
	r.data = data
	return true
}
func (r *scalarBodyReceipt) growSpans() bool {
	if len(r.spans) < cap(r.spans) {
		return true
	}
	next := min(r.limits.Spans, max(8, 2*cap(r.spans)))
	if !r.charge(len(r.spans), next*int(unsafe.Sizeof(scalarReceiptSpan{}))) {
		return false
	}
	spans := make([]scalarReceiptSpan, len(r.spans), next)
	copy(spans, r.spans)
	r.spans = spans
	return true
}

// Append registers ONE complete outer target-operation span, including empty
// operations. Its start must equal the prior cursor, beginning at zero, and its
// end must equal the actual code length. Nested helper spans must not be passed
// as independent operations. Real encoder callbacks supply events; requested
// registers, frame slots and semantic values are not inputs to this method.
func (r *scalarBodyReceipt) Append(code []byte, start, end int, events []scalarWordObservation, envelope *scalarReceiptEnvelope) (int, bool) {
	if !r.writable() {
		return -1, false
	}
	if start != len(r.data) || start < 0 || end < start || end != len(code) || start&3 != 0 || end&3 != 0 {
		return -1, r.fail(scalarSpanFailure(regalloccheck.InvalidGraph, "scalar operation coverage gap or overlap"))
	}
	if end > r.limits.Bytes || len(r.spans) == r.limits.Spans || len(events) > r.limits.Events-r.eventHistory {
		return -1, r.fail(scalarSpanFailure(regalloccheck.ResourceLimit, "scalar receipt count budget"))
	}
	// Charge decoder words, maximum six events, owned-byte copying and fixed
	// envelope validation before work, even on early failure. No history refund.
	words := (end - start) / 4
	if !r.charge(4+8*words, 0) {
		return -1, false
	}
	r.eventHistory += len(events)
	summary, status, ok := auditScalarWordTrace(code, start, end, events, scalarWordAuditLimits{Words: max(1, words), Events: max(1, len(events)), Work: max(1, 7*words)})
	if !ok {
		return -1, r.fail(status)
	}
	span := scalarReceiptSpan{Start: start, End: end}
	if envelope != nil {
		if envelope.Start < start || envelope.End != end || envelope.Site < envelope.Start {
			return -1, r.fail(scalarSpanFailure(regalloccheck.InvalidGraph, "scalar envelope outside its operation"))
		}
		_, conditional, status, decoded := decodeScalarLongEnvelope(code, envelope.Start, envelope.Site, envelope.End)
		if !decoded {
			return -1, r.fail(status)
		}
		controls := 1
		if conditional {
			controls++
		}
		if summary.Controls != controls {
			return -1, r.fail(scalarSpanFailure(regalloccheck.UnsupportedOperation, "unregistered control inside scalar operation"))
		}
		span.Envelope, span.HasEnvelope, span.Conditional = *envelope, true, conditional
	} else if summary.Controls != 0 {
		return -1, r.fail(scalarSpanFailure(regalloccheck.UnsupportedOperation, "scalar control has no registered envelope"))
	}
	if !r.growData(end) || !r.growSpans() {
		return -1, false
	}
	r.data = append(r.data, code[start:end]...)
	r.spans = append(r.spans, span)
	r.summary.Words += summary.Words
	r.summary.Events += summary.Events
	r.summary.Loads += summary.Loads
	r.summary.Stores += summary.Stores
	r.summary.Controls += summary.Controls
	r.summary.SPWrites += summary.SPWrites
	return len(r.spans) - 1, true
}

// Only outer-operation boundaries and current body end are legal entry points.
// A word-aligned normalization/address/return interior is not an entry boundary.
func (r *scalarBodyReceipt) boundary(position int) bool {
	if position < 0 || position > len(r.data) {
		return false
	}
	if position == 0 || position == len(r.data) {
		return true
	}
	lo, hi := 0, len(r.spans)
	for lo < hi {
		if !r.charge(1, 0) {
			return false
		}
		mid := lo + (hi-lo)/2
		if r.spans[mid].End < position {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo < len(r.spans) && r.spans[lo].End == position
}

func (r *scalarBodyReceipt) sameBytes(code []byte) bool {
	if len(code) != len(r.data) {
		return r.fail(scalarSpanMismatch("scalar body length changed after receipt"))
	}
	if !r.charge(1+(len(code)+3)/4, 0) {
		return false
	}
	if !bytes.Equal(code, r.data) {
		return r.fail(scalarSpanMismatch("scalar body bytes changed after receipt"))
	}
	return true
}

// BeginRewrite authenticates the complete preimage and a legal already-known
// destination before allowing one registered fixup transaction. It changes no
// machine bytes. The caller must pair it with CommitRewrite or discard/Close
// this receipt on errors and panics. Incomplete transactions cannot seal.
func (r *scalarBodyReceipt) BeginRewrite(code []byte, id, target int) (scalarRewriteTicket, bool) {
	if !r.writable() {
		return scalarRewriteTicket{}, false
	}
	if id < 0 || id >= len(r.spans) || !r.spans[id].HasEnvelope || r.spans[id].Patched {
		return scalarRewriteTicket{}, r.fail(scalarSpanFailure(regalloccheck.InvalidGraph, "unknown or repeated scalar fixup"))
	}
	if !r.boundary(target) {
		if r.failed {
			return scalarRewriteTicket{}, false
		}
		return scalarRewriteTicket{}, r.fail(scalarSpanMismatch("scalar fixup enters an operation interior or outside body"))
	}
	if !r.sameBytes(code) || !r.charge(1, 0) {
		return scalarRewriteTicket{}, false
	}
	r.serial++ // charged attempts cannot reach overflow under the fixed Work cap
	r.pending = scalarRewriteTicket{owner: r, serial: r.serial, span: id, target: target}
	return r.pending, true
}

// CommitRewrite accepts only the two immediate fields of the recorded
// non-linking AUIPC/JALR envelope. The actual decoded target and branch kind
// must match the authorized boundary and original shape. Other byte mutations
// cannot be hidden by updating a narrow receipt. No budgets are refunded.
func (r *scalarBodyReceipt) CommitRewrite(code []byte, ticket scalarRewriteTicket) bool {
	if !r.owned() {
		return false
	}
	if r.failed {
		return false
	}
	if r.closed || r.sealed || ticket.owner != r || ticket != r.pending || ticket.owner == nil {
		return r.fail(scalarSpanFailure(regalloccheck.InvalidGraph, "invalid scalar fixup transaction"))
	}
	r.pending = scalarRewriteTicket{}
	span := &r.spans[ticket.span]
	if len(code) != len(r.data) {
		return r.fail(scalarSpanMismatch("scalar fixup changed code extent"))
	}
	if !r.charge(16+(len(code)+3)/4, 0) {
		return false
	}
	site, end := span.Envelope.Site, span.Envelope.End
	if !bytes.Equal(code[:site], r.data[:site]) || !bytes.Equal(code[end:], r.data[end:]) ||
		binary.LittleEndian.Uint32(code[site:])&0xfff != binary.LittleEndian.Uint32(r.data[site:])&0xfff ||
		binary.LittleEndian.Uint32(code[site+4:])&0xfffff != binary.LittleEndian.Uint32(r.data[site+4:])&0xfffff {
		return r.fail(scalarSpanMismatch("scalar fixup changed bytes outside authorized immediate fields"))
	}
	target, conditional, status, ok := decodeScalarLongEnvelope(code, span.Envelope.Start, site, end)
	if !ok {
		return r.fail(status)
	}
	if target != int64(ticket.target) || conditional != span.Conditional {
		return r.fail(scalarSpanMismatch("scalar fixup actual target or kind differs"))
	}
	copy(r.data[site:end], code[site:end])
	span.Patched, span.Target = true, target
	return true
}

// Seal checks final coverage, every registered fixup and exact owned bytes.
// Rechecking an already sealed receipt is allowed and still detects later
// mutation, charging global work history. Success is only a receipt proof; it
// does not validate frame/header safety or publish a graph Verified report.
func (r *scalarBodyReceipt) Seal(code []byte) bool {
	if !r.owned() {
		return false
	}
	if r.failed {
		return false
	}
	if r.closed || r.pending.owner != nil {
		return r.fail(scalarSpanFailure(regalloccheck.InvalidGraph, "scalar receipt has an unfinished transaction"))
	}
	if !r.sameBytes(code) || !r.charge(len(r.spans), 0) {
		return false
	}
	for _, span := range r.spans {
		if !span.HasEnvelope {
			continue
		}
		if !span.Patched {
			return r.fail(scalarSpanFailure(regalloccheck.InvalidGraph, "unresolved scalar control envelope"))
		}
		if !r.charge(3, 0) {
			return false
		}
		target, conditional, status, ok := decodeScalarLongEnvelope(code, span.Envelope.Start, span.Envelope.Site, span.Envelope.End)
		if !ok {
			return r.fail(status)
		}
		if target != span.Target || conditional != span.Conditional || !r.boundary(int(target)) {
			if r.failed {
				return false
			}
			return r.fail(scalarSpanMismatch("final scalar control contract changed"))
		}
	}
	r.sealed = true
	return true
}

// Close discards ownership without authorizing reuse or refunding history.
func (r *scalarBodyReceipt) Close() {
	if !r.owned() {
		return
	}
	r.data, r.spans = nil, nil
	r.pending = scalarRewriteTicket{}
	r.closed = true
}
