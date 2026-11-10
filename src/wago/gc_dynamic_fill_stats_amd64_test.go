//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCDynamicNumericFillAdmission(t *testing.T) {
	for _, tc := range []struct {
		name           string
		storage, stack byte
		open           bool
		count          int
		dynamic, short int
	}{
		{"i8", 0x78, 0x7f, false, -1, 1, 0}, {"i16", 0x77, 0x7f, false, -1, 1, 0},
		{"i32", 0x7f, 0x7f, false, -1, 1, 0}, {"i64", 0x7e, 0x7e, false, -1, 1, 0},
		{"f32", 0x7d, 0x7d, false, -1, 1, 0}, {"f64", 0x7c, 0x7c, false, -1, 1, 0},
		{"open", 0x7f, 0x7f, true, -1, 0, 0}, {"reference", 0x6f, 0x6f, false, -1, 0, 0},
		{"short", 0x7f, 0x7f, false, 4, 0, 1}, {"large-constant", 0x7f, 0x7f, false, 32, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			array := []byte{0x5e, tc.storage, 1}
			if tc.open {
				array = append([]byte{0x50, 0}, array...)
			}
			body := []byte{0x20, 0, 0x20, 1, 0x20, 2}
			if tc.count < 0 {
				body = append(body, 0x20, 3)
			} else {
				body = append(body, 0x41, byte(tc.count))
			}
			body = append(body, 0xfb, 16, 0, 0x0b)
			data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(array, []byte{0x60, 4, 0x63, 0, 0x7f, tc.stack, 0x7f, 0})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats backend.ModuleStats
			c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true})
			if err != nil {
				t.Fatal(err)
			}
			defer c.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-array-native-fill-dynamic"]; got != tc.dynamic {
				t.Fatalf("dynamic=%d want%d", got, tc.dynamic)
			}
			if got := stats.Funcs[0].Peephole["gc-array-native-fill"]; got != tc.short {
				t.Fatalf("short=%d want%d", got, tc.short)
			}
		})
	}
}
