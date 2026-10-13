package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Population count is invariant under a rotation. Admit a bare counter or at
// most four constant rotations before popcnt; no arithmetic, effects or calls.
func inspectIntegerPopcountTerm(r *wasm.Reader) (integerAffineTerm, bool) {
	var out integerAffineTerm
	op, err := r.Byte()
	if err != nil || op != 0x20 {
		return out, false
	}
	counter, err := r.U32()
	if err != nil {
		return out, false
	}
	width := 0
	for rotations := 0; rotations <= 4; rotations++ {
		op, err = r.Byte()
		if err != nil {
			return out, false
		}
		if op == 0x69 || op == 0x7b {
			bits := 4
			add := byte(0x6a)
			if op == 0x7b {
				bits, add = 8, 0x7c
			}
			next, ok := r.Peek()
			if !ok || next != add || width != 0 && width != bits {
				return out, false
			}
			return integerAffineTerm{variable: counter, hasVariable: true, width: bits, populationCount: true}, true
		}
		if rotations == 4 {
			return out, false
		}
		switch op {
		case 0x41:
			if width != 0 && width != 4 {
				return out, false
			}
			width = 4
			if _, err = r.I32(); err != nil {
				return out, false
			}
		case 0x42:
			if width != 0 && width != 8 {
				return out, false
			}
			width = 8
			if _, err = r.I64(); err != nil {
				return out, false
			}
		default:
			return out, false
		}
		op, err = r.Byte()
		if err != nil || width == 4 && op != 0x77 && op != 0x78 || width == 8 && op != 0x89 && op != 0x8a {
			return out, false
		}
	}
	return out, false
}
