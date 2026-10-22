//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/regalloccheck"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func scalarReceiptAppend(t *testing.T, receipt *scalarBodyReceipt, prior []byte, emit func(*enc.Asm) *scalarReceiptEnvelope) ([]byte, int) {
	t.Helper()
	var envelope *scalarReceiptEnvelope
	code, events := scalarWordCapture(t, len(prior), func(a *enc.Asm) { envelope = emit(a) })
	copy(code, prior)
	id, ok := receipt.Append(code, len(prior), len(code), events, envelope)
	if !ok {
		t.Fatal("register operation", receipt.failure)
	}
	return code, id
}

func scalarReceiptFixture(t *testing.T, limits scalarReceiptLimits) (*scalarBodyReceipt, []byte, int) {
	t.Helper()
	r := newScalarBodyReceipt(limits)
	code, _ := scalarReceiptAppend(t, r, nil, func(a *enc.Asm) *scalarReceiptEnvelope {
		a.ZeroExtend32(enc.X5, enc.X6)
		return nil
	})
	code, branch := scalarReceiptAppend(t, r, code, func(a *enc.Asm) *scalarReceiptEnvelope {
		start := a.Len()
		site := a.BranchLong(enc.NE, enc.X5, enc.Zero)
		return &scalarReceiptEnvelope{Start: start, Site: site, End: a.Len()}
	})
	code, _ = scalarReceiptAppend(t, r, code, func(a *enc.Asm) *scalarReceiptEnvelope {
		a.Ld(enc.X31, enc.SP, 8)
		a.Sd(enc.X30, enc.X31, 0)
		a.Addi(enc.X10, enc.X30, 0)
		return nil
	})
	return r, code, branch
}

func scalarReceiptPatch(t *testing.T, r *scalarBodyReceipt, code []byte, id, target int) {
	t.Helper()
	ticket, ok := r.BeginRewrite(code, id, target)
	if !ok {
		t.Fatal("begin fixup", r.failure)
	}
	a := enc.Asm{B: code}
	if err := a.PatchLong(r.spans[id].Envelope.Site, int64(target)); err != nil {
		t.Fatal(err)
	}
	if !r.CommitRewrite(a.B, ticket) {
		t.Fatal("commit fixup", r.failure)
	}
}

func TestScalarBodyReceiptOwnedBytesAndFinalMutation(t *testing.T) {
	r, code, branch := scalarReceiptFixture(t, scalarReceiptLimits{})
	scalarReceiptPatch(t, r, code, branch, len(code))
	if !r.Seal(code) || !r.Seal(slices.Clone(code)) {
		t.Fatal(r.failure)
	}
	if r.summary.Words != len(code)/4 || r.summary.Loads != 1 || r.summary.Stores != 1 || r.summary.Controls != 2 {
		t.Fatal(r.summary)
	}
	owned := slices.Clone(r.data)
	// Same effect trace, different branch input register after capture. Even a
	// previously valid final-byte check must not authorize this later mutation.
	code[10] ^= 1
	if !bytes.Equal(r.data, owned) {
		t.Fatal("receipt aliases mutable code")
	}
	if r.Seal(code) || r.failure.Verdict != regalloccheck.Rejected {
		t.Fatal("later mutation escaped", r.failure)
	}
	if !bytes.Equal(r.data, owned) {
		t.Fatal("failure replaced owned evidence")
	}
}

