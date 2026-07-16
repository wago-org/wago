//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"
)

func compileStagedGCTypeSubtypingProductForTest(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.StructuralTypeProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedGCTypeSubtypingProductsCompile(t *testing.T) {
	for _, pin := range stagedGCTypeSubtypingProductPins {
		t.Run(pin.Filename, func(t *testing.T) {
			data := stagedGCTypeSubtypingProductData(t, pin)
			if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "gc type") {
				t.Fatalf("public compile = %v, want closed GC type gate", err)
			}
			c, err := compileStagedGCTypeSubtypingProductForTest(data)
			if err != nil {
				t.Fatalf("staged compile: %v", err)
			}
			_ = c.Close()
		})
	}
}
