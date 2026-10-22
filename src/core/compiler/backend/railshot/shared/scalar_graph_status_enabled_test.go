//go:build wago_regalloccheck

package shared

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

// These lifecycle fixtures do not stand in for emitted target evidence. Their
// tiny graphs isolate failure selection and cleanup; RV adapter activation is a
// separate checkpoint with real-encoder transport and corruption controls.
type scalarDiagnosticTarget struct {
	diagnostic                 regalloccheck.Result
	present                    bool
	queryPanic, restorePanic   any
	queries, restores, reports int
	result                     regalloccheck.Result
}

func (*scalarDiagnosticTarget) ObserveScalarGraph(func(regalloccheck.Effect), func(uint32)) func() {
	return func() {}
}
func (*scalarDiagnosticTarget) ScalarGraphReturnLocation() regalloccheck.Location {
	return regalloccheck.Register(regalloccheck.GP, 5)
}
func (*scalarDiagnosticTarget) ScalarGraphLocalOffset(i int) int32 { return int32(8 * i) }
func (*scalarDiagnosticTarget) ScalarGraphBranchDestination(int) (int, bool, bool) {
	return 0, false, false
}
func (t *scalarDiagnosticTarget) ScalarGraphDiagnostic() (regalloccheck.Result, bool) {
	t.queries++
	if t.queryPanic != nil {
		panic(t.queryPanic)
	}
	return t.diagnostic, t.present
}
func (t *scalarDiagnosticTarget) ScalarGraphResult(_ regalloccheck.Graph, r regalloccheck.Result) {
	t.reports++
	t.result = r
}
func (t *scalarDiagnosticTarget) restore() {
	t.restores++
	t.diagnostic, t.present = regalloccheck.Result{}, false
	if t.restorePanic != nil {
		panic(t.restorePanic)
	}
}
func scalarDiagnosticState(target *scalarDiagnosticTarget) *ScalarState {
	s := new(ScalarState)
	s.target, s.regs = &scalarTestTarget{}, []uint8{5}
	s.graph = &scalarGraphRecorder{s: s, target: target, limits: regalloccheck.DefaultLimits(), model: regalloccheck.Graph{Blocks: []regalloccheck.Block{{}}}, complete: true, returned: true, restoreObservers: target.restore}
	return s
}
func scalarDiagnosticEnd(s *ScalarState, emissionPanic any) (failure any) {
	defer func() { failure = recover() }()
	func() {
		defer checkGraphEnd(s)
		if emissionPanic != nil {
			panic(emissionPanic)
		}
	}()
	return nil
}

func TestScalarGraphDiagnosticSurvivesRestorationAndCannotVerify(t *testing.T) {
	for _, tc := range []struct {
		name       string
		diagnostic regalloccheck.Result
		present    bool
		verdict    regalloccheck.Verdict
		reason     regalloccheck.FailureReason
	}{
		{"unsupported", regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.UnsupportedOperation, Message: "unknown machine span"}, true, regalloccheck.Inconclusive, regalloccheck.UnsupportedOperation},
		{"rejected", regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch, Message: "wrong physical frame slot"}, true, regalloccheck.Rejected, regalloccheck.ProvenanceMismatch},
		{"cannot-verify", regalloccheck.Result{Verdict: regalloccheck.Verified}, true, regalloccheck.Inconclusive, regalloccheck.InvalidGraph},
		{"invalid-verdict", regalloccheck.Result{Verdict: 99, Reason: regalloccheck.UnsupportedOperation}, true, regalloccheck.Inconclusive, regalloccheck.InvalidGraph},
		{"invalid-reason", regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: 99}, true, regalloccheck.Inconclusive, regalloccheck.InvalidGraph},
		{"no-diagnostic", regalloccheck.Result{}, false, regalloccheck.Verified, regalloccheck.NoFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := &scalarDiagnosticTarget{diagnostic: tc.diagnostic, present: tc.present}
			s := scalarDiagnosticState(target)
			failure := scalarDiagnosticEnd(s, nil)
			if (failure != nil) != (tc.verdict == regalloccheck.Rejected) || failure != nil && !strings.Contains(fmt.Sprint(failure), "shared scalar graph") {
				t.Fatal("wrong final panic", failure)
			}
			if target.result.Verdict != tc.verdict || target.result.Reason != tc.reason || target.queries != 1 || target.restores != 1 || target.reports != 1 || target.present || s.graph != nil || s.target != nil || s.regs != nil {
				t.Fatal("diagnostic lost across restoration", target, s.graph)
			}
		})
	}
}

func TestScalarGraphDiagnosticRejectionPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name          string
		graph, target regalloccheck.Result
		modelReject   bool
		message       string
	}{
		{"graph-over-target-limit", regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch, Message: "graph mismatch"}, regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.ResourceLimit}, false, "graph mismatch"},
		{"target-over-graph-limit", regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.ResourceLimit}, regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch, Message: "target mismatch"}, false, "target mismatch"},
		{"actual-disproof-over-target-unsupported", regalloccheck.Result{}, regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.UnsupportedOperation}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := &scalarDiagnosticTarget{diagnostic: tc.target, present: true}
			s := scalarDiagnosticState(target)
			if tc.modelReject {
				s.graph.model.Widths = []uint8{8}
				s.graph.model.Blocks[0].Operations = []regalloccheck.Operation{{Kind: regalloccheck.Use, Location: regalloccheck.Register(regalloccheck.GP, 5), Value: 1}}
			} else {
				s.graph.failure = &tc.graph
			}
			if failure := scalarDiagnosticEnd(s, nil); failure == nil || target.result.Verdict != regalloccheck.Rejected || tc.message != "" && target.result.Message != tc.message {
				t.Fatal("concrete rejection downgraded", failure, target.result)
			}
		})
	}
}

func TestScalarGraphDiagnosticPanicCleanupAndReuse(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		emission, query, restore any
		want                     any
		queries                  int
	}{
		{"query", nil, "query panic", nil, "query panic", 1},
		{"restore", nil, nil, "restore panic", "restore panic", 1},
		{"query-over-restore", nil, "query panic", "restore panic", "query panic", 1},
		{"emission-over-both", "emission panic", "query panic", "restore panic", "emission panic", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := &scalarDiagnosticTarget{queryPanic: tc.query, restorePanic: tc.restore}
			s := scalarDiagnosticState(target)
			if failure := scalarDiagnosticEnd(s, tc.emission); failure != tc.want || target.queries != tc.queries || target.restores != 1 || target.reports != 0 || s.graph != nil || s.target != nil || s.regs != nil {
				t.Fatal("panic replaced or cleanup omitted", failure, target)
			}
			target.queryPanic, target.restorePanic = nil, nil
			next := scalarDiagnosticState(target)
			s.graph, s.target, s.regs = next.graph, next.target, next.regs
			s.graph.s = s
			if failure := scalarDiagnosticEnd(s, nil); failure != nil || target.result.Verdict != regalloccheck.Verified || target.restores != 2 || target.reports != 1 {
				t.Fatal("reused checked state retained failure", failure, target)
			}
		})
	}
}

// Embedding only the existing four-method interface deliberately does not
// expose ScalarGraphDiagnostic, preserving the original adapter contract.
type scalarLegacyDiagnosticTarget struct {
	ScalarGraphTarget
	result  regalloccheck.Result
	reports int
}

func (t *scalarLegacyDiagnosticTarget) ScalarGraphResult(_ regalloccheck.Graph, r regalloccheck.Result) {
	t.result, t.reports = r, t.reports+1
}

func TestScalarGraphDiagnosticOptionalForExistingTargets(t *testing.T) {
	target := &scalarDiagnosticTarget{present: true, diagnostic: regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch}}
	s := scalarDiagnosticState(target)
	legacy := &scalarLegacyDiagnosticTarget{ScalarGraphTarget: target}
	s.graph.target = legacy
	if failure := scalarDiagnosticEnd(s, nil); failure != nil || legacy.result.Verdict != regalloccheck.Verified || legacy.reports != 1 || target.queries != 0 || target.restores != 1 {
		t.Fatal("existing adapter contract changed", failure, legacy, target)
	}
}