func TestScalarBodyReceiptCoverageAndUnregisteredControl(t *testing.T) {
	code, events := scalarWordCapture(t, 0, func(a *enc.Asm) { a.ZeroExtend32(enc.X5, enc.X6) })
	for _, span := range [][2]int{{4, 8}, {0, 4}, {0, 7}, {-4, 8}} {
		r := newScalarBodyReceipt(scalarReceiptLimits{})
		if _, ok := r.Append(code, span[0], span[1], events, nil); ok || r.failure.Reason != regalloccheck.InvalidGraph {
			t.Fatal(span, r.failure)
		}
	}
	r := newScalarBodyReceipt(scalarReceiptLimits{})
	if _, ok := r.Append(code, 0, len(code), events, nil); !ok {
		t.Fatal(r.failure)
	}
	if _, ok := r.Append(code, 0, len(code), events, nil); ok {
		t.Fatal("overlapping scope accepted")
	}
	for _, emit := range []func(*enc.Asm){
		func(a *enc.Asm) { a.JumpLong(false) },
		func(a *enc.Asm) { a.Jal(enc.RA, 0) },
		func(a *enc.Asm) { a.Ret() },
	} {
		code, events := scalarWordCapture(t, 0, emit)
		r := newScalarBodyReceipt(scalarReceiptLimits{})
		if _, ok := r.Append(code, 0, len(code), events, nil); ok || r.failure.Reason != regalloccheck.UnsupportedOperation {
			t.Fatal("unregistered control", r.failure)
		}
	}
	// An extra control before an otherwise valid registered envelope is not
	// covered by that envelope, even though all raw callbacks match.
	var envelope scalarReceiptEnvelope
	code, events = scalarWordCapture(t, 0, func(a *enc.Asm) {
		a.Jal(enc.Zero, 0)
		envelope.Start = a.Len()
		envelope.Site = a.JumpLong(false)
		envelope.End = a.Len()
	})
	r = newScalarBodyReceipt(scalarReceiptLimits{})
	if _, ok := r.Append(code, 0, len(code), events, &envelope); ok || r.failure.Reason != regalloccheck.UnsupportedOperation {
		t.Fatal(r.failure)
	}
	// Empty outer operations register boundaries without inventing byte spans.
	r = newScalarBodyReceipt(scalarReceiptLimits{})
	if _, ok := r.Append(nil, 0, 0, nil, nil); !ok || !r.Seal(nil) {
		t.Fatal(r.failure)
	}
}

func TestScalarBodyReceiptLegalOperationTargets(t *testing.T) {
	for _, target := range []int{0, 8, 20, 32} {
		r, code, branch := scalarReceiptFixture(t, scalarReceiptLimits{})
		scalarReceiptPatch(t, r, code, branch, target)
		if !r.Seal(code) {
			t.Fatal(target, r.failure)
		}
	}
	for _, target := range []int{-4, 4, 12, 16, 24, 28, 36} {
		r, code, branch := scalarReceiptFixture(t, scalarReceiptLimits{})
		if _, ok := r.BeginRewrite(code, branch, target); ok || r.failure.Verdict != regalloccheck.Rejected {
			t.Fatal("interior/outside target accepted", target, r.failure)
		}
	}
	// A future scope beginning at the old body end keeps an already-authorized
	// target legal; it must not invalidate or widen interior entry points.
	r, code, branch := scalarReceiptFixture(t, scalarReceiptLimits{})
	scalarReceiptPatch(t, r, code, branch, len(code))
	code, _ = scalarReceiptAppend(t, r, code, func(a *enc.Asm) *scalarReceiptEnvelope { a.Nop(); return nil })
	if !r.Seal(code) {
		t.Fatal(r.failure)
	}
}

