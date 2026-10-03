package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCProductRequirementGate(t *testing.T) {
	saved := gcProductRequiredGateEnabled
	defer func() { gcProductRequiredGateEnabled = saved }()
	for _, on := range []bool{false, true} {
		gcProductRequiredGateEnabled = on
		for _, f := range []CoreFeatures{0, CoreFeaturesV1, CoreFeatureReferenceTypes, CoreFeatureSIMD, CoreFeatureGC, CoreFeaturesV3} {
			want := !on || f.IsEnabled(CoreFeatureGC)
			if got := gcProductAnalysisNeeded(f); got != want {
				t.Fatalf("on=%v features=%x got=%v want=%v", on, f, got, want)
			}
		}
	}
}
func TestGCProductGatePreservesArtifacts(t *testing.T) {
	saved := gcProductRequiredGateEnabled
	defer func() { gcProductRequiredGateEnabled = saved }()
	function := func(body []byte) []byte {
		return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	}
	for _, tc := range []struct {
		name string
		data []byte
		gc   bool
	}{
		{"plain-with-prefix-in-immediate", function([]byte{0x41, 0xfb, 0x01, 0x1a, 0x41, 42, 0x0b}), false},
		{"abstract-i31", function([]byte{0x41, 42, 0xfb, 0x1c, 0xfb, 0x14, 0x6c, 0x0b}), true},
		{"extern-conversion", function([]byte{0xd0, 0x6f, 0xfb, 0x1a, 0xfb, 0x1b, 0xd1, 0x0b}), true},
		{"struct", stagedGCStructGetOnlyBytes(t), true},
		{"array", stagedGCArrayNumericLocalBytes(t), true},
		{"array-declaration", wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}))), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if got := moduleRequiredFeatures(m).IsEnabled(CoreFeatureGC); got != tc.gc {
				t.Fatalf("GC requirement=%v want=%v", got, tc.gc)
			}
			features := CoreFeaturesV1
			if tc.gc {
				requireCompleteCore3Backend(t)
				features = CoreFeaturesV3
			}
			var want []byte
			for _, on := range []bool{false, true} {
				gcProductRequiredGateEnabled = on
				c, err := NewRuntimeConfig().WithCoreFeatures(features).WithBoundsChecks(BoundsChecksExplicit).Compile(tc.data)
				if err != nil {
					t.Fatal(err)
				}
				b, err := marshalCompiled(c)
				c.Close()
				if err != nil {
					t.Fatal(err)
				}
				if !on {
					want = b
				} else if !bytes.Equal(want, b) {
					t.Fatal("gate changed persisted code or metadata")
				}
			}
		})
	}
}
