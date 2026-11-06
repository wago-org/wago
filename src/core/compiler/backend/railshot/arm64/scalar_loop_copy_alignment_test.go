//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	"testing"
)

func TestPaddedScalarBranchCopiesPreserveEdgesAndTraps(t *testing.T) {
	if profileEnabled {
		t.Skip("unrolling is disabled with source profiling")
	}
	for start := 0; start < 16; start += 4 {
		for words := 10; words < 14; words++ {
			for _, backedge := range []uint32{0x17fffffa, 0x54ffff41, 0x34ffff47, 0xb5ffff47} {
				f := branchLoopProofFixture()
				body := append([]byte(nil), f.a.B...)
				for len(body) < words*4 {
					body = binary.LittleEndian.AppendUint32(body, nopWord)
				}
				// A body-end edge enters only NOP padding before the next copy.
				wrWord(body, 24, 0x14000000|uint32(words-6))
				wrWord(body, 32, backedge)
				wrWord(body, 36, 0x34000001) // unresolved division-by-zero CBZ W1
				f.a.B = nil
				for len(f.a.B) < start {
					f.a.B = binary.LittleEndian.AppendUint32(f.a.B, nopWord)
				}
				f.a.B = append(f.a.B, body...)
				f.ctrl[0] = ctrlFrame{kind: cfBlock}
				f.ctrl[1].controlSite = start
				f.sc.ctrlMerges = nil
				f.appendFrameEnd(&f.ctrl[0], start+4, true)
				f.sc.trapSites[trapDivZero] = []trapSite{{branch: uint32(start + 36), function: 7, pc: 113}}
				if !f.unrollScalarBranches(&f.ctrl[1]) {
					t.Fatalf("rejected start=%d words=%d backedge=%08x", start, words, backedge)
				}
				stride := (words + 3) &^ 3
				stride *= 4
				if len(f.a.B) != start+4*stride+4 {
					t.Fatalf("start=%d words=%d bytes=%d want=%d", start, words, len(f.a.B), start+4*stride+4)
				}
				traps := f.sc.trapSites[trapDivZero]
				if len(traps) != 4 {
					t.Fatalf("trap copies=%d want=4", len(traps))
				}
				for phase := 0; phase < 4; phase++ {
					base := start + phase*stride
					for _, edge := range [][2]int{{8, 20}, {16, 28}, {24, words * 4}, {32, 8}} {
						site := base + edge[0]
						target, ok := branchTarget(site, rdWord(f.a.B, site))
						if !ok || target != base+edge[1] {
							t.Fatalf("start=%d words=%d phase=%d site=%d target=%d want=%d", start, words, phase, site, target, base+edge[1])
						}
					}
					if (base+8)%16 != (start+8)%16 {
						t.Fatal("copied inner loop changed fetch phase")
					}
					for at := base + words*4; at < base+stride; at += 4 {
						if rdWord(f.a.B, at) != nopWord {
							t.Fatalf("padding at %d has an effect", at)
						}
					}
					trap := traps[phase]
					if trap.branch != uint32(base+36) || trap.function != 7 || trap.pc != 113 {
						t.Fatalf("trap identity changed: %+v", trap)
					}
				}
				first, second, overflow := f.frameEndSites(&f.ctrl[0])
				f.patchFrameEndSites(first, second, overflow)
				for phase := 0; phase < 4; phase++ {
					site := start + phase*stride + 4
					if target, ok := branchTarget(site, rdWord(f.a.B, site)); !ok || target != len(f.a.B) {
						t.Fatalf("owned exit at %d targets %d want=%d", site, target, len(f.a.B))
					}
				}
				if target, ok := branchTarget(len(f.a.B)-4, rdWord(f.a.B, len(f.a.B)-4)); !ok || target != start {
					t.Fatalf("group backedge targets %d want=%d", target, start)
				}
			}
		}
	}
}