func TestScalarBodyReceiptFixupMutationBoundaries(t *testing.T) {
	for _, mutate := range []func([]byte, int){
		func(code []byte, site int) { code[0] ^= 0x80 },           // unrelated prior word
		func(code []byte, site int) { code[site-4] ^= 0x80 },      // conditional prefix/skip
		func(code []byte, site int) { code[site] ^= 0x80 },        // AUIPC destination
		func(code []byte, site int) { code[site+4] ^= 0x80 },      // JALR link
		func(code []byte, site int) { code[len(code)-1] ^= 0x10 }, // later operation
	} {
		r, code, branch := scalarReceiptFixture(t, scalarReceiptLimits{})
		ticket, ok := r.BeginRewrite(code, branch, len(code))
		if !ok {
			t.Fatal(r.failure)
		}
		site := r.spans[branch].Envelope.Site
		a := enc.Asm{B: code}
		if err := a.PatchLong(site, int64(len(code))); err != nil {
			t.Fatal(err)
		}
		before := slices.Clone(r.data)
		mutate(code, site)
		if r.CommitRewrite(code, ticket) || r.failure.Verdict != regalloccheck.Rejected {
			t.Fatal("illegal rewrite admitted", r.failure)
		}
		if !bytes.Equal(before, r.data) {
			t.Fatal("failed rewrite changed receipt")
		}
	}
	r, code, branch := scalarReceiptFixture(t, scalarReceiptLimits{})
	ticket, ok := r.BeginRewrite(code, branch, len(code))
	if !ok {
		t.Fatal(r.failure)
	}
	a := enc.Asm{B: code}
	if err := a.PatchLong(r.spans[branch].Envelope.Site, 0); err != nil {
		t.Fatal(err)
	}
	if r.CommitRewrite(code, ticket) || r.failure.Verdict != regalloccheck.Rejected {
		t.Fatal("different legal target accepted", r.failure)
	}
	// A trace-preserving edit before Begin is rejected by exact preimage check.
	r, code, branch = scalarReceiptFixture(t, scalarReceiptLimits{})
	code[10] ^= 1
	if _, ok := r.BeginRewrite(code, branch, len(code)); ok || r.failure.Verdict != regalloccheck.Rejected {
		t.Fatal(r.failure)
	}
}

func TestScalarBodyReceiptTransactionLifetimeAndCopies(t *testing.T) {
	for _, action := range []string{"copy-append", "copy-begin", "copy-commit", "copy-seal", "copy-close", "repeat", "unfinished", "wrong-owner", "closed"} {
		t.Run(action, func(t *testing.T) {
			r, code, branch := scalarReceiptFixture(t, scalarReceiptLimits{})
			copied := *r
			switch action {
			case "copy-append":
				if _, ok := copied.Append(code, len(code), len(code), nil, nil); ok {
					t.Fatal("copied append")
				}
			case "copy-begin":
				if _, ok := copied.BeginRewrite(code, branch, len(code)); ok {
					t.Fatal("copied rewrite")
				}
			case "copy-commit":
				ticket, ok := r.BeginRewrite(code, branch, len(code))
				if !ok {
					t.Fatal(r.failure)
				}
				copied = *r
				if copied.CommitRewrite(code, ticket) {
					t.Fatal("copied commit")
				}
				if r.pending != ticket {
					t.Fatal("copy cleared original transaction")
				}
				a := enc.Asm{B: code}
				if err := a.PatchLong(r.spans[branch].Envelope.Site, int64(len(code))); err != nil {
					t.Fatal(err)
				}
				if !r.CommitRewrite(code, ticket) || !r.Seal(code) {
					t.Fatal(r.failure)
				}
				return
			case "copy-seal":
				scalarReceiptPatch(t, r, code, branch, len(code))
				copied = *r
				if copied.Seal(code) {
					t.Fatal("copied seal")
				}
				if !copied.failed || copied.failure.Reason != regalloccheck.InvalidGraph || !r.Seal(code) {
					t.Fatal(copied.failure, r.failure)
				}
				return
			case "copy-close":
				copied.Close()
			case "repeat":
				scalarReceiptPatch(t, r, code, branch, len(code))
				if _, ok := r.BeginRewrite(code, branch, 0); ok {
					t.Fatal("repeated fixup")
				}
				return
			case "unfinished":
				if _, ok := r.BeginRewrite(code, branch, len(code)); !ok {
					t.Fatal(r.failure)
				}
				if r.Seal(code) {
					t.Fatal("unfinished fixup sealed")
				}
				return
			case "wrong-owner":
				other, otherCode, otherBranch := scalarReceiptFixture(t, scalarReceiptLimits{})
				ticket, ok := other.BeginRewrite(otherCode, otherBranch, len(otherCode))
				if !ok {
					t.Fatal(other.failure)
				}
				if r.CommitRewrite(code, ticket) {
					t.Fatal("foreign ticket")
				}
				return
			case "closed":
				storage, work := r.storage, r.work
				r.Close()
				if r.Seal(code) || r.storage != storage || r.work != work || r.data != nil || r.spans != nil {
					t.Fatal("close revived/refunded", r.failure)
				}
				return
			}
			if !copied.failed || copied.failure.Reason != regalloccheck.InvalidGraph || r.failed || r.closed {
				t.Fatal("copy poisoning was not local", copied.failure, r.failure)
			}
			scalarReceiptPatch(t, r, code, branch, len(code))
			if !r.Seal(code) {
				t.Fatal("original ownership damaged", r.failure)
			}
		})
	}
}

