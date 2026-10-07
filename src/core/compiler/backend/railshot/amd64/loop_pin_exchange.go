//go:build amd64

package amd64

import (
	"os"
	"runtime"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// A small straight-line loop may borrow an inactive function pin. Its previous
// value is homed before the loop and restored on the loop's only exit. Keep the
// default on the platform with native correctness and performance qualification.
// WAGO_AMD64_NO_LOOP_PIN_EXCHANGE=1 is the rollback switch.
var loopPinExchangeEnabled = runtime.GOOS == "linux" && os.Getenv("WAGO_AMD64_NO_LOOP_PIN_EXCHANGE") != "1"

const loopPinBits = 17

func loopPinEntry(old, new int, reg Reg) uint64 {
	return uint64(old+1) | uint64(new)<<7 | uint64(reg)<<13
}

func (f *fn) planLoopPinExchange(r *wasm.Reader) (uint64, uint64) {
	if f.nLocals == 0 || f.nLocals > 64 || f.localBase != 0 || f.moduleEH || len(f.customInstructions) != 0 || f.intervalActive != 0 || len(f.pinnedLocals) == 0 {
		return 0, 0
	}
	look := *r
	start := look.Offset()
	var reads, writes [64]uint8
	var written uint64
	var imm wasm.InstructionImmediate
	backedge := false
	closed := false
	for step := 0; step < 96 && look.Offset()-start <= 512; step++ {
		op, err := look.Byte()
		if err != nil || f.classifier.ClassifyInto(&look, op, &imm) != nil {
			return 0, 0
		}
		if backedge {
			if op != 0x0b {
				return 0, 0
			}
			closed = true
			break
		}
		if op == 0x0d && imm.Index == 0 {
			backedge = true
			continue
		}
		if op == 0x0b || !loopPinAllowed(op) {
			return 0, 0
		}
		switch imm.Kind {
		case wasm.InstrLocalGet:
			if imm.Index >= uint32(f.nLocals) {
				return 0, 0
			}
			if reads[imm.Index] != 255 {
				reads[imm.Index]++
			}
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			if imm.Index >= uint32(f.nLocals) {
				return 0, 0
			}
			if writes[imm.Index] != 255 {
				writes[imm.Index]++
			}
			written |= uint64(1) << imm.Index
		}
	}
	if !closed {
		return 0, 0
	}
	var plan uint64
	var selected uint64
	for count := 0; count < 2; count++ {
		bestNew, bestScore := -1, 0
		for x := 0; x < f.nLocals; x++ {
			if selected&(uint64(1)<<x) != 0 || f.locals[x].reg != regNone || f.localType[x] != mtI32 || f.locals[x].state != lsMem {
				continue
			}
			score := int(reads[x])*2 + int(writes[x])
			if score > bestScore {
				bestNew, bestScore = x, score
			}
		}
		if bestScore < 6 {
			break
		}
		bestOld := -1
		for _, x := range f.pinnedLocals {
			if selected&(uint64(1)<<x) != 0 || f.localType[x] != mtI32 || reads[x] != 0 || writes[x] != 0 || f.locals[x].state == lsConstZero {
				continue
			}
			bestOld = x
			break
		}
		if bestOld == -1 {
			break
		}
		plan |= loopPinEntry(bestOld, bestNew, f.locals[bestOld].reg) << (count * loopPinBits)
		selected |= uint64(1)<<bestOld | uint64(1)<<bestNew
	}
	return plan, written
}

func loopPinAllowed(op byte) bool {
	switch {
	case op == 0x01 || op == 0x1a || op == 0x1b: // nop, drop, select
		return true
	case op >= 0x20 && op <= 0x22: // local get/set/tee
		return true
	case op >= 0x28 && op <= 0x3e: // scalar loads/stores
		return true
	case op >= 0x41 && op <= 0x44: // numeric constants
		return true
	case op >= 0x45 && op <= 0x66: // numeric comparisons
		return true
	case op >= 0x67 && op <= 0xa6: // numeric arithmetic
		return true
	case op >= 0xa7 && op <= 0xbf: // numeric conversions
		return true
	case op >= 0xc0 && op <= 0xc4: // sign extension
		return true
	}
	return false
}

func (f *fn) applyLoopPinExchange(plan uint64) {
	for plan != 0 {
		entry := plan & ((1 << loopPinBits) - 1)
		old := int(entry&0x7f) - 1
		newLocal := int((entry >> 7) & 0x3f)
		reg := Reg((entry >> 13) & 0xf)
		if !f.usesCalls || f.locals[old].state == lsReg {
			f.storeLocalReg(old, reg, false)
		}
		f.locals[old].reg = regNone
		f.locals[old].state = lsMem
		f.locals[newLocal].reg = reg
		f.loadLocalReg(newLocal, reg, false)
		f.locals[newLocal].state = lsStackReg
		for i, x := range f.pinnedLocals {
			if x == old {
				f.pinnedLocals[i] = newLocal
				break
			}
		}
		plan >>= loopPinBits
	}
}

func (f *fn) restoreLoopPinExchange(plan uint64, reachable bool) {
	for plan != 0 {
		entry := plan & ((1 << loopPinBits) - 1)
		old := int(entry&0x7f) - 1
		newLocal := int((entry >> 7) & 0x3f)
		reg := Reg((entry >> 13) & 0xf)
		if reachable {
			if !f.usesCalls || f.locals[newLocal].state == lsReg {
				f.storeLocalReg(newLocal, reg, false)
			}
			f.loadLocalReg(old, reg, false)
		}
		f.locals[newLocal].reg = regNone
		f.locals[newLocal].state = lsMem
		f.locals[old].reg = reg
		f.locals[old].state = lsStackReg
		for i, x := range f.pinnedLocals {
			if x == newLocal {
				f.pinnedLocals[i] = old
				break
			}
		}
		plan >>= loopPinBits
	}
}
