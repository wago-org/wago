//go:build amd64

package amd64

import (
	"fmt"
	"testing"
)

type boundsProofKey struct {
	kind  uint8
	index uint32
}

// The oracle records semantic proofs, independently of the cache's placement
// and eviction policy. Losing a proof is safe; accepting a stale one is not.
func checkBoundsProofs(f *fn, proofs map[boundsProofKey]int32) error {
	for kind := uint8(1); kind <= 2; kind++ {
		for index := uint32(0); index < 12; index++ {
			key := boundsProofKey{kind, index}
			for _, extent := range []int32{1, 4, 8, 16, 32, 64, 65} {
				if f.boundsCertCovers(kind, index, extent) && proofs[key] < extent {
					return fmt.Errorf("unproved access: kind=%d index=%d extent=%d", kind, index, extent)
				}
			}
		}
	}
	if f.boundsCertCovers(0, 0, 1) {
		return fmt.Errorf("unknown source has a proof")
	}
	return nil
}

func TestBoundsCertificatesProofOracle(t *testing.T) {
	old := multiBoundsCertEnabled
	defer func() { multiBoundsCertEnabled = old }()
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprint(multi), func(t *testing.T) {
			multiBoundsCertEnabled = multi
			f := fn{policy: currentCodegenPolicy()}
			proofs := map[boundsProofKey]int32{}
			// More than 256 replacements exercise the uint8 replacement cursor wrap.
			for step := 0; step < 4096; step++ {
				key := boundsProofKey{uint8(1 + step%2), uint32((step / 2) % 12)}
				switch step % 19 {
				case 0:
					f.invalidateBoundsCert()
					clear(proofs)
				case 1, 7:
					f.invalidateBoundsCertFor(key.kind, key.index)
					delete(proofs, key)
				default:
					extent := int32(1 << uint(step%7))
					f.boundsCertUpdate(key.kind, key.index, extent)
					if extent > proofs[key] {
						proofs[key] = extent
					}
					if !f.boundsCertCovers(key.kind, key.index, extent) {
						t.Fatalf("step %d lost new proof", step)
					}
				}
				if err := checkBoundsProofs(&f, proofs); err != nil {
					t.Fatalf("step %d: %v", step, err)
				}
			}
			// Deliberately insert a stale proof. The same oracle must reject it before
			// any native code executes; this is a negative control of the gate.
			f.invalidateBoundsCert()
			f.boundsCertUpdate(1, 0, 64)
			if err := checkBoundsProofs(&f, map[boundsProofKey]int32{}); err == nil {
				t.Fatal("oracle accepted stale proof")
			}
		})
	}
}

func TestBoundsCertificatesHolesAndReplacement(t *testing.T) {
	old := multiBoundsCertEnabled
	multiBoundsCertEnabled = true
	defer func() { multiBoundsCertEnabled = old }()
	f := fn{policy: currentCodegenPolicy()}
	for i := uint32(0); i < 8; i++ {
		f.boundsCertUpdate(1, i, 16)
	}
	f.invalidateBoundsCertFor(1, 0) // A hole before an existing match.
	f.boundsCertUpdate(1, 7, 32)
	f.boundsCertUpdate(1, 7, 8) // A smaller extent must not shrink the proof.
	matches := 0
	for _, c := range f.boundsCerts {
		if c.kind == 1 && c.idx == 7 {
			matches++
		}
	}
	if matches != 1 || !f.boundsCertCovers(1, 7, 32) {
		t.Fatal("update duplicated or shrank a proof after a hole")
	}
	f.boundsCertUpdate(2, 7, 4) // Same index, different source kind.
	if f.boundsCertCovers(2, 7, 8) || !f.boundsCertCovers(1, 7, 32) {
		t.Fatal("source kinds share a proof")
	}
	f.boundsCertUpdate(0, 0, 64)
	if !f.boundsCertCovers(2, 7, 4) {
		t.Fatal("unknown source discarded an independent proof")
	}
	f.invalidateBoundsCertFor(1, 7)
	if f.boundsCertCovers(1, 7, 1) || !f.boundsCertCovers(2, 7, 4) {
		t.Fatal("local invalidation changed a global proof with the same index")
	}
	// Repeated replacement without a reset reaches cursor wrap.
	for i := uint32(8); i < 600; i++ {
		f.boundsCertUpdate(1, i, 16)
		if !f.boundsCertCovers(1, i, 16) || f.boundsCertCovers(1, i, 17) {
			t.Fatalf("replacement %d: wrong extent", i)
		}
	}
	f.invalidateBoundsCert()
	for _, c := range f.boundsCerts {
		if c.kind != 0 {
			t.Fatal("reset retained a proof")
		}
	}
	if f.nextBoundsCert != 0 {
		t.Fatal("reset retained replacement position")
	}
}

func BenchmarkBoundsCertificates(b *testing.B) {
	old := multiBoundsCertEnabled
	multiBoundsCertEnabled = true
	defer func() { multiBoundsCertEnabled = old }()
	for _, sources := range []int{1, 2, 8, 9, 32} {
		b.Run(fmt.Sprintf("sources=%d", sources), func(b *testing.B) {
			f := fn{policy: currentCodegenPolicy()}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := uint32(i % sources)
				if !f.boundsCertCovers(1, key, 16) {
					f.boundsCertUpdate(1, key, 16)
				}
				if i%64 == 63 {
					f.invalidateBoundsCert()
				}
			}
		})
	}
}
