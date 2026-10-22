//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"os"
	"testing"
)

const rv64MultiMemorySpecSourcesSHA256 = "d2e308f186b51668ff394cf8aac4fb0d1fd9e692a467cd8631cae81def67cd4f"
const rv64MultiMemorySpecFilesSHA256 = "a4ea3aa16bbbd079613f8a1cd4cb66d7bdd983d5e8b7f5df981b407e2fa59a32"
const rv64Memory64SpecSourcesSHA256 = "0939d4a84888eea8b6f97a3c78da8c64e63ee6d312b7a0d79cbd0f660e3d8465"
const rv64Memory64SpecFilesSHA256 = "de097bd0e5f69a90932c332b7e46e6bb34e8fc7cca360f2e845d69fdbce5396b"

func rv64PublicMultiMemorySpecPolicy() rv64PublicSpecPolicy {
	return rv64PublicSpecPolicy{features: CoreFeaturesV2 | CoreFeatureMultiMemory, maxMemories: 5, maxInitialPages: 1, maxTotalInitialPages: 2}
}

func rv64PublicMemory64SpecPolicy() rv64PublicSpecPolicy {
	return rv64PublicSpecPolicy{features: CoreFeaturesV2 | CoreFeatureMemory64, maxMemories: 1, maxInitialPages: 1, maxTotalInitialPages: 1, memory64: true}
}

// TestRV64PublicPinnedMultiMemorySpecExecution qualifies six complete Release 3
// files using indexed owned memories. Only two initial pages in total may be
// backed; zero-sized memories still exercise nonzero directory indices.
func TestRV64PublicPinnedMultiMemorySpecExecution(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_MULTIMEMORY_SPEC_JSON_DIR")
	if dir == "" {
		t.Skip("prepare Release 3 fixtures with scripts/prepare-riscv64-numeric-spec.py --profile multimemory and set WAGO_RV64_MULTIMEMORY_SPEC_JSON_DIR")
	}
	manifest, err := rv64PublicNumericManifest(dir, "multimemory")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"modules": 6, "assert_return": 227, "assert_trap": 209}
	rv64PublicSpecRunPolicy(t, dir, manifest, want, rv64PublicMultiMemorySpecPolicy())
}

// TestRV64PublicPinnedMemory64SpecExecution qualifies four complete Release 3
// files. Their successful growth delta is zero; all initial memories are one
// page. The suite checks full-width effective addresses without large backing.
func TestRV64PublicPinnedMemory64SpecExecution(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_MEMORY64_SPEC_JSON_DIR")
	if dir == "" {
		t.Skip("prepare Release 3 fixtures with scripts/prepare-riscv64-numeric-spec.py --profile memory64 and set WAGO_RV64_MEMORY64_SPEC_JSON_DIR")
	}
	manifest, err := rv64PublicNumericManifest(dir, "memory64")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"modules": 8, "assert_return": 315, "assert_trap": 198, "assert_invalid": 46, "assert_invalid_validate": 46, "excluded_text_assert_malformed": 13}
	rv64PublicSpecRunPolicy(t, dir, manifest, want, rv64PublicMemory64SpecPolicy())
}

func TestRV64PublicSpecMemoryBudgetGate(t *testing.T) {
	multi, wide, legacy := rv64PublicMultiMemorySpecPolicy(), rv64PublicMemory64SpecPolicy(), rv64PublicSpecDefaultPolicy()
	for _, tc := range []struct {
		name     string
		policy   rv64PublicSpecPolicy
		memories []memoryDef
		valid    bool
	}{
		{"indexed-last", multi, []memoryDef{{}, {}, {}, {}, {Min: 1}}, true},
		{"aggregate-exact", multi, []memoryDef{{Min: 1}, {Min: 1}}, true},
		{"aggregate-over", multi, []memoryDef{{Min: 1}, {Min: 1}, {Min: 1}}, false},
		{"later-memory-over", multi, []memoryDef{{}, {Min: 2}}, false},
		{"count-over", multi, make([]memoryDef, 6), false},
		{"wide-in-narrow-profile", multi, []memoryDef{{Min: 1, Addr64: true}}, false},
		{"shared", multi, []memoryDef{{Min: 1, Shared: true}}, false},
		{"wide-exact", wide, []memoryDef{{Min: 1, Addr64: true}}, true},
		{"wide-over", wide, []memoryDef{{Min: 1 << 32, Addr64: true}}, false},
		{"narrow-in-wide-profile", wide, []memoryDef{{Min: 1}}, false},
		{"legacy-exact", legacy, []memoryDef{{Min: 16}}, true},
		{"legacy-over", legacy, []memoryDef{{Min: 17}}, false},
		{"legacy-count", legacy, []memoryDef{{}, {}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Compiled{memoryDir: &compiledMemoryDirectory{defs: tc.memories}}
			if err := tc.policy.checkMemories(c); (err == nil) != tc.valid {
				t.Fatalf("budget accepted=%v want %v: %v", err == nil, tc.valid, err)
			}
		})
	}
}
