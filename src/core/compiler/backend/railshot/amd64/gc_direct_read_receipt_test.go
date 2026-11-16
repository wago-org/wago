//go:build amd64 && (wago_gc_direct_reads || wago_gc_direct_alloc || wago_gc_direct_refstore || wago_gc_direct_trusted || wago_gc_direct_trusted_store || wago_gc_direct_scalar_arrays || wago_gc_direct_trusted_arrays || wago_gc_direct_trusted_loop || wago_gc_direct_trusted_alloc)

package amd64

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

func TestDirectReadArtifactReceipt(t *testing.T) {
	m := gcNativeAttributionModule(t, false)
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if cm.GCHelperOnlyReceipt || cm.GCDirectReadABIVersion != gc.DirectABIVersion {
		t.Fatalf("helper=%v direct=%d", cm.GCHelperOnlyReceipt, cm.GCDirectReadABIVersion)
	}
}
