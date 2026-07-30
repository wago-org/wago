//go:build amd64

package amd64

import "testing"

func TestMultiBoundsCertificatesRetainIndependentSources(t *testing.T) {
	old := multiBoundsCertEnabled
	multiBoundsCertEnabled = true
	defer func() { multiBoundsCertEnabled = old }()

	var f fn
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

func TestSingleBoundsCertificateCompatibility(t *testing.T) {
	old := multiBoundsCertEnabled
	multiBoundsCertEnabled = false
	defer func() { multiBoundsCertEnabled = old }()

	var f fn
	f.boundsCertUpdate(1, 3, 16)
	f.boundsCertUpdate(1, 7, 32)
	if f.boundsCertCovers(1, 3, 8) {
		t.Fatal("single-entry mode retained the displaced proof")
	}
	if !f.boundsCertCovers(1, 7, 24) {
		t.Fatal("single-entry mode lost the current proof")
	}
}
