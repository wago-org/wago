//go:build amd64 && (wago_gc_direct_reads || wago_gc_direct_alloc)

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
