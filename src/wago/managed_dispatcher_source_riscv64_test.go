//go:build linux && riscv64 && !wago_precompiled && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"os"
	"testing"
)

func TestRV64ManagedDispatcherBuiltinMatchesSource(t *testing.T) {
	c, err := Compile(nil, managedVoidDispatcherWasm)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	actual, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	// Set this path only when regenerating the checked-in built-in image.
	if output := os.Getenv("WAGO_RV64_MANAGED_DISPATCHER_OUTPUT"); output != "" {
		if err := os.WriteFile(output, actual, 0600); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile("internal_artifacts/managed_void_riscv64.wago")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatal("built-in managed dispatcher differs from its source; regenerate the artifact")
	}
}
