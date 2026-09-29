//go:build amd64 && wago_profile

package amd64

import (
	"bytes"

	"github.com/wago-org/wago/internal/jitprofile"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	plugins "github.com/wago-org/wago/src/core/plugins"
	"testing"
)

func TestProfileLocalFrameTransfersAreSeparateFromOperandSpills(t *testing.T) {
	for _, typ := range []machineType{mtI32, mtI64} {
		emit := func(enabled bool) ([]byte, []jitprofile.CodeSite) {
			f := &fn{a: &encoder.Asm{}, stats: &CodegenStats{RecordSources: enabled}}
			f.a.B = append(f.a.B, 0x90)
			f.loadFrameInt(R12, 24, typ)
			f.a.B = append(f.a.B, 0x90)
			f.storeFrameInt(24, R12, typ)
			f.a.B = append(f.a.B, 0x90)
			if f.stats.Spills != 0 || f.stats.Reloads != 0 {
				t.Fatal("local transport counted as operand spills", f.stats)
			}
			return f.a.B, f.profileCodeSites()
		}
		plain, absent := emit(false)
		code, sites := emit(true)
		if !bytes.Equal(plain, code) || len(absent) != 0 || len(sites) != 2 || sites[0].Kind != "gp-local-load" || sites[1].Kind != "gp-local-store" {
			t.Fatal(typ, sites, absent)
		}
		if err := jitprofile.ValidateCodeSites(sites, uint64(len(code))); err != nil {
			t.Fatal(err)
		}
		for i, site := range sites {
			want := &encoder.Asm{}
			if i == 0 {
				if typ == mtI32 {
					want.Load32(R12, RSP, 24)
				} else {
					want.Load64(R12, RSP, 24)
				}
			} else if typ == mtI32 {
				want.Store32(RSP, 24, R12)
			} else {
				want.Store64(RSP, 24, R12)
			}
			if !bytes.Equal(code[site.Offset:site.Offset+site.Size], want.B) {
				t.Fatal("site does not cover exactly one local transfer", site)
			}
		}
		if _, ok := jitprofile.LookupCodeSite(sites, uint64(len(code)-1)); ok {
			t.Fatal("site covers trailing NOP")
		}
	}
}

func TestProfileCustomSpillSitesCoverEachStoredRegister(t *testing.T) {
	typ, err := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "profile.vector", Size: 64, Carrier: plugins.WasmI32})
	if err != nil {
		t.Fatal(err)
	}
	f := &fn{a: &encoder.Asm{}, s: newStack(), stats: &CodegenStats{RecordSources: true}}
	regs := []Reg{RAX, RCX}
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
		if len(raw) < 6 || raw[0] != 0xc4 || raw[2]&4 == 0 || raw[3] != 0x7f || raw[4]&7 != 4 || raw[5]&7 != 4 {
			t.Fatalf("not a YMM stack store: %x", raw)
		}
	}
}

func TestProfileOperandSitesIdentifyActualStackInstructions(t *testing.T) {
	for _, typ := range []machineType{mtI64, mtF64, mtV128} {
		emit := func(enabled bool) ([]byte, []jitprofile.CodeSite) {
			f := &fn{a: &encoder.Asm{}, s: newStack(), stats: &CodegenStats{RecordSources: enabled}}
			f.a.B = append(f.a.B, 0x90)
			e := f.pushValue(storage{kind: stReg, typ: typ, reg: RAX})
			if typ == mtI64 {
				f.regUser[RAX] = e
				f.spill(e)
			} else {
				f.fregUser[RAX] = e
				f.spillF(e)
			}
			f.a.B = append(f.a.B, 0x90)
			if typ == mtI64 {
				f.materialize(e)
			} else if typ == mtF64 {
				f.materializeF(e)
			} else {
				f.materializeV128(e)
			}
			f.a.B = append(f.a.B, 0x90)
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

			at := 0
			for at < len(raw) && (raw[at] == 0x66 || raw[at] == 0xf2 || raw[at] == 0xf3 || raw[at]&0xf0 == 0x40) {
				at++
			}
			want := byte(0x89)
			if i == 1 {
				want = 0x8b
			}
			if typ != mtI64 {
				switch raw[at] {
				case 0x0f:
					at++
				case 0xc4:
					if raw[at+1]&31 != 1 {
						t.Fatalf("unexpected VEX opcode map: %x", raw)
					}
					at += 3
				case 0xc5:
					at += 2
				default:
					t.Fatalf("not an SSE/VEX memory instruction: %x", raw)
				}
				want = 0x11
				if i == 1 {
					want = 0x10
				}
				if typ == mtV128 {
					want = 0x7f
					if i == 1 {
						want = 0x6f
					}
				}
			}
			if raw[at] != want || raw[at+1]&7 != 4 || raw[at+2]&7 != 4 {
				t.Fatalf("site does not identify expected stack memory instruction: %+v %x", site, raw)
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
