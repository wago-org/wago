package ruleguide

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTypedRecipeBudgets(t *testing.T) {
	for _, directed := range []bool{false, true} {
		recipes := Generate(Seed, directed)
		if len(recipes) != Budget {
			t.Fatalf("budget=%d", len(recipes))
		}
		counts := make(map[string][2]int)
		for _, r := range recipes {
			m, err := wasm.DecodeModule(r.Wasm())
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatalf("%s: %v", r.Name(), err)
			}
			c := counts[r.Family]
			if r.Selected() {
				c[1]++
			} else {
				c[0]++
			}
			counts[r.Family] = c
		}
		for family, c := range counts {
			if c[1] == 0 || (!directed && c[0] == 0) {
				t.Fatalf("directed=%t family=%s missed/selected=%v", directed, family, c)
			}
		}
		t.Logf("seed=%d directed=%t typed-template missed/selected=%v", Seed, directed, counts)
	}
}

func TestIndependentModels(t *testing.T) {
	swap := Cases()[0]
	// Enumerate every byte value in each byte position; expected byte order does
	// not use rotate, mask, or the compiler's recognizing sequence.
	for position := 0; position < 4; position++ {
		for value := uint32(0); value < 256; value++ {
			x := uint32(0x01234567)&^(uint32(255)<<(position*8)) | value<<(position*8)
			var raw [4]byte
			binary.LittleEndian.PutUint32(raw[:], x)
			want := []byte{raw[3], raw[2], raw[1], raw[0], 37, 0, 0, 0}
			if got := swap.Model([3]uint64{uint64(x)}); !bytes.Equal(got, want) {
				t.Fatalf("swap %x: %x != %x", x, got, want)
			}
		}
	}
	swar := Cases()[4]
	for bit := 0; bit < 64; bit++ {
		want := uint32(1)
		if bit%8 == 7 {
			want = 0
		}
		got := binary.LittleEndian.Uint32(swar.Model([3]uint64{uint64(1) << bit}))
		if got != want {
			t.Fatalf("mask bit=%d: %d != %d", bit, got, want)
		}
	}
	// Full-width vector: distinct sign/low bits in every lane, counts beyond the
	// lane width and negative (uint32) counts. Compare each lane by integer division.
	vector := Cases()[7]
	input := [3]uint64{0x800000007fffffff, 0xfedcba9812345678, 0}
	lanes := []uint64{0x7fffffff, 0x80000000, 0x12345678, 0xfedcba98}
	for count := int32(-1); count < 256; count++ {
		vector.Count = count
		got := vector.Model(input)
		for lane, x := range lanes {
			want := uint32(x / (uint64(1) << uint32(count&31)))
			if binary.LittleEndian.Uint32(got[lane*4:]) != want {
				t.Fatalf("shift count=%d lane=%d", count, lane)
			}
		}
	}
	// Each one-premise broken recipe must differ numerically on at least one
	// retained input, not merely change a diagnostic label.
	for _, r := range Cases() {
		if r.Selected() {
			continue
		}
		positive := r
		switch r.Family {
		case "bswap":
			positive.Mask = 0x00ff00ff
			positive.Rotate = 8
			positive.Second = 0
		case "swar":
			positive.Mask = -9187201950435737472
			positive.Variable = false
		case "simd":
			positive.Variable = false
		}
		different := false
		for _, in := range Inputs() {
			different = different || !bytes.Equal(r.Model(in), positive.Model(in))
		}
		if !different {
			t.Fatalf("near miss has no distinguishing input: %s", r.Name())
		}
	}
}
