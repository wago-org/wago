//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func scalarSpanTestWord(code []byte, at int, change func(uint32) uint32) {
	binary.LittleEndian.PutUint32(code[at:], change(binary.LittleEndian.Uint32(code[at:])))
}

func TestScalarMachineFrameSpansDecodeActualTargetBytes(t *testing.T) {
	for _, offset := range []int32{-MaxFrameBytes, -2049, -2048, -8, 0, 8, 16, 2040, 2047, 2048, 4095, 65536, MaxFrameBytes - 8} {
		for _, load := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				t.Run(fmt.Sprintf("offset%d-load%t-wide%t", offset, load, wide), func(t *testing.T) {
					f := target{}
					f.a.Nop() // Coordinates are in the existing target, not rebased.
					start := f.Position()
					if load {
						f.Load(9, offset, wide)
					} else {
						f.Store(offset, 9, wide)
					}
					end := f.Position()
					f.a.Nop()
					before := append([]byte(nil), f.a.B...)
					got, status, ok := decodeScalarFrameSpan(f.a.B, start, end)
					size := 4
					if wide {
						size = 8
					}
					want := scalarFrameTransfer{Offset: offset, Register: 9, Size: size, Load: load}
					if !ok || got != want || status.Reason != regalloccheck.NoFailure || !bytes.Equal(f.a.B, before) {
						t.Fatal("frame span", got, status, ok)
					}
					effect, observed := got.effect()
					if !observed || effect.Kind != regalloccheck.Copy || effect.Size != size {
						t.Fatal("wrong decoded transfer", effect, observed)
					}
					if load {
						if effect.Src != regalloccheck.Slot(offset) || effect.Dst != regalloccheck.Register(regalloccheck.GP, 9) || !wide && effect.ClearTo != 8 {
							t.Fatal(effect)
						}
					} else if effect.Dst != regalloccheck.Slot(offset) || effect.Src != regalloccheck.Register(regalloccheck.GP, 9) {
						t.Fatal(effect)
					}
				})
			}
		}
	}
}

func TestScalarMachineFrameSpanUsesActualSlotAndRegister(t *testing.T) {
	f := target{}
	f.Load(9, 2048, true)
	// The real sequence is LUI, ADDIW, ADD SP, LD. Change the encoded low
	// displacement and destination; no nominal offset/register reaches decode.
	if len(f.a.B) != 16 {
		t.Fatal("fixture recipe changed")
	}
	scalarSpanTestWord(f.a.B, 4, func(w uint32) uint32 { return w&0xfffff | uint32(0x808)<<20 }) // -2048 -> -2040
	scalarSpanTestWord(f.a.B, 12, func(w uint32) uint32 { return w&^(31<<7) | 10<<7 })
	got, status, ok := decodeScalarFrameSpan(f.a.B, 0, len(f.a.B))
	if !ok || got.Offset != 2056 || got.Register != 10 {
		t.Fatal("decoder trusted requested frame transport", got, status)
	}
	store := target{}
	store.Store(4096, 9, true)
	scalarSpanTestWord(store.a.B, len(store.a.B)-4, func(w uint32) uint32 { return w&^(31<<20) | 11<<20 })
	got, status, ok = decodeScalarFrameSpan(store.a.B, 0, len(store.a.B))
	if !ok || got.Register != 11 || got.Load {
		t.Fatal("decoder trusted requested store source", got, status)
	}
	// Address provenance does not survive a changed SP source.
	scalarSpanTestWord(store.a.B, len(store.a.B)-8, func(w uint32) uint32 { return w&^(31<<15) | 10<<15 })
	if _, status, ok := decodeScalarFrameSpan(store.a.B, 0, len(store.a.B)); ok || status.Verdict != regalloccheck.Inconclusive || status.Reason != regalloccheck.UnsupportedOperation {
		t.Fatal("non-SP address authenticated", status, ok)
	}
}

