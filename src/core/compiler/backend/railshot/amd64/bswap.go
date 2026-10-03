//go:build amd64

package amd64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var byteSwapCoverEnabled = os.Getenv("WAGO_AMD64_NO_BSWAP_COVER") != "1"

// tryByteSwapAfterTee covers the exact Rust from_be i32 sequence following a
// local.tee. The tee has already published the original value to its local;
// the ten following pure instructions only byte-swap its stack result.
func (f *fn) tryByteSwapAfterTee(r *wasm.Reader, x int) bool {
	if !byteSwapCoverEnabled || f.unreachable || x < 0 || x >= len(f.localType) || f.localType[x] != mtI32 {
		return false
	}
	if next, ok := r.Peek(); !ok || next != 0x41 {
		return false
	}
	e := f.s.back()
	if e == nil || !e.isValue() || e.st.typ != mtI32 {
		return false
	}
	look := *r
	op := func(want byte) bool {
		got, err := look.Byte()
		return err == nil && got == want
	}
	i32const := func(want int32) bool {
		if !op(0x41) {
			return false
		}
		got, err := look.I32()
		return err == nil && got == want
	}
	if !i32const(0x00ff00ff) || !op(0x71) || !i32const(8) || !op(0x78) || !op(0x20) {
		return false
	}
	local, err := look.U32()
	if err != nil || int(local)+f.localBase != x ||
		!i32const(24) || !op(0x78) || !i32const(0x00ff00ff) ||
		!op(0x71) || !op(0x72) {
		return false
	}
	if err := r.JumpTo(look.Offset()); err != nil {
		return false
	}
	reg := f.materialize(e)
	f.a.Bswap32(reg)
	f.stats.peep("i32-bswap-tee")
	return true
}
