//go:build amd64

package amd64

func (f *fn) countedHeaderEnd(loop *ctrlFrame) int {
	if cooperativeLinearSumEnabled && f.interruptible {
		if m := f.ctrlMerge(loop); m != nil {
			return m.cooperativeHeaderEnd()
		}
		return -1
	}
	return loop.controlSite
}

// Policy-neutral preserving poll pattern, kept local to this experimental
// consumer to avoid changing already-qualified vector machine code. Caller
// recreates flags explicitly. One compiler-owned slot preserves a live sum.
func (f *fn) emitLinearSumPoll(scratch Reg, slot int, pc uint32) {
	f.a.Store64(RSP, f.spillOff(slot), scratch)
	old := f.wasmPC
	f.wasmPC = pc
	f.emitInterruptCheck(scratch)
	f.wasmPC = old
	f.a.Load64(scratch, RSP, f.spillOff(slot))
	f.stats.peep("cooperative-linear-sum-poll")
}
