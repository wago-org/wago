//go:build amd64 && (wago_gc_compact_range_sse2 || wago_gc_compact_weighted_sum)

package amd64

import (
	"strings"
	"testing"
)

func TestCompactRangeSSE2Emitter(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		sc := newScratch()
		f := fn{a: sc.asm, sc: sc, s: sc.stack}
		f.rangeBound = 32
		if diagnosticsEnabled {
			f.stats = &CodegenStats{}
		}
		if blocked {
			for r := Reg(0); r < 16; r++ {
				f.fpinned = f.fpinned.add(r)
			}
		}
		pins := f.fpinned
		done := f.emitCompactRangeSSE2(directLoopLease{object: R12, length: R13}, R14, RAX, RCX, 4, 8)
		if blocked {
			if done != -1 || len(f.a.B) != 0 {
				t.Fatal("no-free-XMM fallback emitted code")
			}
		} else {
			if done < 0 {
				t.Fatal("missing vector path")
			}
			f.a.PatchRel32(done, f.a.Len())
			listing := disasm(t, f.a.B)
			if !strings.Contains(listing, "movdqu") || strings.Contains(listing, "vmovdqu") {
				t.Fatal("not baseline SSE2", listing)
			}
		}
		if pins != f.fpinned {
			t.Fatal("XMM pins changed")
		}
	}
}
