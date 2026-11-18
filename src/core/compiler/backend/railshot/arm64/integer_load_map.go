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

var integerMapAffineExitEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_MAP_AFFINE_EXIT") != "1"

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
		case 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, integerMapMinS, integerMapMinU, integerMapMaxS, integerMapMaxU:
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
				look := scan
				window, _ := scan.Bytes(min(left, scan.BytesLeft()))
				if bytes.IndexByte(window, 0x36) >= 0 && f.integerLoadMapAfterLoad(look) {
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
		case 0x1b, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76:
			// These fixed-width operators have no immediates. Keeping the
			// tiny screen concrete avoids making its copied reader escape
			// through the generic classifier on every rejected loop.
		default:
			return
		}
	}
}

// A short typed probe rejects early control, calls and other memory forms
// without constructing the candidate. Longer expressions keep the full path.
func (f *fn) integerLoadMapAfterLoad(scan wasm.Reader) bool {
	if !skipIntegerMapWordLoad(&scan) {
		return false
	}
	for ops := 0; ops < 12 && f.integerLoadMapWork < integerLoadMapWorkLimit; ops++ {
		f.integerLoadMapWork++
		op, err := scan.Byte()
		if err != nil {
			return false
		}
		switch {
		case op == 0x36:
			return true
		case op == 0x28:
			if !skipIntegerMapWordLoad(&scan) {
				return false
			}
		case op >= 0x20 && op <= 0x22:
			if _, err := scan.U32(); err != nil {
				return false
			}
		case op == 0x41:
			if _, err := scan.I32(); err != nil {
				return false
			}
		case op == 0x1b || op >= 0x45 && op <= 0x4f ||
			op >= 0x6a && op <= 0x6c || op >= 0x71 && op <= 0x76:
		default:
			return false
		}
	}
	return f.integerLoadMapWork < integerLoadMapWorkLimit
}

func skipIntegerMapWordLoad(scan *wasm.Reader) bool {
	align, err := scan.U32()
	if err != nil {
		return false
	}
	if align >= 64 && align < 128 {
		index, err := scan.U32()
		if err != nil || index != 0 {
			return false
		}
	}
	off, err := scan.U32()
	return err == nil && (off == 0 || floatMapOffsetsEnabled && off <= 1<<20)
}

// Keep the scoped reset outside the screen's loop. A defer declared inside a
// Go loop heap-allocates its closure even when that branch immediately returns.
func (f *fn) emitIntegerLoadMapCandidate(source *wasm.Reader) {
	previous := f.integerLoadMapProbe
	f.integerLoadMapProbe = true
	defer func() { f.integerLoadMapProbe = previous }()
	f.emitFloatMapBody(source)
}

func (f *fn) emitIntegerMapUnary(node floatMapNode, out, input Reg) {
	if node.op == 0x45 {
		f.a.NeonCmeqZeroS(out, input)
		f.a.NeonUshrS(out, out, 31)
		return
	}
	// Shift immediates are stored modulo 32. NEON right shifts cannot
	// encode zero, so preserve the zero-count identity explicitly.
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

func (f *fn) emitIntegerMapComparison(op byte, out, a, b Reg) {
	switch op {
	case 0x46, 0x47:
		f.a.NeonCmeqS(out, a, b)
		if op == 0x47 {
			f.a.NeonNot16b(out, out)
		}
	case 0x48:
		f.a.NeonCmgtS(out, b, a)
	case 0x49:
		f.a.NeonCmhiS(out, b, a)
	case 0x4a:
		f.a.NeonCmgtS(out, a, b)
	case 0x4b:
		f.a.NeonCmhiS(out, a, b)
	case 0x4c:
		f.a.NeonCmgeS(out, b, a)
	case 0x4d:
		f.a.NeonCmhsS(out, b, a)
	case 0x4e:
		f.a.NeonCmgeS(out, a, b)
	case 0x4f:
		f.a.NeonCmhsS(out, a, b)
	}
	// A comparison used as an i32 value produces 0 or 1.
	f.a.NeonUshrS(out, out, 31)
}
