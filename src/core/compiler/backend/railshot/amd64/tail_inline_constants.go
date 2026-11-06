//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Facts live only in entry copies. The general loop is compiled with no facts.
// Each child has its own argument facts, and alternative source branches keep
// their parent's facts. Nonconstant control conservatively blocks local writes.
type inlineTailConstants struct {
	values         [8]int64
	base           int
	known, blocked uint8
	entry          *inlineTailPeelEntry
}
type inlineTailPeelEntry struct{ sites []int }

func (f *fn) captureInlineTailConstants(t *inlineTarget, base int) *inlineTailConstants {
	if !t.tailLoop() || t.paramCount() > 8 {
		return nil
	}
	var values [8]int64
	known := uint8(0)
	node := f.s.back()
	for i := t.paramCount() - 1; i >= 0; i-- {
		if node == nil || node == f.s.head {
			return nil
		}
		if node.isValue() && node.st.kind == stConst {
			values[i] = node.st.cval
			known |= 1 << i
		}
		node = baseOfValentBlock(node).prev
	}
	if known == 0 {
		return nil
	}
	for i := t.paramCount(); i < int(t.localTypeEnd-t.typeStart); i++ {
		known |= 1 << i
	}
	return &inlineTailConstants{values: values, base: base, known: known}
}
func (f *fn) inlineTailConstantGet(local int) (int64, bool) {
	c := f.inlineTailConstants
	if c == nil || local < c.base || local >= c.base+8 {
		return 0, false
	}
	i := local - c.base
	return c.values[i], c.known&(1<<i) != 0
}
func (f *fn) inlineTailConstantSet(local int) {
	c := f.inlineTailConstants
	if c == nil || local < c.base || local >= c.base+8 {
		return
	}
	i := local - c.base
	c.known &^= 1 << i
	value := f.s.back()
	if c.blocked&(1<<i) == 0 && value != nil && value.isValue() && value.st.kind == stConst {
		c.values[i] = value.st.cval
		c.known |= 1 << i
	}
}

func (f *fn) scanInlineTailControl(r wasm.Reader, op byte) (wasm.Reader, int, int, int, bool) {
	if f.classifier.SkipImmediate(&r, op) != nil {
		return r, 0, 0, 0, false
	}
	start, alternative, depth := r.Offset(), -1, 0
	for r.HasNext() {
		at := r.Offset()
		code, err := r.Byte()
		if err != nil {
			return r, 0, 0, 0, false
		}
		switch code {
		case 0x02, 0x03, 0x04, 0x1f:
			depth++
		case 0x05:
			if depth == 0 {
				alternative = at
			}
		case 0x0b:
			if depth == 0 {
				return r, start, alternative, at, true
			}
			depth--
		}
		if f.classifier.SkipImmediate(&r, code) != nil {
			return r, 0, 0, 0, false
		}
	}
	return r, 0, 0, 0, false
}
func (f *fn) blockInlineTailConstantWrites(r wasm.Reader, op byte) {
	c := f.inlineTailConstants
	if c == nil || op != 0x02 && op != 0x04 {
		return
	}
	end, start, _, _, ok := f.scanInlineTailControl(r, op)
	if !ok {
		c.known = 0
		c.blocked = 255
		return
	}
	if r.JumpTo(start) != nil {
		c.known = 0
		c.blocked = 255
		return
	}
	writes := uint8(0)
	for r.Offset() < end.Offset() {
		code, err := r.Byte()
		if err != nil {
			c.known = 0
			c.blocked = 255
			return
		}
		if code == 0x21 || code == 0x22 {
			local, err := r.U32()
			if err != nil || local >= 8 {
				c.known = 0
				c.blocked = 255
				return
			}
			writes |= 1 << local
		} else if f.classifier.SkipImmediate(&r, code) != nil {
			c.known = 0
			c.blocked = 255
			return
		}
	}
	c.known &^= writes
	c.blocked |= writes
}

// Keep one structured frame when choosing a constant if arm. Thus all source
// branch depths remain valid, and no instruction in the unused arm executes.
func (f *fn) tryInlineTailConstantIf(r *wasm.Reader) (bool, error) {
	if f.inlineTailConstants == nil || f.unreachable {
		return false, nil
	}
	condition := f.s.back()
	if condition == nil || !condition.isValue() || condition.st.kind != stConst {
		return false, nil
	}
	end, start, alternative, stop, ok := f.scanInlineTailControl(*r, 0x04)
	if !ok {
		return false, nil
	}
	typeReader := *r
	types, err := typeReader.Bytes(start - r.Offset())
	if err != nil {
		return false, err
	}
	chosenStart, chosenStop := start, stop
	if uint32(condition.st.cval) != 0 {
		if alternative >= 0 {
			chosenStop = alternative
		}
	} else if alternative >= 0 {
		chosenStart = alternative + 1
	} else {
		chosenStart = stop
	}
	armReader := *r
	if err := armReader.JumpTo(chosenStart); err != nil {
		return false, err
	}
	arm, err := armReader.Bytes(chosenStop - chosenStart)
	if err != nil {
		return false, err
	}
	body := make([]byte, 0, 2+len(types)+len(arm))
	body = append(body, 0x02)
	body = append(body, types...)
	body = append(body, arm...)
	body = append(body, 0x0b)
	f.materializeTrapsBefore(condition)
	f.erase(condition)
	previousPC := f.tracePCBase
	f.tracePCBase += uint32(chosenStart) - uint32(1+len(types))
	err = f.bodyLoop(wasm.NewReader(body), len(f.ctrl))
	f.tracePCBase = previousPC
	if err != nil {
		return true, err
	}
	if err = r.JumpTo(end.Offset()); err != nil {
		return true, err
	}
	f.stats.peep("inline-tail-constant-if")
	return true, nil
}
