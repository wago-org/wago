//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"
)

func remainderLatchFixture(wide bool) (*fn, *ctrlFrame, Reg) {
	f := gpClassEmitter()
	selection, err := optimizationBindings.ResolveSnapshot(nil, OptimizationSnapshot{}, nil)
	if err != nil {
		panic(err)
	}
	f.policy.Selection = selection
	f.hasLoop = true
	f.pinnedLocalMask = maskOf(X19, X20)
	f.ctrl = []ctrlFrame{{kind: cfLoop, controlSite: 0}}
	sf := uint32(0)
	if wide {
		sf = 1 << 31
	}
	words := []uint32{
		nopWord, nopWord,
		0xaa1303e9, // MOV X9,X19
		0xaa1403f3, // MOV X19,X20
		sf | 0x34000013,
		sf | 0x1ac00800 | 19<<16 | 9<<5 | 11, // UDIV W/X11,W/X9,W/X19
		sf | 0x1b008000 | 19<<16 | 9<<10 | 11<<5 | 10, // MSUB W/X10,W/X11,W/X19,W/X9
		0xaa0a03f4, // MOV X20,X10
	}
	for _, word := range words {
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
	}
	f.sc.trapSites[trapDivZero] = []trapSite{{branch: 16, function: 7, pc: 113}}
	return f, &f.ctrl[0], X20
}

func remainderLatchDirectFixture(wide bool) (*fn, *ctrlFrame, Reg) {
	f, fr, condition := remainderLatchFixture(wide)
	wrWord(f.a.B, 24, rdWord(f.a.B, 24)&^31|20)
	f.a.B = f.a.B[:28]
	return f, fr, condition
}

func TestRemainderLatchUnrollPreservesTrapAndCanonicalExits(t *testing.T) {
	for _, direct := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			fixture := remainderLatchFixture
			if direct {
				fixture = remainderLatchDirectFixture
			}
			f, fr, condition := fixture(wide)
			if !f.unrollRemainderLatch(fr, condition) {
				t.Fatalf("direct=%t wide=%t: fixture rejected", direct, wide)
			}
			if got := f.sc.trapSites[trapDivZero]; len(got) != 1 || got[0].branch != 16 || got[0].function != 7 || got[0].pc != 113 {
				t.Fatalf("direct=%t wide=%t: trap identity changed: %+v", direct, wide, got)
			}
			if len(f.a.B) != 80 {
				t.Fatalf("direct=%t wide=%t: native bytes=%d want 80", direct, wide, len(f.a.B))
			}
			if rdWord(f.a.B, 8) != nopWord || rdWord(f.a.B, 12) != nopWord {
				t.Fatalf("direct=%t wide=%t: source transfers remain", direct, wide)
			}
			if got := rdWord(f.a.B, 16) & 31; got != 20 {
				t.Fatalf("direct=%t wide=%t: first guard register=%d want 20", direct, wide, got)
			}
		}
	}
}

func TestRemainderLatchRejectsUnsafeShapes(t *testing.T) {
	for _, change := range []func(*fn, *ctrlFrame, *Reg){
		func(f *fn, fr *ctrlFrame, c *Reg) { wrWord(f.a.B, 0, 0xd2800020) },
		func(f *fn, fr *ctrlFrame, c *Reg) { wrWord(f.a.B, 16, rdWord(f.a.B, 16)^(1<<24)) },
		func(f *fn, fr *ctrlFrame, c *Reg) { wrWord(f.a.B, 20, rdWord(f.a.B, 20)^(1<<31)) },
		func(f *fn, fr *ctrlFrame, c *Reg) { wrWord(f.a.B, 24, rdWord(f.a.B, 24)^(1<<10)) },
		func(f *fn, fr *ctrlFrame, c *Reg) { wrWord(f.a.B, 28, rdWord(f.a.B, 28)^(1<<16)) },
		func(f *fn, fr *ctrlFrame, c *Reg) { *c = X19 },
		func(f *fn, fr *ctrlFrame, c *Reg) { f.pinnedLocalMask = f.pinnedLocalMask.remove(X19) },
		func(f *fn, fr *ctrlFrame, c *Reg) { f.pinnedLocalMask = f.pinnedLocalMask.add(X11) },
		func(f *fn, fr *ctrlFrame, c *Reg) { f.pinned = f.pinned.add(X11) },
		func(f *fn, fr *ctrlFrame, c *Reg) { f.reserved = f.reserved.add(X11) },
		func(f *fn, fr *ctrlFrame, c *Reg) { f.regUser[X11] = &elem{} },
		func(f *fn, fr *ctrlFrame, c *Reg) { f.sc.trapSites[trapDivZero] = nil },
		func(f *fn, fr *ctrlFrame, c *Reg) { f.sc.trapSites[trapMemOOB] = []trapSite{{branch: 4}} },
		func(f *fn, fr *ctrlFrame, c *Reg) { fr.paramN = 1 },
	} {
		f, fr, condition := remainderLatchFixture(false)
		change(f, fr, &condition)
		original := append([]byte(nil), f.a.B...)
		if f.unrollRemainderLatch(fr, condition) {
			t.Fatal("unsafe shape accepted")
		}
		if string(f.a.B) != string(original) {
			t.Fatal("rejected candidate modified code")
		}
	}
}
