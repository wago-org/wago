//go:build amd64

package amd64

import "os"

var streamingHintFunnelEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_HINT_FUNNEL") == "1"

// streamHintsReject is a negative-only syntactic filter. None of these bits
// propagate from callees, and admission rejects their opcodes even in dead code.
// Tail-call and EH flags are module-wide and must not be added here. Bulk memory
// and ordinary direct calls are supported. Missing hints never prove eligibility.
func streamHintsReject(hints *funcHintView) bool {
	return streamingHintFunnelEnabled && hints != nil &&
		(hints.flags.has(hintHasFloatConst|hintHasSIMD|hintMutatesTable) || hints.hasNonDirectCall())
}
