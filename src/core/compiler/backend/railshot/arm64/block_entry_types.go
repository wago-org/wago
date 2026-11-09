//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// The new block-prefix family retains scalar FP/vector initialization: removing
// it broadly regressed recursive floating code. Type filtering happens only
// when this prefix first encounters a definition. The mask is scan scratch,
// never retained in function hints, operand nodes, or local hotness scores.
func (s *byteBodyScanner) blockEntryLocalEligible(index uint32) bool {
	if s.m == nil {
		// Untyped raw-scanner tests collect source facts without target policy.
		return index < 64
	}
	if !s.entryBlockTypesReady {
		s.entryBlockTypesReady = true
		function := int(s.selfIdx) - s.m.ImportedFuncCount()
		sig, ok := s.m.FuncSignature(s.selfIdx)
		if !ok || function < 0 || function >= len(s.m.Code) {
			return false
		}
		local := len(sig.Params)
		for i, run := range s.m.Code[function].Locals.Runs {
			// Zero-count declarations must not permit an unbounded type walk.
			if i >= 64 || local >= 64 {
				break
			}
			end := min(64, local+int(run.Count))
			if run.Type == wasm.I32 || run.Type == wasm.I64 {
				for x := local; x < end; x++ {
					s.entryBlockGP |= uint64(1) << x
				}
			}
			local = end
		}
	}
	return index < 64 && s.entryBlockGP&(uint64(1)<<index) != 0
}
