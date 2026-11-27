// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func evalI31ConstExprBytes(b []byte, want wasm.ValType, ctx *constExprCompileContext) (constExprResult, bool, error) {
	var m *wasm.Module
	if ctx != nil {
		m = ctx.module
	}
	r := wasm.NewReader(b)
	op, err := r.Byte()
	if err != nil {
		return constExprResult{}, false, err
	}
	got := constExprResult{GlobalIndex: -1, FuncIndex: -1}
	switch op {
	case 0x41:
		v, err := r.I32()
		if err != nil {
			return constExprResult{}, true, err
		}
		got.bits = uint64(uint32(v)<<1 | 1)
	case 0x23:
		index, err := r.U32()
		if err != nil {
			return constExprResult{}, true, err
		}
		if m == nil {
			return constExprResult{}, true, fmt.Errorf("unsupported i31 const expression global.get %d", index)
		}
		gt, ok := m.GlobalTypeByIndex(index)
		if !ok || gt.Mutable || !wasm.EqualValType(gt.Type, wasm.I32) {
			return constExprResult{}, true, fmt.Errorf("unsupported i31 const expression global.get %d", index)
		}
		got.Expr = append([]byte(nil), b...)
	default:
		return constExprResult{}, false, nil
	}
	prefix, err := r.Byte()
	if err != nil {
		return constExprResult{}, true, err
	}
	if prefix != 0xfb {
		return constExprResult{}, false, nil
	}
	sub, err := r.U32()
	if err != nil {
		return constExprResult{}, true, err
	}
	if sub != 28 {
		return constExprResult{}, false, nil
	}
	end, err := r.Byte()
	if err != nil {
		return constExprResult{}, true, fmt.Errorf("i31 const expression missing end: %w", err)
	}
	if end != 0x0b || r.BytesLeft() != 0 {
		// The ref.i31 may be an operand of a longer validated GC constant
		// expression (for example an extern/any conversion round trip). Let the
		// general collector-backed evaluator preserve that complete program.
		return constExprResult{}, false, nil
	}
	got.vtype = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false))
	if !constExprTypeMatches(got.vtype, want, ctx) {
		return constExprResult{}, true, fmt.Errorf("const expression type %s, want %s", got.vtype.String(), want.String())
	}
	return got, true, nil
}

// evalCompiledI31ConstExpr evaluates `<i32 extended constant> ref.i31 end`.
// The i32 operand is any program the scalar evaluator accepts (constants,
// immutable global.get, add/sub/mul), so compile-time admission and
// instantiation agree on every valid i31 initializer. With nil globals it
// only validates the shape.
func evalCompiledI31ConstExpr(b []byte, globals []*Global, defs []GlobalDef, scope constExprGlobalScope) (uint64, error) {
	r := wasm.NewReader(b)
	last := -1
	var lastOp byte
	var lastIndex uint32
	for {
		start := len(b) - r.BytesLeft()
		op, err := r.Byte()
		if err != nil {
			return 0, fmt.Errorf("i31 initializer has invalid end")
		}
		if op == 0x0b {
			if r.BytesLeft() != 0 {
				return 0, fmt.Errorf("i31 initializer has invalid end")
			}
			break
		}
		index, _, err := readConstImmediate(r, op)
		if err != nil {
			return 0, fmt.Errorf("i31 initializer: %w", err)
		}
		last, lastOp, lastIndex = start, op, index
	}
	if last < 0 || lastOp != 0xfb {
		return 0, fmt.Errorf("i31 initializer missing ref.i31")
	}
	if lastIndex != 28 {
		return 0, fmt.Errorf("i31 initializer has unsupported 0xfb opcode %d", lastIndex)
	}
	operand := append(append([]byte(nil), b[:last]...), 0x0b)
	resolve := func(index uint32) (uint64, wasm.ValType, bool, bool) {
		i := int(index)
		if i < 0 || i >= scope.limit || i >= len(defs) {
			return 0, wasm.ValType{}, false, false
		}
		typ, ok := wasmScalarValType(defs[i].Type)
		if !ok {
			return 0, wasm.ValType{}, defs[i].Mutable, false
		}
		if globals == nil {
			return 0, typ, defs[i].Mutable, true
		}
		if i >= len(globals) || globals[i] == nil {
			return 0, wasm.ValType{}, false, false
		}
		return readGlobalObject(globals[i], defs[i].Type), typ, defs[i].Mutable, true
	}
	bits, err := evalCompiledIntegerOperand(operand, wasm.I32, resolve)
	if err != nil {
		return 0, fmt.Errorf("i31 initializer operand: %w", err)
	}
	if globals == nil {
		return 0, nil
	}
	return uint64(uint32(bits)<<1 | 1), nil
}

func lowerI31ElementInitializer(body []byte, want wasm.ValType, ctx *constExprCompileContext, i int, out *RefInit) error {
	if len(body) != 0 && body[0] == 0xd0 {
		result, err := evalConstExprBytesWithContext(body, want, ctx)
		if err == nil && isNullReferenceConstExprResult(result) {
			*out = RefInit{Null: true}
			return nil
		}
	}
	result, matched, err := evalI31ConstExprBytes(body, want, ctx)
	if err != nil {
		return fmt.Errorf("expression %d is not an exact i31 initializer: %v", i, err)
	}
	if !matched {
		result, err = evalConstExprBytesWithContext(body, want, ctx)
		if err != nil || len(result.Expr) == 0 {
			return fmt.Errorf("expression %d is not an exact i31 initializer: %v", i, err)
		}
		*out = RefInit{Expr: append([]byte(nil), result.Expr...)}
		return nil
	}
	if len(result.Expr) != 0 {
		r := wasm.NewReader(result.Expr)
		op, opErr := r.Byte()
		globalIndex, indexErr := r.U32()
		if opErr != nil || op != 0x23 || indexErr != nil {
			return fmt.Errorf("expression %d has malformed i31 global initializer", i)
		}
		*out = RefInit{GlobalIndex: globalIndex, HasGlobal: true, I31Wrap: true}
		return nil
	}
	if result.bits == 0 || result.bits>>32 != 0 || uint32(result.bits)&1 == 0 {
		return fmt.Errorf("expression %d has invalid i31 immediate", i)
	}
	*out = RefInit{FuncIndex: uint32(result.bits)}
	return nil
}

func (c *Compiled) addI31TableInitializer(tableIndex int, initBody []byte, want wasm.ValType, ctx *constExprCompileContext) bool {
	if _, matched, err := evalI31ConstExprBytes(initBody, want, ctx); !matched || err != nil {
		return false
	}
	def := c.tableDef(tableIndex)
	values := make([]RefInit, def.Size)
	for j := range values {
		values[j] = RefInit{Expr: append([]byte(nil), initBody...)}
	}
	c.Elems = append(c.Elems, ElemInit{TableIndex: uint32(tableIndex), RefType: def.Type, ValueTypeIndex: def.ValueTypeIndex, HasValueType: def.HasValueType, Mode: ElemModeActive, Values: values})
	return true
}
