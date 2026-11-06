//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var bulkFillCopyForwardEnabled = true

type bulkValueKey struct {
	value uint32
	local bool
}

func bulkOperandKey(e *elem) (bulkValueKey, bool) {
	if e == nil || e.elemKind() != ekValue || e.st.typ != mtI32 {
		return bulkValueKey{}, false
	}
	switch e.st.kind {
	case stConst:
		return bulkValueKey{value: uint32(e.st.cval)}, true
	case stLocalReg, stLocalRef:
		return bulkValueKey{value: e.st.idx, local: true}, true
	}
	return bulkValueKey{}, false
}

// Only the next three pure leaf operands and memory.copy are admitted. Thus no
// intervening call, write, local assignment or control edge can invalidate the
// filled-source proof. A shared source could be changed by another thread.
func (f *fn) recordFilledCopy(r *wasm.Reader, memory uint32) {
	f.filledCopyEnd = 0
	if !f.opt(optBulkFillCopyForward) || f.localBase != 0 || len(f.customInstructions) != 0 ||
		f.memoryAddr64(memory) || f.memoryShared(memory) {
		return
	}
	count := f.s.back()
	value := f.s.prev(count)
	dst := f.s.prev(value)
	countKey, countOK := bulkOperandKey(count)
	dstKey, dstOK := bulkOperandKey(dst)
	if !countOK || !dstOK || value == nil || value.elemKind() != ekValue || value.st.kind != stConst {
		return
	}
	scan := *r
	read := func() (bulkValueKey, bool) {
		op, err := scan.Byte()
		if err != nil {
			return bulkValueKey{}, false
		}
		switch op {
		case 0x20:
			x, err := scan.U32()
			return bulkValueKey{value: x, local: true}, err == nil
		case 0x41:
			x, err := scan.I32()
			return bulkValueKey{value: uint32(x)}, err == nil
		}
		return bulkValueKey{}, false
	}
	if _, ok := read(); !ok {
		return
	}
	if source, ok := read(); !ok || source != dstKey {
		return
	}
	if length, ok := read(); !ok || length != countKey {
		return
	}
	op, err := scan.Byte()
	if err != nil || op != 0xfc {
		return
	}
	sub, err := scan.U32()
	if err != nil || sub != 10 {
		return
	}
	destination, err := scan.U32()
	if err != nil || f.memoryAddr64(destination) || f.memoryShared(destination) {
		return
	}
	source, err := scan.U32()
	if err != nil || source != memory {
		return
	}
	f.filledCopyEnd = f.tracePCBase + uint32(scan.Offset())
	f.filledCopyByte = byte(value.st.cval)
}
