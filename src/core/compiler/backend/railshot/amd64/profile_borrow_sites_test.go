//go:build amd64 && wago_profile

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestProfileBorrowProtectionCopies(t *testing.T) {
	for _, typ := range []machineType{mtF32, mtF64, mtV128} {
		emit := func(enabled bool) ([]byte, []jitprofile.CodeSite) {
			f := &fn{a: &encoder.Asm{}, s: newStack(), stats: &CodegenStats{RecordSources: enabled}}
			f.fpinnedLocalMask = maskOf(RBX)
			f.a.B = append(f.a.B, 0x90)
			e := f.pushValue(storage{kind: stLocalReg, typ: typ, reg: RBX})
			var dst Reg
			if typ == mtV128 {
				dst = f.materializeV128(e)
			} else {
				dst = f.materializeF(e)
			}
			if dst == RBX || e.st.kind != stReg || e.st.reg != dst {
				t.Fatalf("type %v: no owned copy: %+v dst=%v", typ, e.st, dst)
			}
			if f.stats.Spills != 0 || f.stats.Reloads != 0 {
				t.Fatal("register copy counted as stack traffic")
			}
			f.a.B = append(f.a.B, 0x90)
			return f.a.B, f.profileCodeSites()
		}
		ordinary, absent := emit(false)
		profiled, sites := emit(true)
		if !bytes.Equal(ordinary, profiled) || len(absent) != 0 || len(sites) != 1 {
			t.Fatalf("type %v: opt-in or byte identity failed: %x %x %+v %+v", typ, ordinary, profiled, absent, sites)
		}
		if err := jitprofile.ValidateCodeSites(sites, uint64(len(profiled))); err != nil {
			t.Fatal(err)
		}
		kind := "fp-borrow-copy"
		if typ == mtV128 {
			kind = "vector-borrow-copy"
		}
		site := sites[0]
		if site.Kind != kind || site.Offset != 1 || int(site.Size) != len(profiled)-2 {
			t.Fatalf("type %v: %+v", typ, site)
		}
		// SSE scalar/vector register moves must have a register source (mod=11),
		// the borrowed XMM3 source, and the owned XMM0 destination.
		raw := profiled[site.Offset : site.Offset+site.Size]
		if raw[len(raw)-1] != 0xc3 {
			t.Fatalf("type %v: not an XMM3 to XMM0 register copy: %x", typ, raw)
		}
	}
}
