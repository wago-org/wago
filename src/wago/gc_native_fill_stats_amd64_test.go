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
