//go:build arm64

package arm64

import "encoding/binary"

// Repeat only a register-resident entry guard at the backedge. The initial
// guard still protects zero iterations; continuation jumps to its guarded body.
// Unlike unrolling, this neither copies nor moves any body instruction, so
// already-patched inner control and memory operations keep their exact sites.
func (f *fn) tryPretestLoopLatch(fr *ctrlFrame) bool {
	if !f.opt(optScalarLoopUnroll) || fr.kind != cfLoop || fr.paramN != 0 || fr.resultN != 0 || fr.height != 0 || f.depth() != 0 || f.makesCalls || f.usesCalls || f.lazyZero || f.unreachable || profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.intervalControl || f.selectGroupEnd != 0 || f.streamingReduction || f.policy.CompactNative || f.a.ReserveBranch19 || f.stats != nil && f.stats.RecordSources {
		return false
	}
	start := fr.controlSite
	if start < 0 || start&3 != 0 || start+4 > f.a.Len() {
		return false
	}
	budget := f.newScalarLoopMetadataBudget()
	// Only original caller-local homes may feed the repeated guard. They stay
	// dedicated and current across this call-free function's backedges.
	home := func(reg Reg) bool {
		for x := 0; x < f.nLocals; x++ {
			if !budget.take(1) {
				return false
			}
			local := f.locals[x]
			if local.reg == reg && !local.isFloat && local.state != lsMem && local.state != lsConstZero {
				return true
			}
		}
		return false
	}
	var words [2]uint32
	words[0] = binary.LittleEndian.Uint32(f.a.B[start:])
	exit := 0
	if words[0]&0x7e000000 != 0x34000000 {
		cmp := words[0]
		switch {
		case cmp&0x7f80001f == 0x7100001f: // CMP W/Xn,#imm
			if !home(Reg(cmp >> 5 & 31)) {
				return false
			}
		case cmp&0x7fe0fc1f == 0x6b00001f: // unshifted CMP W/Xn,W/Xm
			if !home(Reg(cmp>>5&31)) || !home(Reg(cmp>>16&31)) {
				return false
			}
		default:
			return false
		}
		if start+8 > f.a.Len() {
			return false
		}
		exit = 1
		words[1] = binary.LittleEndian.Uint32(f.a.B[start+4:])
	}
	branch := words[exit]
	zero := branch&0x7e000000 == 0x34000000
	conditional := branch&0xff000010 == 0x54000000 && branch&15 < 14
	if branch&0x00ffffe0 != 0 || !(exit == 0 && zero && home(Reg(branch&31)) || exit == 1 && conditional) {
		return false
	}
	loopIndex := len(f.ctrl)
	for i := range f.ctrl {
		if !budget.take(1) {
			return false
		}
		if &f.ctrl[i] == fr {
			loopIndex = i
			break
		}
	}
	if loopIndex == len(f.ctrl) {
		return false
	}
	site := start + 4*exit
	var owner *ctrlFrame
	for i := 0; i < loopIndex; i++ {
		if !budget.take(1) {
			return false
		}
		candidate := &f.ctrl[i]
		if candidate.kind != cfBlock && candidate.kind != cfIf || candidate.branchN != 0 || candidate.height != 0 {
			continue
		}
		first, second, overflow := f.frameEndSites(candidate)
		if !budget.take(2 + len(overflow)) {
			return false
		}
		matches := func(packed uint32) bool {
			return packed&frameEndConditional != 0 && int(packed&^frameEndConditional)-1 == site
		}
		found := matches(first) || matches(second)
		for _, packed := range overflow {
			found = found || matches(packed)
		}
		if found {
			if owner != nil {
				return false
			}
			owner = candidate
		}
	}
	target := start + 4*(exit+1)
	latch := f.a.Len() + 4*exit
	delta := target - latch
	// Keep this alternative to unrolling bounded to a small native body.
	if owner == nil || f.a.Len()-target+4*(exit+1) > 64 || delta < -(1<<20) || delta >= 1<<20 {
		return false
	}
	if exit != 0 {
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, words[0])
	}
	if zero {
		branch ^= 1 << 24
	} else {
		branch ^= 1
	}
	f.a.B = binary.LittleEndian.AppendUint32(f.a.B, branch)
	f.patchBranch19(latch, target)
	f.appendFrameEnd(owner, f.a.Branch(), false)
	f.stats.peep("pretest-loop-latch")
	return true
}