func TestScalarBodyReceiptBudgetsAndUnresolvedEdges(t *testing.T) {
	code, events := scalarWordCapture(t, 0, func(a *enc.Asm) { a.ZeroExtend32(enc.X5, enc.X6) })
	for _, limits := range []scalarReceiptLimits{{Bytes: 4}, {Events: 1}, {Storage: 1}, {Work: 1}} {
		r := newScalarBodyReceipt(limits)
		if _, ok := r.Append(code, 0, len(code), events, nil); ok || r.failure.Reason != regalloccheck.ResourceLimit {
			t.Fatal(limits, r.failure)
		}
		if r.Seal(nil) {
			t.Fatal("budget failure repaired")
		}
		if len(r.data) != 0 || len(r.spans) != 0 {
			t.Fatal("failed registration retained receipt content")
		}
	}
	r := newScalarBodyReceipt(scalarReceiptLimits{Spans: 1})
	if _, ok := r.Append(nil, 0, 0, nil, nil); !ok {
		t.Fatal(r.failure)
	}
	if _, ok := r.Append(nil, 0, 0, nil, nil); ok || r.failure.Reason != regalloccheck.ResourceLimit {
		t.Fatal(r.failure)
	}
	r = newScalarBodyReceipt(scalarReceiptLimits{Work: -1})
	if !r.failed || r.failure.Reason != regalloccheck.InvalidGraph {
		t.Fatal(r.failure)
	}
	r, code, _ = scalarReceiptFixture(t, scalarReceiptLimits{})
	if r.Seal(code) || r.failure.Reason != regalloccheck.InvalidGraph {
		t.Fatal("unresolved edge sealed", r.failure)
	}
	r, code, branch := scalarReceiptFixture(t, scalarReceiptLimits{})
	scalarReceiptPatch(t, r, code, branch, len(code))
	// Repeated checks consume one monotonically charged lifetime budget.
	r.limits.Work = r.work + 2*(1+len(code)/4+len(r.spans)+16)
	for attempt := 0; attempt < 10 && !r.failed; attempt++ {
		r.Seal(code)
	}
	if !r.failed || r.failure.Reason != regalloccheck.ResourceLimit {
		t.Fatal("rechecks bypassed global budget", r.failure)
	}
	// Each operation individually fits, but the shared event history does not.
	r = newScalarBodyReceipt(scalarReceiptLimits{Events: 3, Bytes: 8, Spans: 2})
	code, _ = scalarReceiptAppend(t, r, nil, func(a *enc.Asm) *scalarReceiptEnvelope { a.Addi(enc.X5, enc.X6, 0); return nil })
	more, moreEvents := scalarWordCapture(t, len(code), func(a *enc.Asm) { a.Addi(enc.X7, enc.X6, 0) })
	copy(more, code)
	if _, ok := r.Append(more, len(code), len(more), moreEvents, nil); ok || r.failure.Reason != regalloccheck.ResourceLimit || r.eventHistory != 2 {
		t.Fatal("aggregate event budget bypass", r.failure)
	}
	if cap(r.data) > r.limits.Bytes || cap(r.spans) > r.limits.Spans {
		t.Fatal("retained capacity exceeded caps")
	}
}

