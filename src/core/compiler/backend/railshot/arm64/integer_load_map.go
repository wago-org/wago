// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"bytes"
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var integerLoadMapEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_LOAD_MAP") != "1"

const integerLoadMapWorkLimit = 4096

// Internal operators for a select over the same two operands as its relation.
const (
	integerMapMinS byte = 0xc5 + iota
	integerMapMinU
	integerMapMaxS
	integerMapMaxU
)

func integerMapMinMax(a, b int, cmp floatMapNode) (byte, bool) {
	if cmp.op < 0x48 || cmp.op > 0x4f {
		return 0, false
	}
	swapped := cmp.left() == b && cmp.right() == a
	if !swapped && (cmp.left() != a || cmp.right() != b) {
		return 0, false
	}
	// Relational i32 opcodes encode unsigned in bit 0 and greater in bit 1.
	op := integerMapMinS + (cmp.op & 1)
	if (cmp.op&2 != 0) != swapped {
		op += integerMapMaxS - integerMapMinS
	}
	return op, true
}

// Integer recipes use the same owned candidate, source-order Q groups, widened
// memory immediates, nontrapping eligibility guards and scalar tail as floats.
func (p *floatMapPlan) valueType() machineType {
	if p.integer {
		return mtI32
	}
	return mapFloatType(p.wide)
}
func (p *floatMapPlan) load(op byte) bool {
	if p.integer {
		return op == 0x28
	}
	return op == 0x2a || op == 0x2b
}
func (p *floatMapPlan) constant(op byte) bool {
	if p.integer {
		return op == 0x41
	}
	return op == 0x43 || op == 0x44
}
func (p *floatMapPlan) binary(op byte) bool {
	if p.integer {
		switch op {
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, integerMapMinS, integerMapMinU, integerMapMaxS, integerMapMaxU:
			return true
		}
		return false
	}
	return op >= 0x92 && op <= 0x95 || op >= 0xa0 && op <= 0xa3
}
func (p *floatMapPlan) spend() bool {
	if !p.integer && !p.normalizedInduction {
		return true
	}
	p.work++
	return p.work <= p.workLimit
}

func (f *fn) emitIntegerLoadMap(source *wasm.Reader) {
	if !integerLoadMapEnabled || !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	if len(f.intervalLast) != 0 && (!integerMapIntervalsEnabled || !f.intervalControl || f.intervalActive != 0) {
		return
	}
	// Bound failed screens and all admitted planning walks across this function.
	// The large candidate frame is reached only after finding a typed load.
	scan := *source
	for ops := 0; ops < 24 && f.integerLoadMapWork < integerLoadMapWorkLimit; ops++ {
		f.integerLoadMapWork++
		op, e := scan.Byte()
		if e != nil {
			return
		}
		if op == 0x28 {
			// A valid integer plan needs an i32.store after the first load.
			// Immediate bytes may also match; the full planner checks opcodes.
			left := floatMapMaxBytes - (scan.Offset() - source.Offset())
			if left > 0 {
				window, _ := scan.Bytes(min(left, scan.BytesLeft()))
				if bytes.IndexByte(window, 0x36) >= 0 {
					f.emitIntegerLoadMapCandidate(source)
				}
			}
			return
		}
		switch op {
		case 0x20, 0x21, 0x22:
			if _, e := scan.U32(); e != nil {
				return
			}
		case 0x41:
			if _, e := scan.I32(); e != nil {
				return
			}
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76:
			// These fixed-width operators have no immediates. Keeping the
			// tiny screen concrete avoids making its copied reader escape
			// through the generic classifier on every rejected loop.
		default:
			return
		}
	}
}

// Keep the scoped reset outside the screen's loop. A defer declared inside a
// Go loop heap-allocates its closure even when that branch immediately returns.
func (f *fn) emitIntegerLoadMapCandidate(source *wasm.Reader) {
	previous := f.integerLoadMapProbe
	f.integerLoadMapProbe = true
	defer func() { f.integerLoadMapProbe = previous }()
	f.emitFloatMapBody(source)
}

// Shift immediates are stored modulo 32 by the planner. NEON right shifts
// cannot encode zero, so keep the identity exact for a zero effective count.
func (f *fn) emitIntegerMapShift(node floatMapNode, out, input Reg) {
	count := uint8(node.bits)
	if count == 0 {
		if out != input {
			f.a.NeonMov16b(out, input)
		}
		return
	}
	switch node.op {
	case 0x74:
		f.a.NeonShlS(out, input, count)
	case 0x75:
		f.a.NeonSshrS(out, input, count)
	case 0x76:
		f.a.NeonUshrS(out, input, count)
	}
}
