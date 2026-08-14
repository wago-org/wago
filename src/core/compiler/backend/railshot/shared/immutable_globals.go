package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ImmutableIntGlobal is a compact module-summary entry for a defined immutable
// i32/i64 global whose initializer is one literal constant. Entries are sorted
// by Index and remain sparse so modules do not pay O(all globals) storage.
type ImmutableIntGlobal struct {
	Bits  int64
	Index uint32
	I32   bool
}

// ImmutableIntGlobals extracts the deliberately narrow, identity-independent
// constant class safe to fold in every instance. Imported globals, mutable
// globals, references, floats, and extended constant expressions are excluded.
func ImmutableIntGlobals(m *wasm.Module) []ImmutableIntGlobal {
	imported := m.ImportedGlobalCount()
	var out []ImmutableIntGlobal
	for i := range m.Globals {
		g := &m.Globals[i]
		if g.Type.Mutable {
			continue
		}
		bits, i32, ok := singleIntConstant(g.Init)
		matchesType := i32 && wasm.EqualValType(g.Type.Type, wasm.I32) ||
			!i32 && wasm.EqualValType(g.Type.Type, wasm.I64)
		if !ok || !matchesType {
			continue
		}
		out = append(out, ImmutableIntGlobal{Bits: bits, Index: uint32(imported + i), I32: i32})
	}
	return out
}

func singleIntConstant(expr wasm.Expr) (bits int64, i32, ok bool) {
	if len(expr.BodyBytes) != 0 {
		r := wasm.NewReader(expr.BodyBytes)
		op, err := r.Byte()
		if err != nil {
			return 0, false, false
		}
		switch op {
		case 0x41:
			v, err := r.I32()
			if err != nil {
				return 0, false, false
			}
			end, err := r.Byte()
			return int64(v), true, err == nil && end == 0x0b && r.BytesLeft() == 0
		case 0x42:
			v, err := r.I64()
			if err != nil {
				return 0, false, false
			}
			end, err := r.Byte()
			return v, false, err == nil && end == 0x0b && r.BytesLeft() == 0
		default:
			return 0, false, false
		}
	}
	if len(expr.Instrs) != 1 {
		return 0, false, false
	}
	in := expr.Instrs[0]
	switch in.Kind {
	case wasm.InstrI32Const:
		return int64(in.I32), true, true
	case wasm.InstrI64Const:
		return in.I64, false, true
	default:
		return 0, false, false
	}
}

// FindImmutableIntGlobal looks up a sparse sorted summary without a map.
func FindImmutableIntGlobal(constants []ImmutableIntGlobal, index uint32) (ImmutableIntGlobal, bool) {
	lo, hi := 0, len(constants)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if constants[mid].Index < index {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(constants) && constants[lo].Index == index {
		return constants[lo], true
	}
	return ImmutableIntGlobal{}, false
}
