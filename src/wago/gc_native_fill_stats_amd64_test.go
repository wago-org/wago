//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestGCNativeNumericFillAdmission(t *testing.T) {
	for _, tc := range []struct {
		storage byte
		typ     wasm.ValType
		want    []int
	}{
		{0x78, wasm.I32, []int{0, 1, 1, 1, 0, 0, 1}},
		{0x77, wasm.I32, []int{0, 1, 1, 1, 0, 0, 1}},
		{0x7f, wasm.I32, []int{0, 1, 1, 1, 0, 0, 1}},
		{0x7e, wasm.I64, []int{0, 1, 0, 1, 0, 0, 1}},
		{0x7d, wasm.F32, []int{0, 1, 1, 1, 0, 0, 1}},
		{0x7c, wasm.F64, []int{0, 1, 0, 1, 0, 0, 1}},
	} {
		m, err := wasm.DecodeModule(gcNumericFillModule(tc.storage, tc.typ, 20))
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		var stats backend.ModuleStats
		c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true})
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range tc.want {
			if got := stats.Funcs[i].Peephole["gc-array-native-fill"]; got != want {
				t.Errorf("storage %x function %d: native fills %d, want %d", tc.storage, i, got, want)
			}
		}
		c.CodeImage.Close()
	}
}

func TestGCArrayNewFillFusionAdmission(t *testing.T) {
	for _, tc := range []struct {
		name        string
		start       byte
		count       byte
		wantFusion  int
		wantFill    int
		wantForward int
	}{
		{name: "exact", count: 16, wantFusion: 1, wantForward: 1},
		{name: "partial", start: 1, count: 15, wantFill: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(gcArrayNewFillFusionModule(tc.start, tc.count))
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats backend.ModuleStats
			compiled, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true})
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-array-new-fill-fusion"]; got != tc.wantFusion {
				t.Fatalf("fusion count = %d, want %d", got, tc.wantFusion)
			}
			if got := stats.Funcs[0].Peephole["gc-array-native-fill"]; got != tc.wantFill {
				t.Fatalf("native fill count = %d, want %d", got, tc.wantFill)
			}
			if got := stats.Funcs[0].Peephole["gc-fresh-array-get-forward"]; got != tc.wantForward {
				t.Fatalf("fresh array forwarding = %d, want %d", got, tc.wantForward)
			}
		})
	}
}
