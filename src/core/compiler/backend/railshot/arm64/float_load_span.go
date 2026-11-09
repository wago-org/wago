//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var floatLoadSpanEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_LOAD_SPAN") != "1"

// A successful base access proves base < memoryBytes. A declared memory32
// maximum below 4GiB then gives a fixed amount of headroom for a positive
// wrapping i32 address addition. No assumption about the runtime input is used.
func (f *fn) addressOffsetCannotWrap(delta uint32) bool {
	if delta == 0 || delta > 1<<20 || f.localBase != 0 {
		return false
	}
	m, ok := f.memoryType(0)
	return ok && !m.Shared && !m.Limits.Addr64 && m.Limits.HasMax && m.Limits.Max <= 65536 &&
		m.Limits.Max*65536+uint64(delta) <= 1<<32
}

// Only an already-proved current base may lend its certificate to base+delta.
// Without that proof an out-of-bounds base can wrap into valid memory, so the
// ordinary effective-address check and unkeyed result must remain intact.
func (f *fn) affineMemoryBoundsSource(e *elem) (kind uint8, idx, bias uint32) {
	if !floatLoadSpanEnabled || !f.boundsFacts || f.guardMode || f.threadedMemory0 || e == nil || !e.isDeferred() || e.st.typ != mtI32 || e.deferredOp() != opAdd {
		return
	}
	a, b := f.s.arg0(e), f.s.arg1(e)
	if a.elemKind() == ekValue && a.st.kind == stConst {
		a, b = b, a
	}
	if b.elemKind() != ekValue || b.st.kind != stConst || b.st.cval <= 0 || b.st.cval > 1<<20 || a.elemKind() != ekValue {
		return
	}
	k, x := boundsSource(a.st)
	d := uint32(b.st.cval)
	if k == 0 || !f.addressOffsetCannotWrap(d) || !f.boundsCertCovers(k, x, 1) {
		return
	}
	f.stats.peep("affine-memory-bounds-source")
	return k, x, d
}

// Consume only the optional const/add part of a copied-reader load address.
// The caller charges both skipped operations and leaves the load to its normal
// decoder. Rejection never changes the reader.
func (f *fn) peekPositiveLoadOffset(r *wasm.Reader) (uint32, bool) {
	p := *r
	op, e := p.Byte()
	if e != nil || op != 0x41 {
		return 0, false
	}
	d, e := p.I32()
	if e != nil || d <= 0 || !f.addressOffsetCannotWrap(uint32(d)) {
		return 0, false
	}
	op, e = p.Byte()
	if e != nil || op != 0x6a {
		return 0, false
	}
	op, ok := p.Peek()
	if !ok || op < 0x28 || op > 0x35 {
		return 0, false
	}
	*r = p
	return uint32(d), true
}
