//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestConditionalBoundsRestoreOnlyUnchangedLocal(t *testing.T) {
	f := fn{boundsFacts: true, conditionalBoundsSized: true, policy: currentCodegenPolicy()}
	f.boundsCertUpdate(1, 3, 16)
	f.boundsCertUpdate(1, 7, 32)
	f.boundsCertUpdate(2, 9, 64)
	saved := f.conditionalBoundsSnapshot(7)
	f.invalidateBoundsCert()
	f.restoreConditionalBounds(saved)
	if !f.boundsCertCovers(1, 3, 16) || f.boundsCertCovers(1, 7, 1) || f.boundsCertCovers(2, 9, 1) {
		t.Fatal("conditional fallthrough retained an invalid certificate or lost a stable local")
	}
	f.invalidateBoundsCert()
	if f.boundsCertCovers(1, 3, 1) {
		t.Fatal("join must invalidate restored proof")
	}
}

func TestConditionalBoundsFallthroughAndJoin(t *testing.T) {
	// Prove eight bytes, optionally branch out, then store through the original
	// local. After the join, replace that local and perform an independent store.
	body := []byte{0, 0x02, 0x40,
		0x20, 0, 0x29, 0, 0, 0x1a,
		0x20, 2, 0x0d, 0,
		0x20, 0, 0x20, 1, 0x36, 0, 4,
		0x0b,
		0x20, 3, 0x21, 0,
		0x20, 0, 0x20, 1, 0x36, 0, 0,
		0x41, 0, 0x0b}
	// Exercise the admitted source-size tier without altering execution.
	padding := make([]byte, 1024)
	for i := range padding {
		padding[i] = 0x01
	}
	body = append(body[:len(body)-1], append(padding, 0x0b)...)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		opts := CompileOptions{Optimizations: map[string]bool{"bounds-facts": true, "memory-min-bounds": false, "conditional-bounds-fallthrough": enabled}}
		for _, p := range []uint64{0, 65528, 65529, 65532, 0xffffffff, 0x8877665500000011} {
			for _, other := range []uint64{0, 65532, 65533, 0xffffffff, 0x8877665500000011} {
				for _, branch := range []uint64{0, 1} {
					expected := make([]byte, 65536)
					ptr, dst := uint64(uint32(p)), uint64(uint32(other))
					trap := ptr+8 > 65536
					if !trap && branch == 0 {
						binary.LittleEndian.PutUint32(expected[ptr+4:], 42)
					}
					if !trap {
						if dst+4 > 65536 {
							trap = true
						} else {
							binary.LittleEndian.PutUint32(expected[dst:], 42)
						}
					}
					got, err := runScalarUnrollMemory(t, m, opts, p, 42, branch, other)
					if (err != nil) != trap || !bytes.Equal(got, expected) {
						t.Fatalf("p=%d other=%d branch=%d trap=%v err=%v memory equal=%v", p, other, branch, trap, err, bytes.Equal(got, expected))
					}
				}
			}
		}
	}
}
