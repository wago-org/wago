//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// SourceAttempt is an opaque ownership token. Creating it does not construct
// source contracts or claim any physical-emission coverage.
type SourceAttempt struct {
	owner        *ScalarState
	module       *wasm.Module
	function     int
	ledger       *wasm.SourceLedger
	plan         *sourceRecipePlan
	finalAttempt codegen.SourceFinalAttempt
	finalReason  regalloccheck.FailureReason
}

// SetSourceContext borrows compile-scoped facts after worker cleanup is installed.
func SetSourceContext(s *ScalarState, ctx *codegen.SourceContext) {
	if s.sourcePartitionSet {
		SetSourceWorkerContext(s, ctx, s.sourcePartition[0], s.sourcePartition[1])
		return
	}
	SetSourceWorkerContext(s, ctx, 0, 1)
}

// PrepareSourceWorker records partition coordinates before launch, without
// borrowing facts or allocating pools. SetSourceContext still starts ownership
// after worker cleanup is installed. Keeping workers out of the runWorker
// closure also preserves TinyGo's ordinary pre-DCE capture layout.
func PrepareSourceWorker(s *ScalarState, worker, workers int) {
	if s.sourceBudgetSet || s.sourcePartitionSet {
		panic("regalloccheck: source worker partition initialized twice")
	}
	s.sourcePartition = [2]int{worker, workers}
	s.sourcePartitionSet = true
}

// SetSourceWorkerContext partitions fixed compilation-wide history credits once.
// Function reset and retry never replenish them. Scheduling can affect which
// functions receive coverage after exhaustion, never the meaning of Verified.
func SetSourceWorkerContext(s *ScalarState, ctx *codegen.SourceContext, worker, workers int) {
	if s.sourceAttempt != nil {
		panic("regalloccheck: source context changed during active attempt")
	}
	if s.sourceBudgetSet {
		panic("regalloccheck: source worker initialized twice")
	}
	s.sourceBudgetSet = true
	s.sourceContext = ctx
	s.sourceWork, s.sourceStorage = 0, 0
	if ctx != nil && workers > 0 && worker >= 0 && worker < workers {
		s.sourceWork = (1 << 20) / workers
		s.sourceStorage = (1 << 20) / workers
		if worker < (1<<20)%workers {
			s.sourceWork++
			s.sourceStorage++
		}
	}
}

// BeginSourceAttempt rejects reentrancy without replacing the enclosing owner.
// The caller must install cleanup before calling; a failed begin grants no token.
func BeginSourceAttempt(s *ScalarState, m *wasm.Module, function int) *SourceAttempt {
	if s == nil {
		return nil
	}
	if s.sourceAttempt != nil {
		panic("regalloccheck: nested source attempt")
	}
	if _, _, ok := codegen.ValidatedSourceContext(s.sourceContext, m); !ok || function < 0 || function >= len(m.Code) {
		return nil
	}
	t := &SourceAttempt{owner: s, module: m, function: function, finalAttempt: s.sourceFinalAttempt, finalReason: s.sourceFinalReason}
	s.sourceAttempt = t
	return t
}

// AttachSourceLedger transfers exclusive ownership only on success. The caller
// must relinquish the ledger after attachment and must never share it between
// attempts. Admission must build
// the ledger under reviewed aggregate budgets before attaching it. No automatic
// construction occurs until an independently mapped emission recipe consumes it.
func AttachSourceLedger(t *SourceAttempt, ledger *wasm.SourceLedger) bool {
	if t == nil || t.owner == nil || t.owner.sourceAttempt != t || t.ledger != nil {
		return false
	}
	_, features, ok := codegen.ValidatedSourceContext(t.owner.sourceContext, t.module)
	if !ok || !ledger.ValidFor(t.module, t.function, features) {
		return false
	}
	t.ledger = ledger
	return true
}

// EndSourceAttempt closes only the ledger owned by this successful begin.
// It is nil-safe and idempotent, including unwinds before backend fn allocation.
func EndSourceAttempt(t *SourceAttempt) {
	if t == nil || t.owner == nil {
		return
	}
	if t.owner.sourceAttempt != t {
		panic("regalloccheck: foreign source attempt owner")
	}
	CloseSourcePlan(SourcePlanToken{t.plan})
	t.ledger.Close()
	t.owner.sourceAttempt = nil
	*t = SourceAttempt{}
}

func finishSourceWorker(s *ScalarState) {
	EndSourceAttempt(s.sourceAttempt)
	s.sourceContext = nil
	s.sourceFinalAttempt = codegen.SourceFinalAttempt{}
	s.sourceFinalReason = regalloccheck.NoFailure
	s.sourceWork, s.sourceStorage = 0, 0
	s.sourceBudgetSet = false
	s.sourcePartition = [2]int{}
	s.sourcePartitionSet = false
}
