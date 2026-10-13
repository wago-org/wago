package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

type integerAffineTerm struct {
	coefficient, constant  uint64
	width                  int
	variable, global       uint32
	hasVariable, hasGlobal bool
	calls                  uint8
}

// inspectIntegerAffineTerm evaluates a bounded pure postfix expression as A*i+B
// modulo its integer width. All local reads must name the same counter. A
// product is affine only when at least one operand has zero coefficient.
func inspectIntegerAffineTerm(r *wasm.Reader, blocked bool) (integerAffineTerm, bool) {
	return inspectIntegerAffineTermForModule(r, blocked, nil, false)
}

func inspectIntegerAffineTermForModule(r *wasm.Reader, blocked bool, module *wasm.Module, allowCalls bool) (integerAffineTerm, bool) {
	start := r.Offset()
	first, ok := r.Peek()
	if !ok || (first != 0x20 && first != 0x41 && first != 0x42 && first != 0x23) {
		return integerAffineTerm{}, false
	}
	// Read the value at invocation time; initializers and imported bits are not
	// compiler constants. Only a bare integer getter is admitted. Mutable values require the
	// backend's independent whole-function value-pin proof.
	if first == 0x23 {
		if module == nil {
			return integerAffineTerm{}, false
		}
		_, _ = r.Byte()
		x, err := r.U32()
		if err != nil {
			return integerAffineTerm{}, false
		}
		gt, ok := module.GlobalTypeByIndex(x)
		if !ok {
			return integerAffineTerm{}, false
		}
		width := 0
		if wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I32) {
			width = 4
		} else if wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I64) {
			width = 8
		} else {
			return integerAffineTerm{}, false
		}
		next, ok := r.Peek()
		if !ok || !(blocked && next == 0x0b || !blocked && (next == 0x6a || next == 0x7c)) {
			return integerAffineTerm{}, false
		}
		return integerAffineTerm{global: x, hasGlobal: true, width: width}, true
	}
	// The common single-counter term avoids the expression stack entirely.
	if first == 0x20 {
		_, _ = r.Byte()
		x, err := r.U32()
		if err != nil {
			return integerAffineTerm{}, false
		}
		next, ok := r.Peek()
		if ok && (blocked && next == 0x0b || !blocked && (next == 0x6a || next == 0x7c)) {
			return integerAffineTerm{coefficient: 1, variable: x, hasVariable: true}, true
		}
		if r.JumpTo(start) != nil {
			return integerAffineTerm{}, false
		}
	}
	// floatSign is zero for an integer, otherwise a finite float whose sign
	// is proven: f32 nonnegative/nonpositive = 1/2, f64 = 3/4.
	type affineValue struct {
		coefficient, constant uint64
		floatSign             uint8
	}
	var stack [16]affineValue
	var out integerAffineTerm
	depth := 0
	setWidth := func(width int) bool {
		if out.width != 0 && out.width != width {
			return false
		}
		out.width = width
		return true
	}
	for n := 0; n < 32 && r.Offset()-start < 128; n++ {
		op, ok := r.Peek()
		if !ok {
			return out, false
		}
		// With one term left, the unblocked add consumes the earlier accumulator.
		if depth == 1 && (blocked && op == 0x0b || !blocked && (op == 0x6a || op == 0x7c)) {
			if stack[0].floatSign != 0 {
				return out, false
			}
			out.coefficient, out.constant = stack[0].coefficient, stack[0].constant
			return out, true
		}
		_, _ = r.Byte()
		switch op {
		case 0x20:
			x, err := r.U32()
			if err != nil || depth == len(stack) {
				return out, false
			}
			if out.hasVariable && out.variable != x {
				return out, false
			}
			out.variable, out.hasVariable = x, true
			stack[depth] = affineValue{coefficient: 1}
			depth++
		case 0x41, 0x42:
			if depth == len(stack) {
				return out, false
			}
			var value uint64
			if op == 0x41 {
				v, err := r.I32()
				if err != nil || !setWidth(4) {
					return out, false
				}
				value = uint64(uint32(v))
			} else {
				v, err := r.I64()
				if err != nil || !setWidth(8) {
					return out, false
				}
				value = uint64(v)
			}
			stack[depth] = affineValue{constant: value}
			depth++
		case 0x10:
			if !allowCalls || module == nil || depth == 0 || stack[depth-1].floatSign != 0 || out.calls == 4 {
				return out, false
			}
			index, err := r.U32()
			if err != nil {
				return out, false
			}
			helper, ok := inspectIntegerAffineHelper(module, index)
			if !ok || !setWidth(helper.width) {
				return out, false
			}
			a := &stack[depth-1]
			a.coefficient *= helper.coefficient
			a.constant = a.constant*helper.coefficient + helper.constant
			if helper.width == 4 {
				a.coefficient = uint64(uint32(a.coefficient))
				a.constant = uint64(uint32(a.constant))
			}
			out.calls++
		case 0xb3, 0xb5, 0xb8, 0xba: // f32/f64.convert_i32/i64_u
			width := 4
			if op == 0xb5 || op == 0xba {
				width = 8
			}
			if depth == 0 || stack[depth-1].floatSign != 0 || !setWidth(width) {
				return out, false
			}
			sign := uint8(1)
			if op == 0xb8 || op == 0xba {
				sign = 3
			}
			// Every unsigned integer converts to a finite nonnegative float,
			// including rounded UINT64_MAX. The affine bits are no longer useful.
			stack[depth-1] = affineValue{floatSign: sign}
		case 0x8c, 0x9a: // f32/f64.neg
			if depth == 0 {
				return out, false
			}
			sign := stack[depth-1].floatSign
			base := uint8(1)
			if op == 0x9a {
				base = 3
			}
			if sign != base && sign != base+1 {
				return out, false
			}
			stack[depth-1].floatSign = base + 1 - (sign - base)
		case 0xfc:
			sub, err := r.U32()
			if err != nil || depth == 0 {
				return out, false
			}
			width, sign := 4, uint8(2)
			switch sub {
			case 1: // i32.trunc_sat_f32_u
			case 3:
				sign = 4 // i32.trunc_sat_f64_u
			case 5:
				width = 8 // i64.trunc_sat_f32_u
			case 7:
				width, sign = 8, 4 // i64.trunc_sat_f64_u
			default:
				return out, false
			}
			if stack[depth-1].floatSign != sign || !setWidth(width) {
				return out, false
			}
			// Finite nonpositive inputs, including -0, saturate to unsigned zero.
			stack[depth-1] = affineValue{}
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e:
			width := 4
			if op >= 0x7c {
				width = 8
			}
			if depth < 2 || stack[depth-2].floatSign != 0 || stack[depth-1].floatSign != 0 || !setWidth(width) {
				return out, false
			}
			a, b := stack[depth-2], stack[depth-1]
			var v affineValue
			switch op {
			case 0x6a, 0x7c:
				v.coefficient = a.coefficient + b.coefficient
				v.constant = a.constant + b.constant
			case 0x6b, 0x7d:
				v.coefficient = a.coefficient - b.coefficient
				v.constant = a.constant - b.constant
			default:
				if a.coefficient != 0 && b.coefficient != 0 {
					return out, false
				}
				v.coefficient = a.coefficient*b.constant + b.coefficient*a.constant
				v.constant = a.constant * b.constant
			}
			if width == 4 {
				v.coefficient = uint64(uint32(v.coefficient))
				v.constant = uint64(uint32(v.constant))
			}
			depth--
			stack[depth-1] = v
		default:
			return out, false
		}
	}
	return out, false
}

