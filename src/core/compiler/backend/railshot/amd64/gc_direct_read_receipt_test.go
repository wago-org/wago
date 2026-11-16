//go:build amd64 && wago_gc_direct_reads

package amd64

import "testing"

func TestDirectReadArtifactReceipt(t *testing.T) {
	m := gcNativeAttributionModule(t, false)
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if cm.GCHelperOnlyReceipt || cm.GCDirectReadABIVersion != 1 {
		t.Fatalf("helper=%v direct=%d", cm.GCHelperOnlyReceipt, cm.GCDirectReadABIVersion)
	}
}
