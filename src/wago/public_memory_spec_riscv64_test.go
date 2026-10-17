//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"os"
	"testing"
)

const rv64MemorySpecSourcesSHA256 = "e1ff79d0dc7cc790fb084a08b83cf40a78d99a47c37ded5b6cd76ae771cda4a5"
const rv64MemorySpecFilesSHA256 = "1db20095c628b50560f8c1aad7a0eef35ed2cf455836087e7ffb7baa728255f5"

// TestRV64PublicPinnedMemorySpecExecution covers scalar loads/stores, effective
// addresses, boundary traps and byte order. Each module starts with one page;
// the oversized memory.grow request must fail without backing a large memory.
func TestRV64PublicPinnedMemorySpecExecution(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_MEMORY_SPEC_JSON_DIR")
	if dir == "" {
		t.Skip("prepare pinned fixtures with scripts/prepare-riscv64-numeric-spec.py --profile memory and set WAGO_RV64_MEMORY_SPEC_JSON_DIR")
	}
	manifest, err := rv64PublicNumericManifest(dir, "memory")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"modules": 9, "assert_return": 330, "assert_trap": 219, "assert_invalid": 97, "assert_invalid_validate": 97, "excluded_text_assert_malformed": 21}
	rv64PublicSpecRun(t, dir, manifest, want)
}
