//go:build arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// For A,B = B,A%B, alternate the physical A/B roles and compute each
// remainder in the old A. Four copies restore the original roles. Restore the
// remainder local unless a bounded forward suffix proves it unread.
func (f *fn) unrollRemainderPair(start int, words []uint32, exit, increment int, owner *ctrlFrame, after ...*wasm.Reader) bool {
	if len(words) != 6 || exit != 0 || increment != 5 || owner == nil {
		return false
	}
	sf := words[1] & 0x80000000
	q, a, b := Reg(words[1]&31), Reg(words[1]>>5&31), Reg(words[1]>>16&31)
	r := Reg(words[2] & 31)
	if words[0] != sf|0x34000000|uint32(b) ||
		words[1] != sf|0x1ac00800|uint32(b)<<16|uint32(a)<<5|uint32(q) ||
		words[2] != sf|0x1b008000|uint32(b)<<16|uint32(a)<<10|uint32(q)<<5|uint32(r) ||
		words[3] != 0xaa0003e0|uint32(b)<<16|uint32(a) ||
		words[4] != 0xaa0003e0|uint32(r)<<16|uint32(b) ||
		a == b || a == r || b == r || a >= 31 || b >= 31 || r >= 31 ||
		!f.pinnedLocalMask.has(a) || !f.pinnedLocalMask.has(b) || !f.pinnedLocalMask.has(r) ||
		q == a || q == b || q == r || f.pinnedLocalMask.has(q) || f.pinned.has(q) || f.reserved.has(q) || f.regUser[q] != nil {
		return false
	}
	allocatable := false
	for _, reg := range gpAlloc {
		allocatable = allocatable || q == reg
	}
	if !allocatable {
		return false
	}
	deadRemainder := f.remainderLocalUnread(r, after...)
	// The caller already proved the counter independent of every body operand.
	update := words[increment]
	f.truncateNativeCode(start)
	appendWord := func(word uint32) { f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word) }
	move := func(dst, src Reg) { appendWord(0xaa0003e0 | uint32(src)<<16 | uint32(dst)) }
	var exits [3]int
	for phase := 0; phase < 4; phase++ {
		left, right := a, b
		if phase&1 != 0 {
			left, right = b, a
		}
		if phase != 0 {
			exits[phase-1] = f.a.Len()
		}
		appendWord(sf | 0x34000000 | uint32(right))
		appendWord(sf | 0x1ac00800 | uint32(right)<<16 | uint32(left)<<5 | uint32(q))
		appendWord(sf | 0x1b008000 | uint32(right)<<16 | uint32(left)<<10 | uint32(q)<<5 | uint32(left))
	}
	if !deadRemainder {
		move(r, b)
	}
	appendWord(update&^uint32(0xfff<<10) | 4<<10)
	f.patchBranch26(f.a.Branch(), start)
	for phase, site := range exits {
		f.patchBranch19(site, f.a.Len())
		if phase&1 == 0 { // after one or three bodies, roles are exchanged
			if deadRemainder {
				// The taken CBZ follows MSUB W/X, so the old A is fully zero.
				move(a, b)
				move(b, Reg(31))
			} else {
				move(r, a)
				move(a, b)
				move(b, r)
			}
		} else if !deadRemainder {
			move(r, b)
		}
		appendWord(update&^uint32(0xfff<<10) | uint32(phase+1)<<10)
		f.appendFrameEnd(owner, f.a.Branch(), false)
	}
	f.stats.peep("scalar-loop-unroll-pretest")
	f.stats.peep("scalar-loop-unroll-counter")
	f.stats.peep("scalar-loop-unroll-remainder-pair")
	if deadRemainder {
		f.stats.peep("scalar-loop-remainder-dead-local")
	}
	return true
}
