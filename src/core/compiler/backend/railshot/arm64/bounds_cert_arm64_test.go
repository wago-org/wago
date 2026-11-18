//go:build arm64

package arm64

import "testing"

func TestMultiBoundsCertificatesRetainIndependentSourcesArm64(t *testing.T) {
	old := multiBoundsCertEnabled
	multiBoundsCertEnabled = true
	defer func() { multiBoundsCertEnabled = old }()

	f := fn{policy: currentCodegenPolicy()}
	f.boundsCertUpdate(1, 3, 16)
	f.boundsCertUpdate(1, 7, 32)
	if !f.boundsCertCovers(1, 3, 8) || !f.boundsCertCovers(1, 7, 24) {
		t.Fatal("interleaved local sources did not retain independent proofs")
	}
	f.invalidateBoundsCertFor(1, 3)
	if f.boundsCertCovers(1, 3, 8) {
		t.Fatal("changed local retained a stale proof")
	}
	if !f.boundsCertCovers(1, 7, 24) {
		t.Fatal("changing one local discarded an independent proof")
	}
	f.invalidateBoundsCert()
	if f.boundsCertCovers(1, 7, 24) {
		t.Fatal("full invalidation retained a proof")
	}
}

func TestSingleBoundsCertificateCompatibilityArm64(t *testing.T) {
	old := multiBoundsCertEnabled
	multiBoundsCertEnabled = false
	defer func() { multiBoundsCertEnabled = old }()

	f := fn{policy: currentCodegenPolicy()}
	f.boundsCertUpdate(1, 3, 16)
	f.boundsCertUpdate(1, 7, 32)
	if f.boundsCertCovers(1, 3, 8) {
		t.Fatal("single-entry mode retained the displaced proof")
	}
	if !f.boundsCertCovers(1, 7, 24) {
		t.Fatal("single-entry mode lost the current proof")
	}
}

func TestBoundsCacheAdmissionMatchesUnconditionalInvalidation(t *testing.T) {
	old := multiBoundsCertEnabled
	defer func() { multiBoundsCertEnabled = old }()
	for _, multi := range []bool{false, true} {
		multiBoundsCertEnabled = multi
		f, reference := fn{policy: currentCodegenPolicy()}, fn{policy: currentCodegenPolicy()}
		state := uint32(119)
		for step := 0; step < 32000; step++ {
			state = state*1664525 + 1013904223
			kind, index, extent := uint8(state>>16%3), state>>8%32, int32(state>>2)
			switch (state >> 24) % 16 {
			case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11:
				f.boundsCertUpdate(kind, index, extent)
				reference.boundsCertUpdate(kind, index, extent)
			case 12, 13, 14:
				f.invalidateBoundsCertFor(kind, index)
				for i := range reference.boundsCerts {
					c := &reference.boundsCerts[i]
					if c.kind == kind && c.idx == index {
						*c = boundsCert{}
					}
				}
			case 15:
				f.invalidateBoundsCert()
				reference.boundsCerts = [8]boundsCert{}
				reference.nextBoundsCert = 0
			}
			if f.boundsCerts != reference.boundsCerts || f.nextBoundsCert != reference.nextBoundsCert {
				t.Fatalf("multi%v step%d: changed certificate state", multi, step)
			}
			for _, source := range []uint8{1, 2} {
				for idx := uint32(0); idx < 32; idx++ {
					want := false
					for _, c := range reference.boundsCerts {
						if c.kind == source && c.idx == idx && extent <= c.extent {
							want = true
							break
						}
					}
					if got := f.boundsCertCovers(source, idx, extent); got != want {
						t.Fatalf("multi%v step%d source%d index%d: got%v want%v", multi, step, source, idx, got, want)
					}
				}
			}
		}
	}
}