// Only original, byte-backed, local integer leaves qualify. Imported functions,
// declared locals, indirect calls and any effects or control reject the proof.
func inspectIntegerAffineHelper(module *wasm.Module, index uint32) (integerAffineTerm, bool) {
	var zero integerAffineTerm
	imported := uint64(module.ImportedFuncCount())
	if uint64(index) < imported {
		return zero, false
	}
	local := uint64(index) - imported
	if local >= uint64(len(module.Code)) {
		return zero, false
	}
	ft, ok := module.LocalFuncType(int(local))
	if !ok || len(ft.Params) != 1 || len(ft.Results) != 1 || !wasm.EqualValType(ft.Params[0], ft.Results[0]) {
		return zero, false
	}
	width := 0
	if wasm.EqualValType(ft.Params[0], wasm.I32) {
		width = 4
	} else if wasm.EqualValType(ft.Params[0], wasm.I64) {
		width = 8
	} else {
		return zero, false
	}
	fn := &module.Code[local]
	if len(fn.Locals.Runs) != 0 || len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 128 {
		return zero, false
	}
	r := wasm.ReaderFrom(fn.BodyBytes)
	p, ok := inspectIntegerAffineTerm(&r, true)
	if !ok || p.hasVariable && p.variable != 0 || p.width != 0 && p.width != width || r.Offset() != len(fn.BodyBytes)-1 {
		return zero, false
	}
	p.width = width
	return p, true
}
