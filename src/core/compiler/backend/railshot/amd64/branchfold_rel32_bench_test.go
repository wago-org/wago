//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	amd64enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

// BenchmarkBranchFoldRel32Sites measures the post-assembly fold with a bounded
// relocation inventory. Each pair has two retained rel32 fields; folding removes
// the JMP field while keeping the Jcc field for the native finalizer.
func BenchmarkBranchFoldRel32Sites(b *testing.B) {
	for _, pairs := range []int{128, 512, 2048} {
		b.Run(fmt.Sprintf("pairs=%d", pairs), func(b *testing.B) {
			a := &amd64enc.Asm{Rel32SiteLimit: pairs * 2}
			sc := &scratch{brFoldSites: make([]int, 0, pairs)}
			for range pairs {
				over := a.JccPlaceholder(amd64enc.CondNE)
				jump := a.JmpPlaceholder()
				a.PatchRel32(jump, 0)
				a.PatchRel32(over, a.Len())
				sc.brFoldSites = append(sc.brFoldSites, over)
			}
			originalCode := append([]byte(nil), a.B...)
			originalSites := append([]amd64enc.Rel32Site(nil), a.Rel32Sites...)
			f := fn{a: a, sc: sc, policy: currentCodegenPolicy()}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				copy(a.B, originalCode)
				a.Rel32Sites = a.Rel32Sites[:len(originalSites)]
				copy(a.Rel32Sites, originalSites)
				f.finalizeBranchFolds()
			}
		})
	}
}

// TestBranchFoldRel32Retention keeps emitted recorder order even when the
// displacement fields were patched in a different order from their code sites.
func TestBranchFoldRel32Retention(t *testing.T) {
	a := &amd64enc.Asm{Rel32SiteLimit: 6}
	a.B = append(a.B, 0x90) // common branch target
	sc := &scratch{}
	var overs, jumps []int
	for i := 0; i < 3; i++ {
		over := a.JccPlaceholder(amd64enc.CondNE)
		jump := a.JmpPlaceholder()
		a.PatchRel32(jump, 0)
		target := a.Len()
		if i == 1 {
			target++ // stale candidate: Jcc does not skip exactly the JMP
		}
		a.PatchRel32(over, target)
		sc.brFoldSites = append(sc.brFoldSites, over)
		overs = append(overs, over)
		jumps = append(jumps, jump)
	}
	stale := append([]byte(nil), a.B[overs[1]-2:overs[1]+9]...)
	f := fn{a: a, sc: sc, policy: currentCodegenPolicy()}
	f.finalizeBranchFolds()
	if a.Rel32Overflow || a.Rel32Count != 6 {
		t.Fatalf("recorder state: overflow=%v count=%d", a.Rel32Overflow, a.Rel32Count)
	}
	want := []int{overs[0], jumps[1], overs[1], overs[2]}
	if len(a.Rel32Sites) != len(want) {
		t.Fatalf("retained sites = %d, want %d", len(a.Rel32Sites), len(want))
	}
	for i, site := range a.Rel32Sites {
		if site.At() != want[i] {
			t.Errorf("retained site %d = %d, want %d", i, site.At(), want[i])
		}
	}
	for _, over := range []int{overs[0], overs[2]} {
		if a.B[over-1] != 0x84 || int32(binary.LittleEndian.Uint32(a.B[over:])) != int32(-(over+4)) {
			t.Errorf("folded Jcc at %d does not target byte zero", over)
		}
		if !bytes.Equal(a.B[over+4:over+9], []byte{0x0f, 0x1f, 0x44, 0, 0}) {
			t.Errorf("folded JMP at %d was not replaced with NOP", over+4)
		}
	}
	if !bytes.Equal(a.B[overs[1]-2:overs[1]+9], stale) {
		t.Fatal("stale branch candidate changed")
	}
}

func TestBranchFoldRel32Overflow(t *testing.T) {
	a := &amd64enc.Asm{Rel32SiteLimit: 1}
	over := a.JccPlaceholder(amd64enc.CondNE)
	jump := a.JmpPlaceholder()
	a.PatchRel32(jump, 0)
	a.PatchRel32(over, a.Len()) // overflows the recorder
	f := fn{a: a, sc: &scratch{brFoldSites: []int{over}}, policy: currentCodegenPolicy()}
	f.finalizeBranchFolds()
	if !a.Rel32Overflow || a.Rel32Count != 2 || len(a.Rel32Sites) != 0 {
		t.Fatalf("overflow recorder state: overflow=%v count=%d sites=%v", a.Rel32Overflow, a.Rel32Count, a.Rel32Sites)
	}
	if a.B[over-1] != 0x84 || !bytes.Equal(a.B[over+4:over+9], []byte{0x0f, 0x1f, 0x44, 0, 0}) {
		t.Fatal("overflow changed branch fold behavior")
	}
}
