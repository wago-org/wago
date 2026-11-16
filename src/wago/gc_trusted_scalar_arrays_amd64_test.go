//go:build linux && amd64 && wago_gc_direct_trusted_arrays

package wago

import "testing"

func TestTrustedDirectScalarArrayPolicy(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), directScalarArrayModule(0x7e, 0x7e, 11))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if c.executionView().gcTrustedDirectVersion != 3 {
		t.Fatal("missing policy3 attestation")
	}
}
