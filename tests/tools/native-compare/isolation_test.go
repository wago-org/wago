//go:build wago_nativecompare

package main

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// Broad build/install discovery must not acquire this command when another
// diagnostic tag is enabled or a new helper/test file is added without the gate.
func TestCommandRequiresExplicitOptIn(t *testing.T) {
	for _, tags := range []string{"", "wago_runtime", "wago_runtime,wago_lean,wago_minimal", "wago_profile", "wago_codegenstats", "wago_nativecompare"} {
		output, err := exec.Command("go", "list", "-e", "-json", "-tags="+tags, ".").CombinedOutput()
		if err != nil {
			t.Fatalf("tags %q: %v: %s", tags, err, output)
		}
		var selected struct {
			GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
		}
		if err := json.Unmarshal(output, &selected); err != nil {
			t.Fatal(err)
		}
		count := len(selected.GoFiles) + len(selected.CgoFiles) + len(selected.TestGoFiles) + len(selected.XTestGoFiles)
		if (count != 0) != (tags == "wago_nativecompare") {
			t.Fatalf("tags %q selected diagnostic files: %+v", tags, selected)
		}
	}
}
