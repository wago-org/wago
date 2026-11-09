//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var loopLocalLeaseEnabled = os.Getenv("WAGO_ARM64_NO_LOOP_LOCAL_LEASE") != "1"

// A lease belongs to one flat loop. No source control join or helper can run
// while its locals temporarily use the ordinary pinned-local machinery.
type loopLocalLease struct {
	depth uint32
	count uint8
	local [6]uint16
	reg   [6]Reg
}

const loopLocalLeaseWorkLimit = 4096

func (f *fn) planLoopLocalLease(r *wasm.Reader) (loopLocalLease, bool) {
	var plan loopLocalLease
	if !loopLocalLeaseEnabled || !callFreeLoopRegionEnabled || f.nLocals <= 64 ||
		f.pinnedLocalMask != 0 || f.fpinnedLocalMask != 0 || f.loopLocalLease.depth != 0 ||
		f.interruptible || f.threadedMemory0 || f.moduleEH || f.preserveCallerPins ||
		len(f.gcTypeLayouts) != 0 || f.gcFrameRoots != nil || len(f.customInstructions) != 0 ||
		len(f.intervalLast) != 0 || f.inlineDepth != 0 || f.localBase != 0 ||
		f.loopLocalLeaseWork >= loopLocalLeaseWorkLimit {
		return plan, false
	}
	var cells [16]struct {
		local uint16
		refs  uint16
	}
	count := 0
	peek := *r
	start := peek.Offset()
	var imm wasm.InstructionImmediate
	for n := 0; n < 128 && peek.Offset()-start < 512; n++ {
		if f.loopLocalLeaseWork >= loopLocalLeaseWorkLimit {
			return plan, false
		}
		f.loopLocalLeaseWork++
		op, err := peek.Byte()
		if err != nil {
			return plan, false
		}
		// Only a final conditional self-backedge may transfer control. Source
		// validation supplies the empty stack at this void loop's end.
		if op == 0x0d {
			label, err := peek.U32()
			end, endErr := peek.Byte()
			if err != nil || endErr != nil || label != 0 || end != 0x0b || peek.Offset()-start > 512 {
				return plan, false
			}
			for slot := 0; slot < len(plan.local); slot++ {
				best := -1
				for i := 0; i < count; i++ {
					if cells[i].refs >= 2 && (best < 0 || cells[i].refs > cells[best].refs) {
						best = i
					}
				}
				if best < 0 {
					break
				}
				plan.local[plan.count] = cells[best].local
				plan.count++
				cells[best].refs = 0
			}
			return plan, plan.count != 0
		}
		if !(op == 0x01 || op == 0x1a || op == 0x1b || op >= 0x20 && op <= 0x22 ||
			op == 0x28 || op == 0x29 || op >= 0x2c && op <= 0x37 || op >= 0x3a && op <= 0x3e ||
			op == 0x41 || op == 0x42 || op >= 0x45 && op <= 0x5a || op >= 0x67 && op <= 0x8a ||
			op == 0xa7 || op == 0xac || op == 0xad || op >= 0xc0 && op <= 0xc4) ||
			f.classifier.ClassifyInto(&peek, op, &imm) != nil {
			return plan, false
		}
		if imm.TouchesMemory && imm.MemIndex != 0 {
			return plan, false
		}
		if op >= 0x20 && op <= 0x22 {
			x := imm.Index
			if uint64(x) >= uint64(len(f.localType)) || x > 65535 || (f.localType[x] != mtI32 && f.localType[x] != mtI64) {
				return plan, false
			}
			i := 0
			for i < count && cells[i].local != uint16(x) {
				i++
			}
			if i == count {
				if count == len(cells) {
					return plan, false
				}
				cells[i].local = uint16(x)
				count++
			}
			cells[i].refs++
		}
	}
	return plan, false
}

func (f *fn) beginLoopLocalLease(r *wasm.Reader, fr *ctrlFrame) {
	if fr.paramN != 0 || fr.resultN != 0 {
		return
	}
	plan, ok := f.planLoopLocalLease(r)
	if !ok {
		return
	}
	// Reserve through the existing allocator masks; never displace a live
	// operand, immutable constant, memory base, global, or other reservation.
	n := 0
	for _, reg := range [...]Reg{X19, X20, X21, X22, X23, X27} {
		if f.pinned.has(reg) || f.reserved.has(reg) || f.regUser[reg] != nil {
			continue
		}
		plan.reg[n] = reg
		n++
		if n == int(plan.count) {
			break
		}
	}
	if n == 0 {
		return
	}
	plan.count = uint8(n)
	plan.depth = uint32(len(f.ctrl) + 1)
	for i := 0; i < n; i++ {
		x, reg := int(plan.local[i]), plan.reg[i]
		zero := f.locals[x].state == lsConstZero
		f.locals[x].reg = reg
		f.pinnedLocalMask = f.pinnedLocalMask.add(reg)
		if zero {
			f.materializeZeroLocal(x, false)
		} else {
			f.loadLocalReg(x, reg, false)
		}
		f.locals[x].state = lsReg
	}
	f.loopLocalLease = plan
	fr.set(ctrlLoopCallFree, true)
	f.stats.peep("loop-local-lease")
}

func (f *fn) finishLoopLocalLease(depth int) {
	plan := f.loopLocalLease
	if plan.depth != uint32(depth) {
		return
	}
	// The only exit is the final backedge's fallthrough. opEnd has flushed
	// pending roots, so every final source local can now return to its home.
	for i := 0; i < int(plan.count); i++ {
		x, reg := int(plan.local[i]), plan.reg[i]
		f.storeLocalReg(x, reg, false)
		f.locals[x].reg = regNone
		f.locals[x].state = lsMem
		f.pinnedLocalMask = f.pinnedLocalMask.remove(reg)
	}
	f.loopLocalLease = loopLocalLease{}
}
