//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"os"
	"testing"
)

const rv64ControlSpecSourcesSHA256 = "dcd9fb758811a88f6e10afd4bf42f7794c481fca2c2bcb179933519a79391b22"
const rv64ControlSpecFilesSHA256 = "023c706b0c1fbbc5b14da9a3e004c57619b9527e84bccb5715d4ea3dad68d193"

// TestRV64PublicPinnedControlSpecExecution qualifies ten complete locals and
// structured-control files. The separate br_table file needs reference-value
// transport; call and factorial have separate bounded recursion profiles.
func TestRV64PublicPinnedControlSpecExecution(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_CONTROL_SPEC_JSON_DIR")
	if dir == "" {
		t.Skip("prepare pinned fixtures with scripts/prepare-riscv64-numeric-spec.py --profile control and set WAGO_RV64_CONTROL_SPEC_JSON_DIR")
	}
	manifest, err := rv64PublicNumericManifest(dir, "control")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"modules": 10, "assert_return": 654, "assert_trap": 1, "assert_invalid": 437, "assert_invalid_validate": 437, "excluded_text_assert_malformed": 53}
	rv64PublicSpecRun(t, dir, manifest, want)
}
