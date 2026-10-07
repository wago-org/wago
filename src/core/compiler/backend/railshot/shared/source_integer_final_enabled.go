//go:build wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Private concrete issuer data cannot be constructed by an opaque transport
// client or by supplying a generic result/boolean. Bytes are an owned snapshot
// of exactly the image decoded by a complete original-source physical proof.
type sourceIntegerCertificate struct {
	ctx      *codegen.SourceContext
	module   *wasm.Module
	function int
	attempt  codegen.SourceFinalAttempt
	bytes    []byte
}

func (w *sourceIntegerCertificate) Close() {
	if w != nil {
		*w = sourceIntegerCertificate{}
	}
}

func sourceIntegerFinalProfile(ctx *codegen.SourceContext, m *wasm.Module, function int) bool {
	a, _, ok := codegen.ValidatedSourceContext(ctx, m)
	if !ok || function <= 0 || function >= len(m.Code) || len(m.Code) > 32 || len(m.Imports) != 0 || len(m.Exports) != 0 || m.Start != nil || len(m.Tables) != 0 || len(m.Elements) != 0 || len(m.Globals) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 {
		return false
	}
	// Original addressability, including incoming return_call targets, is an
	// independent source obligation. Synthetic host0 alone has a wrapper here.
	forbidden := wasm.ValidatedFuncHasTailCall | wasm.ValidatedFuncUsesReferenceTypes | wasm.ValidatedFuncUsesTypedFunctionReferences | wasm.ValidatedFuncUsesRefFunc | wasm.ValidatedFuncUsesGC | wasm.ValidatedFuncUsesExceptionHandling
	return a.Flags()&forbidden == 0
}

// Run at every native attempt entry, before recipe admission can fail. Retiring
// a candidate never replenishes the worker or final-matcher pools.
func PrepareSourceIntegerFinalAttempt(s *ScalarState, m *wasm.Module, function int) {
	if s == nil {
		return
	}
	codegen.RetireSourceFinalWitness(s.sourceContext, m, function)
	s.sourceFinalAttempt = codegen.SourceFinalAttempt{}
	s.sourceFinalReason = regalloccheck.NoFailure
	if sourceIntegerFinalProfile(s.sourceContext, m, function) {
		s.sourceFinalAttempt = codegen.BeginSourceFinalAttempt(s.sourceContext, m, function, &s.sourceWork, &s.sourceStorage)
		s.sourceFinalReason = codegen.SourceFinalAttemptFailure(s.sourceFinalAttempt)
	}
}

// KMP visits the entire image independently of directory metadata. Its caller
// prepays the conservative 4*(image+pattern)+1 comparison/initialization upper bound.
// Count saturates at two: any duplicate (including overlap) is inconclusive.
func sourceIntegerUniqueImage(image, pattern []byte, prefix []int) (start, count int) {
	if len(pattern) == 0 || len(prefix) < len(pattern) {
		return -1, 0
	}
	prefix[0] = 0
	for i, j := 1, 0; i < len(pattern); i++ {
		for j > 0 && pattern[i] != pattern[j] {
			j = prefix[j-1]
		}
		if pattern[i] == pattern[j] {
			j++
		}
		prefix[i] = j
	}
	start = -1
	for i, j := 0, 0; i < len(image); i++ {
		for j > 0 && image[i] != pattern[j] {
			j = prefix[j-1]
		}
		if image[i] == pattern[j] {
			j++
		}
		if j == len(pattern) {
			start = i - len(pattern) + 1
			count++
			if count == 2 {
				return start, count
			}
			j = prefix[j-1]
		}
	}
	return
}

// Promotion has a private concrete proof owner and its consumed attempt. The
// opaque transport and a generic caller-supplied Result grant no new authority.
func recordSourceIntegerFinalResult(ctx *codegen.SourceContext, index int, w *sourceIntegerCertificate, token codegen.SourceFinalAttempt, r regalloccheck.Result) {
	if r.Verdict == regalloccheck.Verified && (w == nil || w.ctx != ctx || w.function != index || w.attempt != token || !codegen.SourceFinalAttemptConsumed(token, ctx, w.module, index)) {
		return
	}
	old, ok := codegen.SourceResultForFinalPass(ctx, index)
	if !ok {
		return
	}
	if old.Verdict == regalloccheck.Rejected || old.Reason == regalloccheck.ResourceLimit || old.Verdict == regalloccheck.Verified && r.Verdict != regalloccheck.Verified {
		return
	}
	if r.Verdict == regalloccheck.Inconclusive && old.Verdict == regalloccheck.Inconclusive && old.Message != "" {
		r.Message = old.Message + "; " + r.Message
	}
	codegen.RecordSourceResult(ctx, index, r)
}

