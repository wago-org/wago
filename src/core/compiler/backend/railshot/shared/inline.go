// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

import (
	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// InstructionNeedsInlineBoundary reports whether an instruction consumes or
// creates a control label that must remain inside a synthetic callee frame when
// its body is spliced into a caller. The opcode covers byte-backed scanners;
// kind covers decoded AST instructions and prefixed instructions.
func InstructionNeedsInlineBoundary(op byte, kind wasm.InstrKind) bool {
	if kind == wasm.InstrInvalid {
		switch op {
		case 0x00, // unreachable
			0x02, // block
			0x03, // loop
			0x04, // if
			0x05, // else
			0x0c, // br
			0x0d, // br_if
			0x0e, // br_table
			0x0f: // return
			return true
		case 0x1f: // try_table
			return buildfeatures.ExceptionHandling != 0
		case 0xd5, 0xd6: // br_on_null, br_on_non_null
			return buildfeatures.TypedFunctionReferences != 0
		}
	}
	switch kind {
	case wasm.InstrUnreachable, wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf,
		wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn:
		return true
	case wasm.InstrTryTable:
		return buildfeatures.ExceptionHandling != 0
	case wasm.InstrBrOnNull, wasm.InstrBrOnNonNull:
		return buildfeatures.TypedFunctionReferences != 0
	case wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
		return buildfeatures.GC != 0
	default:
		return false
	}
}

// InstructionNeedsEHFrame reports whether an instruction requires the
// exception-handling frame plan that inline targets do not yet transfer.
func InstructionNeedsEHFrame(op byte, kind wasm.InstrKind) bool {
	if buildfeatures.ExceptionHandling == 0 {
		return false
	}
	if kind == wasm.InstrInvalid {
		switch op {
		case 0x08, 0x0a, 0x1f: // throw, throw_ref, try_table
			return true
		}
	}
	switch kind {
	case wasm.InstrThrow, wasm.InstrThrowRef, wasm.InstrTryTable:
		return true
	default:
		return false
	}
}
