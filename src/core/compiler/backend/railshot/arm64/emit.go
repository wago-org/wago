//go:build arm64

package arm64

import a64 "github.com/wago-org/wago/src/core/encoder/arm64"

// The condense engine: materialize a deferred-action valent block into machine
// code, with target hints (compute the result straight into a destination
// register and reuse operand registers in place). Ported from WARP's
// condenseValentBlock / emitDeferredAction / selectInstr operand folding.
//
// AArch64 is a load/store, three-operand, orthogonal RISC: no memory operands, no
// flag side-effects on ordinary ALU ops, and no fixed-register instructions. Every
// fold x86 did inside one instruction becomes an explicit sequence here — but the
// operand-stack / valent-block / condense architecture is unchanged; only the leaf
// lowering differs (see the arm64 CONTRACT §4).

// aluEnc identifies a binary integer ALU op for the applyALU dispatcher. On x86
// this carried the encoding bytes (rr/rm/digit) an instruction selected between;
// AArch64 has a distinct orthogonal instruction per op (ADD/SUB/AND/ORR/EOR in
// both widths), so the "encoding" reduces to the wasm op itself plus the
// commutativity flag the selector consults.
type aluEnc struct {
	op   wOp
	comm bool
}

var aluTable = [...]aluEnc{
	opAdd: {opAdd, true},
	opSub: {opSub, false},
	opAnd: {opAnd, true},
	opOr:  {opOr, true},
	opXor: {opXor, true},
}

// addFoldImm folds `dest = dest + v` (signed) into the 12-bit add/sub-immediate
// form: a non-negative v uses AddImm, a negative v uses SubImm of its magnitude.
// Returns false when |v| exceeds the 12-bit range.
func (f *fn) addFoldImm(dest Reg, v int64, w bool) bool {
	return f.addFoldImm3(dest, dest, v, w)
}

// memRefFoldable reports whether a deferred load may be folded directly as an
// ALU/CMP operand. AArch64 has no memory operands, so a deferred load can NEVER be
// folded — applyALU/applyMul/condenseCompare therefore always take their
// materialize→reg-reg path (see the arm64 CONTRACT §4a).
func memRefFoldable(st storage, w bool) bool { return false }

var _ = a64.X0 // ensure the a64 import is referenced even before the encoder methods land
