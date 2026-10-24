//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Exact root masks describe the original source calls. A straight-line integer
// leaf adds no safepoint, reference local, or call of its own, so its one source
// mask can be consumed without recording a native call. Filter the existing
// candidate table to retain its size, signature and optimization policy limits.
func restrictGCNumericInlineTargets(m *wasm.Module, targets *inlineTargetTable) {
	if targets.empty() {
		return
	}
	for i := range targets.data.targets {
		t := &targets.data.targets[i]
		local := t.globalIdx - targets.first
		if !gcNumericInlineLeaf(m, local) {
			targets.data.slots[local] |= inlineTargetInvalid
		}
	}
}

func gcNumericInlineLeaf(m *wasm.Module, local int) bool {
	ft, ok := m.LocalFuncType(local)
	if !ok || !sigFitsRegABI(ft) || !sigIsIntOnly(ft) {
		return false
	}
	for _, run := range m.Code[local].Locals.Runs {
		if !isIntValType(run.Type) {
			return false
		}
	}
	r := wasm.ReaderFrom(m.Code[local].BodyBytes)
	for r.BytesLeft() != 0 {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch {
		case op == 0x0b:
			return r.BytesLeft() == 0
		case op == 0x20 || op == 0x21 || op == 0x22:
			_, err = r.U32()
		case op == 0x41:
			_, err = r.I32()
		case op == 0x42:
			_, err = r.I64()
		case op == 0x01 || op == 0x1a || op == 0x1b, // nop/drop/untyped select
			op >= 0x45 && op <= 0x5a,               // i32/i64 comparisons
			op >= 0x67 && op <= 0x8a,               // i32/i64 arithmetic and bit operations
			op == 0xa7 || op == 0xac || op == 0xad, // integer width conversions
			op >= 0xc0 && op <= 0xc4:               // integer sign extension
		default:
			return false
		}
		if err != nil {
			return false
		}
	}
	return false
}
