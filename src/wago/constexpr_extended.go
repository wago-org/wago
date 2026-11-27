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

// evalScalarConstExprProgram evaluates the scalar portion of WebAssembly 3.0
// extended constant expressions. The same strict parser is used for compile-time
// folding, codec validation, and instantiation so malformed persisted programs
// fail closed.
func evalScalarConstExprProgram(b []byte, want wasm.ValType, resolve constExprGlobalResolver) (bits uint64, usesGlobal bool, err error) {
	if !wasm.EqualValType(want, wasm.I32) && !wasm.EqualValType(want, wasm.I64) {
		return 0, false, fmt.Errorf("extended const expression type %s is not scalar integer", want.String())
	}
	r := wasm.NewReader(b)
	// Validated expressions are normally shallow. Keep the common operand stack
	// inline; append provides a bounded spill path for deeper valid programs.
	var inline [8]scalarConstValue
	stack := inline[:0]
	push := func(v scalarConstValue) { stack = append(stack, v) }
	pop2 := func(typ wasm.ValType) (scalarConstValue, scalarConstValue, error) {
		if len(stack) < 2 {
			return scalarConstValue{}, scalarConstValue{}, fmt.Errorf("extended const expression stack underflow")
		}
		rhs := stack[len(stack)-1]
		lhs := stack[len(stack)-2]
		stack = stack[:len(stack)-2]
		if !wasm.EqualValType(lhs.typ, typ) || !wasm.EqualValType(rhs.typ, typ) {
			return scalarConstValue{}, scalarConstValue{}, fmt.Errorf("extended const expression operand type mismatch")
		}
		return lhs, rhs, nil
	}

	for r.HasNext() {
		op, readErr := r.Byte()
		if readErr != nil {
			return 0, usesGlobal, readErr
		}
		switch {
		case op == 0x0b:
			if r.BytesLeft() != 0 {
				return 0, usesGlobal, fmt.Errorf("extended const expression has trailing bytes")
			}
			if len(stack) != 1 || !wasm.EqualValType(stack[0].typ, want) {
				return 0, usesGlobal, fmt.Errorf("extended const expression result type mismatch")
			}
			return stack[0].bits, usesGlobal, nil
		case constExprLowOpcodesForImage&(1<<0x23) != 0 && op == 0x23 ||
			constExprNumericOpcodesForImage&(1<<(0x41-0x40)) != 0 && op == 0x41 ||
			constExprNumericOpcodesForImage&(1<<(0x42-0x40)) != 0 && op == 0x42:
			value, err := readScalarConstOperand(r, op, resolve)
			if err != nil {
				return 0, usesGlobal, err
			}
			usesGlobal = usesGlobal || op == 0x23
			push(value)
		case constExprArithmeticOpcodesForImage != 0 && constExprArithmeticOpcodesForImage&(uint64(1)<<(op-0x40)) != 0:
			typ := wasm.I32
			if op >= 0x7c {
				typ = wasm.I64
			}
			lhs, rhs, popErr := pop2(typ)
			if popErr != nil {
				return 0, usesGlobal, popErr
			}
			push(scalarConstValue{bits: evalConstIntegerArithmetic(op, lhs.bits, rhs.bits), typ: typ})
		default:
			return 0, usesGlobal, fmt.Errorf("unsupported extended const expression opcode 0x%02x", op)
		}
	}
	return 0, usesGlobal, fmt.Errorf("extended const expression missing end")
}

// evalConstIntegerArithmetic takes a validated add, sub, or mul opcode.
// Truncating the result implements the i32 operations modulo 2^32.
func evalConstIntegerArithmetic(op byte, lhs, rhs uint64) uint64 {
	var bits uint64
	switch {
	case constExprNumericOpcodesForImage&(1<<(0x6a-0x40)) != 0 && op == 0x6a || constExprNumericOpcodesForImage&(1<<(0x7c-0x40)) != 0 && op == 0x7c:
		bits = lhs + rhs
	case constExprNumericOpcodesForImage&(1<<(0x6b-0x40)) != 0 && op == 0x6b || constExprNumericOpcodesForImage&(1<<(0x7d-0x40)) != 0 && op == 0x7d:
		bits = lhs - rhs
	case constExprNumericOpcodesForImage&(1<<(0x6c-0x40)) != 0 && op == 0x6c || constExprNumericOpcodesForImage&(1<<(0x7e-0x40)) != 0 && op == 0x7e:
		bits = lhs * rhs
	}
	if op < 0x7c {
		return uint64(uint32(bits))
	}
	return bits
}
