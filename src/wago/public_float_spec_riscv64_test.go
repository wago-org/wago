//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"os"
	"testing"
)

const rv64FloatSpecSourcesSHA256 = "a38ae669275588fb5bddb88805731a6b0f71a7a404953f45229b68dd6173a803"
const rv64FloatSpecFilesSHA256 = "38b793910662d1921afc118840e0aa62fa85f724e6ccbcd21eb754c352833c91"

// TestRV64PublicPinnedFloatSpecExecution exercises the complete pinned f32/f64
// arithmetic, comparison and bitwise files. NaN classes and exact payloads use
// the shared matcher; every f32 input carries deliberately nonzero upper bits.
func TestRV64PublicPinnedFloatSpecExecution(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_FLOAT_SPEC_JSON_DIR")
	if dir == "" {
		t.Skip("prepare pinned fixtures with scripts/prepare-riscv64-numeric-spec.py --profile float and set WAGO_RV64_FLOAT_SPEC_JSON_DIR")
	}
	manifest, err := rv64PublicNumericManifest(dir, "float")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"modules": 6, "assert_return": 10520, "assert_invalid": 40, "assert_invalid_validate": 40, "excluded_text_assert_malformed": 4}
	rv64PublicSpecRun(t, dir, manifest, want)
}
