//go:build arm64 && wago_profile

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/internal/jitprofile"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
	plugins "github.com/wago-org/wago/src/core/plugins"
	"testing"
)

func TestProfileCustomSpillSitesCoverEachStoredRegister(t *testing.T) {
	typ, err := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "profile.vector", Size: 64, Carrier: plugins.WasmI32})
	if err != nil {
		t.Fatal(err)
	}
	f := &fn{a: &encoder.Asm{}, s: newStack(), stats: &CodegenStats{RecordSources: true}}
	regs := []Reg{X0, X1, X2, X3}
	e := f.pushValue(storage{kind: stReg, typ: mtCustom, reg: regs[0]})
	f.s.setElemCold(e, &typ, regs)
	for _, r := range regs {
		f.fregUser[r] = e
	}
	f.spillF(e)
	sites := f.profileCodeSites()
	if len(sites) != len(regs) || e.st.kind != stSlot {
		t.Fatal(sites, e.st)
	}
	if err := jitprofile.ValidateCodeSites(sites, uint64(len(f.a.B))); err != nil {
		t.Fatal(err)
	}
	for _, site := range sites {
		if site.Kind != "custom-spill" {
			t.Fatal(site)
		}
		raw := f.a.B[site.Offset : site.Offset+site.Size]
		if len(raw) != 4 || binary.LittleEndian.Uint32(raw)&0xffc003e0 != 0x3d8003e0 {
			t.Fatalf("not a Q stack store: %x", raw)
		}
	}
}

func TestProfileOperandSitesIdentifyActualStackInstructions(t *testing.T) {
	for _, typ := range []machineType{mtI64, mtF64, mtV128} {
		emit := func(enabled bool) ([]byte, []jitprofile.CodeSite) {
			f := &fn{a: &encoder.Asm{}, s: newStack(), stats: &CodegenStats{RecordSources: enabled}}
			f.a.Nop()
			e := f.pushValue(storage{kind: stReg, typ: typ, reg: X0})
			if typ == mtI64 {
				f.regUser[X0] = e
				f.spill(e)
			} else {
				f.fregUser[X0] = e
				f.spillF(e)
			}
			f.a.Nop()
			if typ == mtI64 {
				f.materialize(e)
			} else if typ == mtF64 {
				f.materializeF(e)
			} else {
				f.materializeV128(e)
			}
			f.a.Nop()
			return f.a.B, f.profileCodeSites()
		}
		plain, absent := emit(false)
		code, sites := emit(true)
		if !bytes.Equal(plain, code) || len(absent) != 0 || len(sites) != 2 {
			t.Fatalf("type %v: code neutrality or opt-in failed: %x %x %+v %+v", typ, plain, code, absent, sites)
		}
		if err := jitprofile.ValidateCodeSites(sites, uint64(len(code))); err != nil {
			t.Fatal(err)
		}
		kind := "gp"
		if typ == mtF64 {
			kind = "fp"
		}
		if typ == mtV128 {
			kind = "vector"
		}
		if sites[0].Kind != kind+"-spill" || sites[1].Kind != kind+"-reload" {
			t.Fatal(sites)
		}
		for i, site := range sites {
			raw := code[site.Offset : site.Offset+site.Size]

			if len(raw) != 4 {
				t.Fatalf("expected one load/store: %x", raw)
			}
			word := binary.LittleEndian.Uint32(raw)
			want := uint32(0xf9000000)
			if typ == mtF64 {
				want = 0xfd000000
			}
			if typ == mtV128 {
				want = 0x3d800000
			}
			if i == 1 {
				want |= 0x00400000
			}
			if word&0xffc00000 != want || word>>5&31 != 31 {
				t.Fatalf("site does not identify expected SP load/store: %+v %08x", site, word)
			}

		}
		if _, ok := jitprofile.LookupCodeSite(sites, 0); ok {
			t.Fatal("site leaked into preceding NOP")
		}
		if _, ok := jitprofile.LookupCodeSite(sites, uint64(len(code)-1)); ok {
			t.Fatal("site leaked into trailing NOP")
		}
	}
}
