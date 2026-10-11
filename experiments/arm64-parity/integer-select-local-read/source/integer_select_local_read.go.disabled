//go:build arm64

package arm64

import "os"

var integerSelectLocalReadEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ") != "1"

// Unlike popValue, these operands remain on the stack until all reads are
// protected. Regional homes are leases: erasing a borrowed reference before
// pinning its physical register would let another operand evict its value.
// The caller passes the declared destination width: a deferred comparison
// records its input width, although its result is always I32.
func (f *fn) emitIntegerSelectLocalRead(a, b, cond *elem, dest Reg, wide bool) {
	saved := f.pinned
	f.pinned = f.pinned.add(dest)
	c, ownC := f.selectSinkRead(cond)
	f.pinned = f.pinned.add(c)
	br, ownB := f.selectSinkRead(b)
	f.pinned = f.pinned.add(br)
	ar, ownA := f.selectSinkRead(a)
	f.pinned = f.pinned.add(ar)
	f.a.CmpImm32(c, 0)
	f.a.Csel(dest, br, ar, condE, wide)
	f.pinned = saved
	if ownC {
		f.release(c)
	}
	if ownB {
		f.release(br)
	}
	if ownA {
		f.release(ar)
	}
	f.erase(cond)
	f.erase(b)
	f.erase(a)
	if !ownC || !ownB || !ownA {
		f.stats.peep("integer-select-local-read")
	}
}
