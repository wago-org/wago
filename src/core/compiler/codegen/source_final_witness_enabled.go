//go:build wago_regalloccheck

package codegen

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// SourceFinalWitness is an opaque checked-only transport. Readiness is decided
// by the final consumer's private concrete proof issuer, never this interface.
// Close must be nonpanicking and must not reenter the owning context.
type SourceFinalWitness interface{ Close() }
type SourceFinalAttempt struct {
	ctx    *SourceContext
	index  int
	serial uint64
	reason regalloccheck.FailureReason
}
type sourceFinalSlot struct {
	witness  SourceFinalWitness
	serial   uint64
	consumed bool
}

// RetireSourceFinalWitness invalidates earlier attempt authority before later
// admission/budget checks can fail. Removal and close never refund history.
func RetireSourceFinalWitness(ctx *SourceContext, m *wasm.Module, index int) {
	if _, _, ok := ValidatedSourceContext(ctx, m); !ok {
		return
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if index < 0 || index >= len(ctx.finalWitnesses) {
		return
	}
	s := &ctx.finalWitnesses[index]
	if s.witness != nil {
		s.witness.Close()
	}
	*s = sourceFinalSlot{}
}

// BeginSourceFinalAttempt spends the current worker's historical credits before
// allocation. The context collection is shared, so its first allocation is
// charged once under the same lock, not once per worker.
func BeginSourceFinalAttempt(ctx *SourceContext, m *wasm.Module, index int, work, storage *int) SourceFinalAttempt {
	if _, _, ok := ValidatedSourceContext(ctx, m); !ok || work == nil || storage == nil {
		return SourceFinalAttempt{reason: regalloccheck.InvalidGraph}
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if index < 0 || index >= len(m.Code) {
		return SourceFinalAttempt{reason: regalloccheck.InvalidGraph}
	}
	if len(m.Code) > 32 || ctx.finalSerial == ^uint64(0) {
		return SourceFinalAttempt{reason: regalloccheck.ResourceLimit}
	}
	needWork, needStorage := 1, 0
	if ctx.finalWitnesses == nil {
		needWork += len(m.Code)
		needStorage = 2 * len(m.Code)
	}
	if *work < needWork || *storage < needStorage {
		return SourceFinalAttempt{reason: regalloccheck.ResourceLimit}
	}
	*work -= needWork
	*storage -= needStorage
	if ctx.finalWitnesses == nil {
		ctx.finalWitnesses = make([]sourceFinalSlot, len(m.Code))
	}
	s := &ctx.finalWitnesses[index]
	if s.serial != 0 || s.witness != nil || s.consumed {
		return SourceFinalAttempt{reason: regalloccheck.InvalidGraph}
	}
	ctx.finalSerial++
	s.serial = ctx.finalSerial
	return SourceFinalAttempt{ctx: ctx, index: index, serial: s.serial}
}
func SourceFinalAttemptFailure(t SourceFinalAttempt) regalloccheck.FailureReason {
	if t.serial != 0 {
		return regalloccheck.NoFailure
	}
	if t.reason != regalloccheck.NoFailure {
		return t.reason
	}
	return regalloccheck.InvalidGraph
}
func SourceFinalAttemptValid(t SourceFinalAttempt, ctx *SourceContext, m *wasm.Module, index int) bool {
	if _, _, ok := ValidatedSourceContext(ctx, m); !ok || t.ctx != ctx || t.index != index || t.serial == 0 {
		return false
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	return index >= 0 && index < len(ctx.finalWitnesses) && ctx.finalWitnesses[index].serial == t.serial && !ctx.finalWitnesses[index].consumed
}
func RetireSourceFinalAttempt(t SourceFinalAttempt, m *wasm.Module) {
	ctx := t.ctx
	if _, _, ok := ValidatedSourceContext(ctx, m); !ok || t.serial == 0 {
		return
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if t.index < 0 || t.index >= len(ctx.finalWitnesses) {
		return
	}
	s := &ctx.finalWitnesses[t.index]
	if s.serial != t.serial {
		return
	}
	if s.witness != nil {
		s.witness.Close()
	}
	*s = sourceFinalSlot{}
}
func StoreSourceFinalWitness(t SourceFinalAttempt, m *wasm.Module, w SourceFinalWitness) bool {
	ctx := t.ctx
	if w == nil {
		return false
	}
	if _, _, ok := ValidatedSourceContext(ctx, m); !ok || t.serial == 0 {
		return false
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if t.index < 0 || t.index >= len(ctx.finalWitnesses) {
		return false
	}
	s := &ctx.finalWitnesses[t.index]
	if s.serial != t.serial || s.consumed || s.witness != nil {
		return false
	}
	s.witness = w
	return true
}

// Take consumes once after workers join. The coordinator must close every taken
// witness on all exits; context Close handles records that were never taken.
func TakeSourceFinalWitness(ctx *SourceContext, m *wasm.Module, index int) (SourceFinalAttempt, SourceFinalWitness) {
	if _, _, ok := ValidatedSourceContext(ctx, m); !ok {
		return SourceFinalAttempt{}, nil
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if index < 0 || index >= len(ctx.finalWitnesses) {
		return SourceFinalAttempt{}, nil
	}
	s := &ctx.finalWitnesses[index]
	if s.consumed || s.witness == nil {
		return SourceFinalAttempt{}, nil
	}
	w := s.witness
	s.witness = nil
	s.consumed = true
	return SourceFinalAttempt{ctx: ctx, index: index, serial: s.serial}, w
}
func ChargeFinalSourceWitnessPass(ctx *SourceContext, m *wasm.Module, work, storage int) bool {
	if _, _, ok := ValidatedSourceContext(ctx, m); !ok {
		return false
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if work < 0 || storage < 0 || work > ctx.finalWork || storage > ctx.finalStorage {
		return false
	}
	ctx.finalWork -= work
	ctx.finalStorage -= storage
	return true
}
func SourceResultForFinalPass(ctx *SourceContext, index int) (regalloccheck.Result, bool) {
	if ctx == nil || ctx.reporter == nil {
		return regalloccheck.Result{}, false
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	if index < 0 || index >= len(ctx.reports) {
		return regalloccheck.Result{}, false
	}
	return ctx.reports[index], true
}
func SourceFinalAttemptConsumed(t SourceFinalAttempt, ctx *SourceContext, m *wasm.Module, index int) bool {
	if _, _, ok := ValidatedSourceContext(ctx, m); !ok || t.ctx != ctx || t.index != index || t.serial == 0 {
		return false
	}
	ctx.reportMu.Lock()
	defer ctx.reportMu.Unlock()
	return index >= 0 && index < len(ctx.finalWitnesses) && ctx.finalWitnesses[index].serial == t.serial && ctx.finalWitnesses[index].consumed
}
func closeSourceFinalWitnesses(ctx *SourceContext) {
	for i := range ctx.finalWitnesses {
		if w := ctx.finalWitnesses[i].witness; w != nil {
			w.Close()
		}
		ctx.finalWitnesses[i] = sourceFinalSlot{}
	}
	ctx.finalWitnesses = nil
}