func TestScalarMachineFrameZeroAndPartialHeaderAccesses(t *testing.T) {
	for _, load := range []bool{false, true} {
		var a enc.Asm
		if load {
			a.Lbu(enc.Zero, enc.SP, 15)
		} else {
			a.Sb(enc.Zero, enc.SP, 15)
		}
		got, status, ok := decodeScalarFrameSpan(a.B, 0, len(a.B))
		if !ok || got.Offset != 15 || got.Size != 1 || got.Load != load {
			t.Fatal(got, status)
		}
		effect, observed := got.effect()
		if load && observed || !load && (!observed || effect.Kind != regalloccheck.Kill || effect.Dst != regalloccheck.Slot(15) || effect.Size != 1) {
			t.Fatal("x0/partial transfer", effect, observed)
		}
	}
	// Decoding a header write reports the actual bytes. It does not certify
	// preservation of the return pointer; a later whole-body policy must reject.
	var a enc.Asm
	a.Sw(enc.X9, enc.SP, 12)
	got, _, ok := decodeScalarFrameSpan(a.B, 0, len(a.B))
	if !ok || got.Offset != 12 || got.Size != 4 || got.Load {
		t.Fatal("header overlap hidden", got)
	}
}

func TestScalarMachineAddressConstantsFollowRVSemantics(t *testing.T) {
	for _, value := range []int64{math.MinInt64, math.MinInt64 + 1, -0x100000001, -2147483649, math.MinInt32, -131072, -2049, -2048, -1, 0, 1, 2047, 2048, 131071, math.MaxInt32, 2147483648, 0x100000001, math.MaxInt64} {
		var a enc.Asm
		a.Constant64(enc.Scratch1, value)
		got, ok := decodeScalarAddressConstant(a.B)
		if !ok || got != uint64(value) {
			t.Fatalf("constant %x decoded %x/%t", value, got, ok)
		}
	}
}

func TestScalarMachineFrameUnknownAndBoundedSpans(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source func() []byte
		reason regalloccheck.FailureReason
	}{
		{"dynamic", func() []byte { var a enc.Asm; a.Ld(enc.X9, enc.X10, 0); return a.B }, regalloccheck.UnsupportedOperation},
		{"wrong-scratch", func() []byte {
			var a enc.Asm
			a.Constant64(enc.X30, 2048)
			a.Add(enc.X31, enc.SP, enc.X30)
			a.Ld(enc.X9, enc.X31, 0)
			return a.B
		}, regalloccheck.UnsupportedOperation},
		{"address-clobber", func() []byte {
			var a enc.Asm
			a.Constant64(enc.X31, 2048)
			a.Add(enc.X31, enc.SP, enc.X31)
			a.Addi(enc.X31, enc.Zero, 0)
			a.Ld(enc.X9, enc.X31, 0)
			return a.B
		}, regalloccheck.UnsupportedOperation},
		{"far-coordinate", func() []byte {
			var a enc.Asm
			a.Constant64(enc.X31, math.MaxInt64)
			a.Add(enc.X31, enc.SP, enc.X31)
			a.Ld(enc.X9, enc.X31, 2047)
			return a.B
		}, regalloccheck.UnsupportedOperation},
		{"too-many-words", func() []byte { return make([]byte, (scalarFrameSpanWords+1)*4) }, regalloccheck.ResourceLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.source()
			if _, status, ok := decodeScalarFrameSpan(b, 0, len(b)); ok || status.Verdict != regalloccheck.Inconclusive || status.Reason != tc.reason {
				t.Fatal(status, ok)
			}
		})
	}
	var a enc.Asm
	a.Ld(enc.X9, enc.SP, 16)
	for _, span := range [][2]int{{-1, 4}, {0, 0}, {0, 3}, {1, 4}, {4, 0}, {0, 8}, {math.MaxInt, math.MaxInt}} {
		if _, status, ok := decodeScalarFrameSpan(a.B, span[0], span[1]); ok || status.Verdict != regalloccheck.Inconclusive {
			t.Fatal("invalid span accepted", span, status)
		}
	}
}

