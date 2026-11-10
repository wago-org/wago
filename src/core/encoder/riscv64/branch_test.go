package riscv64

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

func decodedLongTarget(code []byte, site int) int64 {
	high := int64(int32(binary.LittleEndian.Uint32(code[site:]) & 0xfffff000))
	low := int64(int32(binary.LittleEndian.Uint32(code[site+4:])) >> 20)
	return (int64(site) + high + low) &^ 1 // JALR clears bit zero.
}

func decodedBranch(w uint32) int32 {
	u := (w>>31)<<12 | ((w>>7)&1)<<11 | ((w>>25)&63)<<5 | ((w>>8)&15)<<1
	return int32(u<<19) >> 19
}

func TestLongFixupBoundaries(t *testing.T) {
	deltas := []int64{-2147485696, -2147483648, -1048576, -4096, -2052, -2048, -2044,
		-4, 0, 4, 2044, 2048, 2052, 4092, 4096, 1048576, 2147481596}
	rng := rand.New(rand.NewSource(64))
	for i := 0; i < 4096; i++ {
		delta := int64(int32(rng.Uint32())) &^ 3
		if delta <= 2147481596 {
			deltas = append(deltas, delta)
		}
	}
	for _, call := range []bool{false, true} {
		var a Asm
		a.Nop()
		site := a.JumpLong(call)
		if site != 4 || a.Len() != 12 {
			t.Fatalf("site=%d len=%d", site, a.Len())
		}
		for _, delta := range deltas {
			target := int64(site) + delta
			if err := a.PatchLong(site, target); err != nil {
				t.Fatalf("delta=%d: %v", delta, err)
			}
			if got := decodedLongTarget(a.B, site); got != target || a.Len() != 12 {
				t.Fatalf("delta=%d got=%d want=%d len=%d", delta, got, target, a.Len())
			}
			link := (binary.LittleEndian.Uint32(a.B[site+4:]) >> 7) & 31
			if (link == uint32(RA)) != call || link != 0 && link != 1 {
				t.Fatal("fixup changed link register")
			}
		}
	}
}

func TestFixupErrorsAreTransactional(t *testing.T) {
	var a Asm
	a.Nop()
	site := a.JumpLong(false)
	if err := a.PatchLong(site, 4096); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), a.B...)
	for _, target := range []int64{-2147485700 + 4, 2147481600 + 4, math.MinInt64, math.MaxInt64, 2, -2} {
		if err := a.PatchLong(site, target); err == nil || !bytes.Equal(a.B, before) {
			t.Fatalf("target=%d: err=%v mutated=%t", target, err, !bytes.Equal(a.B, before))
		}
	}
	for _, badSite := range []int{-4, -1, 0, 2, 8, 12, math.MaxInt} {
		if err := a.PatchLong(badSite, 0); err == nil || !bytes.Equal(a.B, before) {
			t.Fatalf("bad site %d: %v", badSite, err)
		}
	}
	for _, pos := range []int{site, site + 4} {
		a.B[pos] ^= 4 // Corrupt opcode, retain immediates.
		corrupt := append([]byte(nil), a.B...)
		if err := a.PatchLong(site, 0); err == nil || !bytes.Equal(a.B, corrupt) {
			t.Fatalf("corrupt envelope at %d: %v", pos, err)
		}
		copy(a.B, before)
	}
	for _, c := range []struct {
		pos  int
		bits uint32
	}{
		{site, 1 << 7},      // AUIPC destination x31 -> x30.
		{site + 4, 1 << 15}, // JALR base x31 -> x30.
		{site + 4, 1 << 12}, // Invalid JALR funct3.
		{site + 4, 2 << 7},  // Link x2 is not an emitted jump/call envelope.
	} {
		w := binary.LittleEndian.Uint32(a.B[c.pos:])
		binary.LittleEndian.PutUint32(a.B[c.pos:], w^c.bits)
		corrupt := append([]byte(nil), a.B...)
		if err := a.PatchLong(site, 0); err == nil || !bytes.Equal(a.B, corrupt) {
			t.Fatalf("corrupt register/function field at %d: %v", c.pos, err)
		}
		copy(a.B, before)
	}
	a.Addi(X5, X6, 2048)
	if err := a.PatchLong(site, 0); err != a.Err() || !bytes.Equal(a.B, before) {
		t.Fatal("fixup accepted image with emission error")
	}
	for n := 0; n < 8; n++ {
		a := Asm{B: make([]byte, n)}
		if err := a.PatchLong(0, 0); err == nil {
			t.Fatalf("short image %d accepted", n)
		}
	}
}

