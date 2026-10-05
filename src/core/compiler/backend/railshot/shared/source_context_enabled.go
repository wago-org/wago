//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// SourceAttempt is an opaque ownership token. Creating it does not construct
// source contracts or claim any physical-emission coverage.
type SourceAttempt struct {
	owner    *ScalarState
	module   *wasm.Module
	function int
	ledger   *wasm.SourceLedger
}

// SetSourceContext borrows compile-scoped facts after worker cleanup is installed.
func SetSourceContext(s *ScalarState, ctx *codegen.SourceContext) {
	if s.sourceAttempt != nil {
		panic("regalloccheck: source context changed during active attempt")
	}
	s.sourceContext = ctx
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
	t := &SourceAttempt{owner: s, module: m, function: function}
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
	t.ledger.Close()
	t.owner.sourceAttempt = nil
	*t = SourceAttempt{}
}

func finishSourceWorker(s *ScalarState) {
	EndSourceAttempt(s.sourceAttempt)
	s.sourceContext = nil
}