func TestScalarBodyReceiptCaptureIsNotSemanticOperandAuthentication(t *testing.T) {
	var envelope scalarReceiptEnvelope
	code, events := scalarWordCapture(t, 0, func(a *enc.Asm) {
		envelope.Start = a.Len()
		envelope.Site = a.BranchLong(enc.NE, enc.X5, enc.X6)
		envelope.End = a.Len()
	})
	// Before capture, a different branch register preserves the raw trace and
	// envelope structure. This primitive has no semantic operand contract and
	// must NOT claim to prove that register was the requested source operand.
	binary.LittleEndian.PutUint32(code, binary.LittleEndian.Uint32(code)^1<<15)
	r := newScalarBodyReceipt(scalarReceiptLimits{})
	id, ok := r.Append(code, 0, len(code), events, &envelope)
	if !ok {
		t.Fatal(r.failure)
	}
	scalarReceiptPatch(t, r, code, id, len(code))
	if !r.Seal(code) {
		t.Fatal(r.failure)
	}
	code[1] ^= 0x10 // a later trace-preserving condition change MUST fail
	if r.Seal(code) || r.failure.Verdict != regalloccheck.Rejected {
		t.Fatal(r.failure)
	}
}

func TestScalarBodyReceiptAllocationHistoryAcrossGrowth(t *testing.T) {
	emit := func(a *enc.Asm) *scalarReceiptEnvelope {
		for i := 0; i < 9; i++ {
			a.Nop()
		}
		return nil
	}
	elementBytes := int(unsafe.Sizeof(scalarReceiptSpan{}))
	r := newScalarBodyReceipt(scalarReceiptLimits{})
	var code []byte
	for i := 0; i < 9; i++ {
		oldStorage, oldData, oldSpans := r.storage, cap(r.data), cap(r.spans)
		code, _ = scalarReceiptAppend(t, r, code, emit)
		want := oldStorage
		if cap(r.data) != oldData {
			want += cap(r.data)
		}
		if cap(r.spans) != oldSpans {
			want += cap(r.spans) * elementBytes
		}
		if r.storage != want {
			t.Fatal("growth refunded prior backing", i, r.storage, want)
		}
	}
	if cap(r.data) <= 256 || cap(r.spans) <= 8 {
		t.Fatal("growth thresholds not exercised")
	}
	liveBacking := cap(r.data) + cap(r.spans)*elementBytes
	if r.storage <= liveBacking {
		t.Fatal("allocation history collapsed to live size", r.storage, liveBacking)
	}
	// The live final arrays would fit this cap, but their cumulative allocated
	// history would not. Reject before allocating the final span replacement.
	limited := newScalarBodyReceipt(scalarReceiptLimits{Storage: liveBacking})
	code = nil
	for i := 0; i < 8; i++ {
		code, _ = scalarReceiptAppend(t, limited, code, emit)
	}
	beforeData, beforeSpans, beforeStorage := slices.Clone(limited.data), slices.Clone(limited.spans), limited.storage
	more, events := scalarWordCapture(t, len(code), func(a *enc.Asm) { emit(a) })
	copy(more, code)
	if _, ok := limited.Append(more, len(code), len(more), events, nil); ok || limited.failure.Reason != regalloccheck.ResourceLimit {
		t.Fatal("history cap bypassed at growth", limited.failure)
	}
	if !bytes.Equal(limited.data, beforeData) || !slices.Equal(limited.spans, beforeSpans) || limited.storage != beforeStorage {
		t.Fatal("failed growth retained content or refunded history")
	}
}
