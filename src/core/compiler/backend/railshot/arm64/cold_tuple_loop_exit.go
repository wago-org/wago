//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var coldTupleLoopExitEnabled = true

const coldTupleLoopExitSite = -3

func (fr *ctrlFrame) isColdTupleLoopExit() bool {
	return fr.kind == cfBlock && fr.controlSite == coldTupleLoopExitSite
}

// tryColdTupleLoopExit makes the continuation fall through when an integer-pair
// if ends an optimized parameter loop. Its true arm only reads two dedicated
// local homes; its false arm computes arguments and branches back to that loop.
// An existing cold edge carries the true arm's canonical result stores.
func (f *fn) tryColdTupleLoopExit(r *wasm.Reader, signature wasm.Reader, types []machineType) (bool, error) {
	if !f.opt(optColdTupleLoopExit) || f.unreachable || len(types) != 2 || len(f.locals) > 64 || len(f.ctrl) == 0 || f.loopLocalParams.depth != uint32(len(f.ctrl)) {
		return false, nil
	}
	loop := &f.ctrl[len(f.ctrl)-1]
	if loop.kind != cfLoop || !loop.has(ctrlLoopCallFree) || loop.resultN != 2 || f.depth() != loop.height+1 {
		return false, nil
	}
	// Active loopLocalParams already excludes calls, GC/EH, profiling, custom
	// instructions, interruptibility, compact mode and regional register leases.
	index, err := signature.I64()
	if err != nil || index < 0 || uint64(index) > uint64(^uint32(0)) {
		return false, nil
	}
	ft, ok := f.m.TypeFunc(uint32(index))
	if !ok || len(ft.Params) != 0 || len(ft.Results) != 2 {
		return false, nil
	}
	for _, typ := range ft.Results {
		if typ != wasm.I32 && typ != wasm.I64 {
			return false, nil
		}
	}
	rr := *r
	var locals [2]int
	var registers [2]Reg
	for i := range locals {
		op, err := rr.Byte()
		if err != nil || op != 0x20 {
			return false, nil
		}
		x, err := rr.U32()
		if err != nil {
			return false, nil
		}
		absolute := uint64(x) + uint64(f.localBase)
		if x < f.inlineForwardN {
			absolute = uint64(f.inlineForward[x])
		}
		if absolute >= uint64(len(f.localType)) || f.localType[absolute] != types[i] {
			return false, nil
		}
		reg, floating, pinned := f.pinReg(int(absolute))
		if !pinned || floating {
			return false, nil
		}
		locals[i], registers[i] = int(absolute), reg
	}
	op, err := rr.Byte()
	if err != nil || op != 0x05 {
		return false, nil
	}
	continuation := rr.Offset()
	proved := false
	for n := 0; n < 64 && rr.Offset()-continuation < 256; n++ {
		op, err := rr.Byte()
		if err != nil {
			return false, nil
		}
		switch {
		case op == 0x0c:
			label, err := rr.U32()
			if err != nil || label != 1 {
				return false, nil
			}
			end, err := rr.Byte()
			if err != nil || end != 0x0b {
				return false, nil
			}
			proved = true
		case op == 0x01:
		case op == 0x20:
			if _, err := rr.U32(); err != nil {
				return false, nil
			}
		case op == 0x41:
			if _, err := rr.I32(); err != nil {
				return false, nil
			}
		case op == 0x42:
			if _, err := rr.I64(); err != nil {
				return false, nil
			}
		case op >= 0x45 && op <= 0x5a || op >= 0x67 && op <= 0x8a || op == 0xa7 || op == 0xac || op == 0xad:
		default:
			return false, nil
		}
		if proved {
			break
		}
	}
	if !proved {
		return false, nil
	}
	if err := r.JumpTo(continuation); err != nil {
		return false, err
	}
	f.materializePendingTraps()
	for _, local := range locals {
		f.recoverLocal(local)
	}
	condition := f.s.back()
	f.flushBelow(condition)
	site := -1
	if isFusableCondition(condition) {
		if f.opt(optZeroBranch) {
			if reg, owned, wide, ok := f.condenseSimpleEqzOperand(condition); ok {
				if wide {
					site = f.a.Cbz64(reg)
				} else {
					site = f.a.Cbz32(reg)
				}
				if owned {
					f.release(reg)
				}
				f.stats.peep("zero-branch")
			}
		}
		if site < 0 {
			site = f.a.Bcond(f.condenseToFlags(condition))
		}
	} else {
		reg, owned := f.materializeRead(f.popValue())
		if f.opt(optZeroBranch) {
			site = f.a.Cbnz32(reg)
			f.stats.peep("zero-branch")
		} else {
			f.a.CmpImm32(reg, 0)
			site = f.a.Bcond(condNE)
		}
		if owned {
			f.release(reg)
		}
	}
	fr := ctrlFrame{kind: cfBlock, resultN: 2, branchN: 2, types: types, res0: types[0], height: f.depth(), controlSite: coldTupleLoopExitSite}
	f.setFrameBaseTypes(&fr, f.currentLogicalTypes())
	f.captureGCFrameShape(&fr)
	// Only the cold edge reaches this block's end. Preserve its header tracker,
	// including lazy zero locals that the hot continuation might merely read.
	var entry packedLocStates
	for x := range f.locals {
		entry.set(x, f.locals[x].state)
	}
	f.setFrameEntryState(&fr, entry)
	f.pushCtrl(&fr)
	frame := &f.ctrl[len(f.ctrl)-1]
	mark := f.a.Len()
	for i, local := range locals {
		value := f.pushValue(storage{kind: stLocalReg, typ: types[i], reg: registers[i], idx: uint32(local)})
		f.applyFactsForLocal(value, local)
	}
	f.flush()
	code := append([]byte(nil), f.a.B[mark:]...)
	f.a.B = f.a.B[:mark]
	f.setDepthTypesWithGCRoots(f.frameBaseTypes(frame), f.frameBaseGCRoots(frame))
	f.appendFrameColdEdge(frame, coldEdge{site: site, code: code})
	frame.set(ctrlEndReachable, true)
	f.stats.peep("cold-tuple-loop-exit")
	return true, nil
}
