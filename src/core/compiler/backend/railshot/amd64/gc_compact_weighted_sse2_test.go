//go:build amd64 && wago_gc_compact_weighted_sse2

package amd64

import (
	"strings"
	"testing"
)

func TestCompactWeightedSSE2Emitter(t *testing.T) {
	for _, free := range []int{0, 5, 6, 16} {
		sc := newScratch()
		f := fn{a: sc.asm, sc: sc, s: sc.stack}
		f.weightedBound = 32
		f.weightedWeight = -7
		if diagnosticsEnabled {
			f.stats = &CodegenStats{}
		}
		for r := Reg(free); r < 16; r++ {
			f.fpinned = f.fpinned.add(r)
		}
		pins := f.fpinned
		done := f.emitCompactWeightedSSE2(directLoopLease{object: R12, length: R13}, R14, R15, RAX, RCX)
		if free < 6 {
			if done != -1 || len(f.a.B) != 0 {
				t.Fatal("pressure fallback emitted fragment")
			}
		} else {
			if done < 0 {
				t.Fatal("missing SIMD")
			}
			f.a.PatchRel32(done, f.a.Len())
			s := disasm(t, f.a.B)
			if strings.Count(s, "pmuludq") != 2 || !strings.Contains(s, "paddd") || strings.Contains(s, "vpmul") || strings.Contains(s, "pmulld") {
				t.Fatal("not SSE2", s)
			}
		}
		if f.fpinned != pins {
			t.Fatal("pins changed")
		}
	}
}
