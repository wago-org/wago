//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"
)

func remainderPairWords(wide bool) []uint32 {
	sf := uint32(0)
	if wide {
		sf = 1 << 31
	}
	return []uint32{sf | 0x34000002, sf | 0x1ac20829, sf | 0x1b028523,
		0xaa0203e1, 0xaa0303e2, sf | 0x110004a5}
}

func TestRemainderPairCopiesStayOffHotPath(t *testing.T) {
	for _, wide := range []bool{false, true} {
		f := gpClassEmitter()
		f.pinnedLocalMask = maskOf(X1, X2, X3, X5)
		f.ctrl = []ctrlFrame{{kind: cfBlock}}
		words := remainderPairWords(wide)
		for _, word := range words {
			f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
		}
		if !f.unrollScalarCounter(0, words, 0, &f.ctrl[0]) {
			t.Fatal("fixture rejected")
		}
		moves := 0
		for pc := 0; pc < len(f.a.B); pc += 4 {
			word := rdWord(f.a.B, pc)
			if word&0xfc000000 == 0x14000000 {
				break
			}
			if _, _, ok := coldLoopMove64(word); ok {
				moves++
			}
		}
		if moves != 1 {
			t.Fatalf("wide=%t: hot transfer copies=%d want one group-end restore", wide, moves)
		}
	}
}

func TestRemainderPairRejectsUnsafeShapes(t *testing.T) {
	for _, change := range []func(*fn, []uint32){
		func(f *fn, w []uint32) { w[0] ^= 1 << 24 }, // CBNZ is not a nonzero-divisor guard
		func(f *fn, w []uint32) { w[0] ^= 1 << 31 },
		func(f *fn, w []uint32) { w[1] ^= 1 << 31 },
		func(f *fn, w []uint32) { w[2] ^= 1 << 31 },
		func(f *fn, w []uint32) { w[2] ^= 1 << 10 }, // accumulator must be the dividend
		func(f *fn, w []uint32) { w[3] ^= 1 << 31 }, // preserve full-width local transfers
		func(f *fn, w []uint32) { w[4] ^= 1 << 16 },
		func(f *fn, w []uint32) { w[2] = w[2]&^31 | 1; w[4] = 0xaa0103e2 }, // aliased remainder
		func(f *fn, w []uint32) { f.pinnedLocalMask = f.pinnedLocalMask.remove(X3) },
		func(f *fn, w []uint32) { f.pinnedLocalMask = f.pinnedLocalMask.add(X9) },
		func(f *fn, w []uint32) { f.pinned = f.pinned.add(X9) },
		func(f *fn, w []uint32) { f.reserved = f.reserved.add(X9) },
		func(f *fn, w []uint32) { f.regUser[X9] = &elem{} },
	} {
		f := gpClassEmitter()
		f.pinnedLocalMask = maskOf(X1, X2, X3, X5)
		words := remainderPairWords(false)
		change(f, words)
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, nopWord)
		if f.unrollRemainderPair(0, words, 0, 5, &ctrlFrame{kind: cfBlock}) {
			t.Fatalf("unsafe shape accepted: %x", words)
		}
		if len(f.a.B) != 4 || rdWord(f.a.B, 0) != nopWord {
			t.Fatal("fallback modified code")
		}
	}
}
