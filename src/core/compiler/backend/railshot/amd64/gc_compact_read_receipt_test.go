//go:build amd64 && wago_gc_compact_reads

package amd64

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

func TestCompactNativeReadArtifactReceipt(t *testing.T) {
	if gcDirectReads {
		cm, err := CompileModuleWith(nil, CompileOptions{})
		if cm != nil || err == nil {
			t.Fatal("mixed compact/direct lowering accepted")
		}
		return
	}
	m := gcNativeAttributionModule(t, false)
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if cm.GCHelperOnlyReceipt || cm.GCCompactHelperVersion != 0 || cm.GCDirectReadABIVersion != 0 || uint32(cm.GCCompactNativeVersion) != gc.CompactNativeABIVersion {
		t.Fatalf("helper=%v direct=%d", cm.GCHelperOnlyReceipt, cm.GCDirectReadABIVersion)
	}
}