func TestConditionalLongEnvelopeAndRelocation(t *testing.T) {
	for _, cond := range []Cond{EQ, NE, LT, GE, LTU, GEU} {
		var worker Asm
		worker.Nop()
		site := worker.BranchLong(cond, X5, X6)
		if site != 8 || worker.Len() != 16 {
			t.Fatalf("conditional site=%d len=%d", site, worker.Len())
		}
		w := binary.LittleEndian.Uint32(worker.B[site-4:])
		if decodedBranch(w) != 12 || Cond((w>>12)&7) != cond^1 || (w>>15)&31 != 5 || (w>>20)&31 != 6 {
			t.Fatalf("incorrect conditional envelope %#08x", w)
		}
		target := int64(0x1808)
		if err := worker.PatchLong(site, target); err != nil {
			t.Fatal(err)
		}
		var image Asm
		for i := 0; i < 137; i++ {
			image.Nop()
		}
		prefix := image.Len()
		image.B = append(image.B, worker.B...)
		relocatedSite, relocatedTarget := prefix+site, int64(prefix)+target
		if err := image.PatchLong(relocatedSite, relocatedTarget); err != nil {
			t.Fatal(err)
		}
		if got := decodedLongTarget(image.B, relocatedSite); got != relocatedTarget {
			t.Fatalf("relocated target %d != %d", got, relocatedTarget)
		}
		if !bytes.Equal(image.B[prefix:], worker.B) {
			t.Fatal("uniform worker relocation changed PC-relative bytes")
		}
		// Moving only the destination changes the displacement and must repatch
		// both high and low fields, including their carry boundary.
		if err := image.PatchLong(relocatedSite, relocatedTarget+4096); err != nil {
			t.Fatal(err)
		}
		if got := decodedLongTarget(image.B, relocatedSite); got != relocatedTarget+4096 {
			t.Fatal("cross-worker target adjustment failed")
		}
	}
}

func TestAllShortBranchDisplacements(t *testing.T) {
	for delta := int32(-4096); delta <= 4092; delta += 4 {
		var a Asm
		a.Branch(EQ, X31, X30, delta)
		if a.Err() != nil || decodedBranch(binary.LittleEndian.Uint32(a.B)) != delta {
			t.Fatalf("branch delta=%d err=%v", delta, a.Err())
		}
	}
}

func TestJalDisplacementProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(21))
	deltas := []int32{-1048576, -524288, -4096, -2048, -4, 0, 4, 2048, 4096, 524288, 1048572}
	for bit := 2; bit < 20; bit++ {
		deltas = append(deltas, 1<<bit, -(1 << bit))
	}
	for i := 0; i < 4096; i++ {
		deltas = append(deltas, int32(rng.Intn(1<<19)-(1<<18))*4)
	}
	for _, link := range []Reg{Zero, RA, X31} {
		for _, delta := range deltas {
			var a Asm
			a.Jal(link, delta)
			w := binary.LittleEndian.Uint32(a.B)
			// Decode the scattered J-type immediate independently of the encoder.
			u := (w>>31)<<20 | ((w>>12)&255)<<12 | ((w>>20)&1)<<11 | ((w>>21)&1023)<<1
			got := int32(u<<11) >> 11
			if a.Err() != nil || got != delta || Reg((w>>7)&31) != link || w&127 != 0x6f {
				t.Fatalf("JAL delta=%d got=%d link=%d code=%x err=%v", delta, got, link, a.B, a.Err())
			}
		}
	}
}

func FuzzLongFixup(f *testing.F) {
	for _, delta := range []int64{-2147485696, 2147481596, 2147481600, -2147485700,
		-2048, 2048, 0, 2, math.MinInt64, math.MaxInt64} {
		f.Add(delta)
	}
	f.Fuzz(func(t *testing.T, delta int64) {
		var a Asm
		site := a.JumpLong(false)
		before := append([]byte(nil), a.B...)
		err := a.PatchLong(site, delta)
		valid := delta&3 == 0 && delta >= -2147485696 && delta <= 2147481596
		if (err == nil) != valid {
			t.Fatalf("delta=%d valid=%t err=%v", delta, valid, err)
		}
		if valid && decodedLongTarget(a.B, site) != delta || !valid && !bytes.Equal(a.B, before) {
			t.Fatalf("delta=%d incorrect target or partial fixup", delta)
		}
	})
}
