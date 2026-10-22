package wago

import "fmt"

// This private binding is only constructed for a registered native custom
// instruction. Its host fallback traps; it never interprets erased values as
// numeric cells or collector/reference tokens.
type nativeCustomInstructionBinding struct {
	fn  slotHostFunc
	sig FuncSig
}

func (b nativeCustomInstructionBinding) validate(sig FuncSig) error {
	if b.fn == nil || !funcSigEqual(b.sig, sig) {
		return fmt.Errorf("native custom instruction signature mismatch")
	}
	return nil
}
