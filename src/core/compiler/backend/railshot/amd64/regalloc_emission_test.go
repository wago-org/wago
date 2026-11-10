//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"crypto/sha256"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// scripts/check-regalloc-code.sh compares these exact code-image fingerprints
// across ordinary and checked builds. The checker must emit no guest code.
func TestRegallocCheckEmissionFingerprint(t *testing.T) {
	for _, test := range []struct {
		name   string
		module func(testing.TB) *wasm.Module
	}{
		{"scalar", benchSmallScalarModule},
		{"control", benchMediumControlModule},
		{"simd", benchSIMDHeavyModule},
		{"wrapper", benchSIMDWrapperCallModule},
		{"simd-control", benchSIMDControlModule},
	} {
		for _, compact := range []bool{false, true} {
			compiled, err := CompileModuleWith(test.module(t), CompileOptions{Workers: 1, CompactNative: compact})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("REGALLOC_CODE %s/compact=%t %x", test.name, compact, sha256.Sum256(compiled.Code))
			if compiled.CodeImage != nil {
				_ = compiled.CodeImage.Close()
			}
		}
	}
}
