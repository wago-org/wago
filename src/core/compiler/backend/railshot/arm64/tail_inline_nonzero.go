//go:build arm64

package arm64

const inlineWriteVersionLimit = 1 << 28

// Local facts from a control edge are scoped by both the open if and the local's
// write version. Tokens saturate, and deeper controls simply retain the checks.
func (f *fn) inlineNonzeroPredicate(node *elem) uint32 {
	if f.inlineTailLoopFrame == 0 || !f.opt(optValueFacts) || node == nil {
		return 0
	}
	nonzeroThen := true
	if node.isDeferred() && node.deferredOp() == opEqz {
		node = f.s.arg0(node)
		nonzeroThen = false
	}
	if node == nil || node.elemKind() != ekValue || (node.st.kind != stLocalReg && node.st.kind != stLocalRef) {
		return 0
	}
	local := node.st.index()
	index := local - f.localBase
	if index < 0 || index >= len(f.inlineWriteVersions) || local >= len(f.localType) || node.st.typ != f.localType[local] {
		return 0
	}
	version := f.inlineWriteVersions[index]
	if version >= inlineWriteVersionLimit {
		return 0
	}
	value := version<<4 | uint32(index+1)<<1
	if nonzeroThen {
		value |= 1
	}
	return value
}
func (f *fn) setInlineNonzeroPredicate(value uint32) {
	if f.inlineTailLoopFrame == 0 {
		return
	}
	index := len(f.ctrl) - f.inlineTailLoopFrame
	if index >= 0 && index < len(f.inlineNonzeroPredicates) {
		f.inlineNonzeroPredicates[index] = value
	}
}
func (f *fn) inlineLocalNonzero(local int) bool {
	if f.inlineTailLoopFrame == 0 || !f.opt(optValueFacts) {
		return false
	}
	index := local - f.localBase
	if index < 0 || index >= len(f.inlineWriteVersions) {
		return false
	}
	for offset, value := range f.inlineNonzeroPredicates {
		frameIndex := f.inlineTailLoopFrame + offset
		if value == 0 || frameIndex >= len(f.ctrl) || f.ctrl[frameIndex].kind != cfIf {
			continue
		}
		if int(value>>1&7) != index+1 || value>>4 != f.inlineWriteVersions[index] {
			continue
		}
		then := value&1 != 0
		if then != f.ctrl[frameIndex].has(ctrlHasElse) {
			return true
		}
	}
	return false
}

// Every scalar local-write cover invalidates these facts, including fused
// selects and if assignments that bypass setLocal. Assignment versions are
// monotone through control joins; writes in another arm conservatively kill reuse.
func (f *fn) invalidateLocalWriteFacts(local int) {
	f.invalidateBoundsCertFor(1, uint32(local))
	if f.inlineTailLoopFrame == 0 {
		return
	}
	index := local - f.localBase
	if index >= 0 && index < len(f.inlineWriteVersions) && f.inlineWriteVersions[index] < inlineWriteVersionLimit {
		f.inlineWriteVersions[index]++
	}
}