func TestScalarMachineLongEnvelopesDecodePatchedBytes(t *testing.T) {
	for _, conditional := range []bool{false, true} {
		for _, delta := range []int64{-2147485696, -4096, -2052, -2048, -4, 0, 4, 2044, 2048, 2052, 4096, 2147481596} {
			var a enc.Asm
			// An unrelated preceding short branch cannot classify the next jump.
			a.Branch(enc.NE, enc.X5, enc.X6, 12)
			start, site := a.Len(), 0
			if conditional {
				site = a.BranchLong(enc.LTU, enc.X9, enc.X10)
			} else {
				site = a.JumpLong(false)
			}
			end := a.Len()
			want := int64(site) + delta
			if err := a.PatchLong(site, want); err != nil {
				t.Fatal(err)
			}
			before := append([]byte(nil), a.B...)
			got, kind, status, ok := decodeScalarLongEnvelope(a.B, start, site, end)
			if !ok || got != want || kind != conditional || status.Reason != regalloccheck.NoFailure || !bytes.Equal(before, a.B) {
				t.Fatal("long decode", got, want, kind, status)
			}
		}
	}
}

func TestScalarMachineLongEnvelopeKindAndCorruption(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mutate      func([]byte, int)
		verdict     regalloccheck.Verdict
		decoded     bool
		conditional bool
	}{
		{"nop-is-unconditional", func(b []byte, site int) { binary.LittleEndian.PutUint32(b[site-4:], 0x13) }, regalloccheck.Inconclusive, true, false},
		{"call-link", func(b []byte, site int) { scalarSpanTestWord(b, site+4, func(w uint32) uint32 { return w | 1<<7 }) }, regalloccheck.Rejected, false, false},
		{"wrong-address-register", func(b []byte, site int) {
			scalarSpanTestWord(b, site+4, func(w uint32) uint32 { return w&^(31<<15) | 30<<15 })
		}, regalloccheck.Rejected, false, false},
		{"wrong-skip", func(b []byte, site int) { var a enc.Asm; a.Branch(enc.NE, enc.X9, enc.X10, 8); copy(b[site-4:], a.B) }, regalloccheck.Rejected, false, false},
		{"unknown-high", func(b []byte, site int) { binary.LittleEndian.PutUint32(b[site:], 0x13) }, regalloccheck.Inconclusive, false, false},
		{"unknown-short", func(b []byte, site int) { binary.LittleEndian.PutUint32(b[site-4:], 0xffffffff) }, regalloccheck.Inconclusive, false, false},
		{"halfword-target", func(b []byte, site int) { scalarSpanTestWord(b, site+4, func(w uint32) uint32 { return w | 2<<20 }) }, regalloccheck.Rejected, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a enc.Asm
			site := a.BranchLong(enc.EQ, enc.X9, enc.X10)
			if err := a.PatchLong(site, 64); err != nil {
				t.Fatal(err)
			}
			tc.mutate(a.B, site)
			_, kind, status, ok := decodeScalarLongEnvelope(a.B, 0, site, len(a.B))
			if ok != tc.decoded || ok && kind != tc.conditional || !ok && status.Verdict != tc.verdict {
				t.Fatal("corrupt envelope", kind, status, ok)
			}
		})
	}
	var a enc.Asm
	site := a.JumpLong(false)
	for _, span := range [][3]int{{-1, site, 8}, {0, site, 4}, {0, site, 12}, {0, 4, 8}, {1, site, 8}, {0, math.MaxInt, 8}} {
		if _, _, status, ok := decodeScalarLongEnvelope(a.B, span[0], span[1], span[2]); ok || status.Verdict != regalloccheck.Inconclusive {
			t.Fatal("bad envelope extent", span, status)
		}
	}
}

func TestScalarMachineSpanDecodersAllocateNoBacking(t *testing.T) {
	f := target{}
	f.Load(9, 65536, true)
	var a enc.Asm
	site := a.BranchLong(enc.EQ, enc.X9, enc.X10)
	if err := a.PatchLong(site, int64(a.Len())); err != nil {
		t.Fatal(err)
	}
	if got := testing.AllocsPerRun(10, func() {
		if _, _, ok := decodeScalarFrameSpan(f.a.B, 0, len(f.a.B)); !ok {
			panic("frame span")
		}
		if _, _, _, ok := decodeScalarLongEnvelope(a.B, 0, site, len(a.B)); !ok {
			panic("long span")
		}
	}); got != 0 {
		t.Fatal("bounded decoder allocated", got)
	}
}
