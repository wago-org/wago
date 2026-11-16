//go:build linux && amd64 && (wago_gc_direct_trusted_arrays || wago_gc_direct_trusted_refstruct || wago_gc_direct_trusted_alloc)

package wago

import "testing"

func TestTrustedDirectScalarArrayPolicy(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), directScalarArrayModule(0x7e, 0x7e, 11))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if c.executionView().gcTrustedDirectVersion < 3 || c.executionView().gcTrustedDirectVersion > 5 {
		t.Fatal("missing scalar-array attestation")
	}
}
