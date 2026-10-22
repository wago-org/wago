//go:build wago_regalloccheck

package shared

import "github.com/wago-org/wago/internal/regalloccheck"

// ScalarGraphDiagnosticTarget is optional checked-only failure evidence. A
// target can make an incomplete machine recipe Inconclusive or report a known
// physical mismatch as Rejected; it must never supply Verified. The result is
// snapshotted before observer restoration clears function-local target state.
// It adds no physical lowering hook or ordinary-build target requirement.
type ScalarGraphDiagnosticTarget interface {
	ScalarGraphDiagnostic() (regalloccheck.Result, bool)
}

func scalarGraphCapturePanic(run func()) (failure any) {
	defer func() { failure = recover() }()
	run()
	return nil
}

func scalarGraphFinalResult(g *scalarGraphRecorder, diagnostic regalloccheck.Result, present bool) regalloccheck.Result {
	r := regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.UnsupportedOperation, Message: "incomplete scalar emission"}
	if g.failure != nil {
		r = *g.failure
	} else if g.complete && g.returned && len(g.pending) == 0 {
		r = g.model.Verify(g.limits)
	}
	if !present {
		return r
	}
	if diagnostic.Verdict != regalloccheck.Inconclusive && diagnostic.Verdict != regalloccheck.Rejected || diagnostic.Reason == regalloccheck.NoFailure || diagnostic.Reason > regalloccheck.ResourceLimit {
		diagnostic = regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.InvalidGraph, Block: -1, Operation: -1, Message: "invalid scalar target diagnostic"}
	}
	// Retain a concrete disproof from either source even when the other side
	// cannot finish its analysis. A target diagnostic can never create success.
	if r.Verdict == regalloccheck.Rejected {
		return r
	}
	if diagnostic.Verdict == regalloccheck.Rejected || r.Verdict == regalloccheck.Verified {
		return diagnostic
	}
	return r
}
