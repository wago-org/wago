//go:build linux && riscv64 && !tinygo

package riscv64_test

import (
	"fmt"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/wago"
)

// Former fixed-image inputs must use the ordinary source and artifact paths.
// A wire mutation is accepted exactly when it is a valid Core 3 module.
func TestFormerRV64FixtureGeneralCompilation(t *testing.T) {
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3).WithBoundsChecks(wago.BoundsChecksExplicit)
	check := func(t *testing.T, wire []byte, wantValid bool, artifact bool) {
		t.Helper()
		c, err := cfg.Compile(wire)
		if !wantValid {
			if c != nil {
				c.Close()
				t.Fatal("invalid source compiled")
			}
			if err == nil {
				t.Fatal("invalid source returned no error")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if artifact {
			blob, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := wago.LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			loaded.Close()
		}
	}
	for _, tc := range backend.GeneralGCSourceFixtures() {
		t.Run(tc.Name, func(t *testing.T) {
			t.Run("source", func(t *testing.T) { check(t, tc.Wire, true, true) })
			t.Run("canonical", func(t *testing.T) { check(t, tc.Canonical, true, true) })
			// A custom section is inert and cannot select a different compiler product.
			t.Run("custom", func(t *testing.T) {
				wire := append(append([]byte(nil), tc.Canonical...), 0, 2, 1, 'q')
				check(t, wire, true, true)
			})
			t.Run("mutations", func(t *testing.T) {
				for i := range tc.Wire {
					wire := append([]byte(nil), tc.Wire...)
					wire[i] ^= 1
					t.Run(fmt.Sprint(i), func(t *testing.T) {
						m, err := wasm.DecodeModule(wire)
						if err == nil {
							err = wasm.ValidateModuleWithFeatures(m, wasm.ValidationFeatures{CompactImports: true, MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true})
						}
						check(t, wire, err == nil, false)
					})
				}
			})
		})
	}
}