// VerifySourceIntegerFinalModule consumes pending proofs after all workers and
// native layout/relocation transforms have finished. It certifies function-
// specific preservation and raw private lookup mapping at compiler return.
// Surrounding functions, callers, wrappers and future runtime sealing remain
// unverified. No entry metadata chooses the matching window or source identity.
func VerifySourceIntegerFinalModule(ctx *codegen.SourceContext, m *wasm.Module, image []byte, entry, internal []int, admit bool) {
	var certificates [32]*sourceIntegerCertificate
	var tokens [32]codegen.SourceFinalAttempt
	var results [32]regalloccheck.Result
	var starts, ends [32]int
	defer func() {
		for _, w := range certificates {
			w.Close()
		}
	}()
	_, _, valid := codegen.ValidatedSourceContext(ctx, m)
	if !valid || len(m.Code) > 32 {
		return
	}
	candidates := 0
	for i := range m.Code {
		token, opaque := codegen.TakeSourceFinalWitness(ctx, m, i)
		if opaque == nil {
			continue
		}
		w, ok := opaque.(*sourceIntegerCertificate)
		if !ok {
			opaque.Close()
			continue
		}
		certificates[i] = w
		tokens[i] = token
		candidates++
		results[i] = sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "final private function image not authenticated")
		if w.ctx != ctx || w.module != m || w.function != i || w.attempt != token || len(w.bytes) < 15 || len(w.bytes) > 2048 || !sourceIntegerFinalProfile(ctx, m, i) {
			results[i] = sourceLeafUnavailable(regalloccheck.InvalidGraph, "foreign or retired final function witness")
		}
	}
	if candidates == 0 {
		return
	}
	// Refusal also consumes and closes every pending snapshot.
	defer func() {
		for i, w := range certificates {
			if w != nil {
				recordSourceIntegerFinalResult(ctx, i, w, tokens[i], results[i])
			}
		}
	}()
	if !admit || len(image) > 4096 || len(entry) != len(m.Code) || len(internal) != len(m.Code) {
		return
	}
	if !codegen.ChargeFinalSourceWitnessPass(ctx, m, 1+2*len(m.Code), 2304) {
		for i, w := range certificates {
			if w != nil {
				results[i] = sourceLeafUnavailable(regalloccheck.ResourceLimit, "final image matcher construction credits exhausted")
			}
		}
		return
	}
	for i := range entry {
		if entry[i] < 0 || entry[i] >= len(image) || internal[i] < 0 || internal[i] >= len(image) {
			return
		}
	}
	var prefix [2048]int
	for i, w := range certificates {
		if w == nil || results[i].Reason == regalloccheck.InvalidGraph {
			continue
		}
		// KMP comparisons, prefix initialization and complete raw-directory
		// checks are covered before any traversal. Nothing refunds unused credit.
		if !codegen.ChargeFinalSourceWitnessPass(ctx, m, 4*(len(image)+len(w.bytes))+2*len(entry)+1, 0) {
			results[i] = sourceLeafUnavailable(regalloccheck.ResourceLimit, "final image matching credits exhausted")
			continue
		}
		start, count := sourceIntegerUniqueImage(image, w.bytes, prefix[:len(w.bytes)])
		if count != 1 || entry[i] != start || internal[i] != start {
			continue
		}
		end := start + len(w.bytes)
		aliases := false
		for j := range entry {
			if j != i && ((entry[j] >= start && entry[j] < end) || (internal[j] >= start && internal[j] < end)) {
				aliases = true
				break
			}
		}
		if aliases {
			continue
		}
		starts[i], ends[i] = start, end
		results[i] = regalloccheck.Result{Verdict: regalloccheck.Verified, Message: "original integer function bytes and private entry preserved at compiler return; other entries and runtime publication unverified"}
	}
	if !codegen.ChargeFinalSourceWitnessPass(ctx, m, len(m.Code)*len(m.Code), 0) {
		for i, w := range certificates {
			if w != nil && results[i].Verdict == regalloccheck.Verified {
				results[i] = sourceLeafUnavailable(regalloccheck.ResourceLimit, "final range comparison credits exhausted")
			}
		}
		return
	}
	for i := range certificates {
		if ends[i] == 0 {
			continue
		}
		for j := 0; j < i; j++ {
			if ends[j] != 0 && starts[i] < ends[j] && starts[j] < ends[i] {
				results[i] = sourceLeafUnavailable(regalloccheck.UnsupportedOperation, "overlapping independently certified ranges")
				results[j] = results[i]
			}
		}
	}
}
