//go:build amd64 && wago_gc_compact_helpers

package amd64

import (
	"strings"
	"testing"
)

func TestCompactHelperArtifactReceipt(t *testing.T) {
	// Reject a mixed mode before even inspecting the input module.
	if gcDirectReads {
		cm, err := CompileModuleWith(nil, CompileOptions{})
		if cm != nil || err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("mixed capability: module=%v err=%v", cm, err)
		}
		return
	}
	if !gcHelperOnly {
		t.Fatal("compact mode did not suppress native handle lowering")
	}
	m := gcNativeAttributionModule(t, false)
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if cm.GCCompactHelperVersion != 1 || cm.GCHelperOnlyReceipt || cm.GCDirectReadABIVersion != 0 || cm.GCTrustedDirectVersion != 0 {
		t.Fatalf("receipt compact=%d helper=%v direct=%d trusted=%d", cm.GCCompactHelperVersion, cm.GCHelperOnlyReceipt, cm.GCDirectReadABIVersion, cm.GCTrustedDirectVersion)
	}
}
