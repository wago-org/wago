//go:build linux && amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func experimentalReductionModule(t testing.TB, kind string) *wasm.Module {
	body := []byte{1, 1, 0x7e, 0x42, 0, 0x21, 2, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x45, 0x0d, 1,
		0x20, 2, 0x20, 0, 0x29, 0, 0, 0x7c, 0x21, 2, 0x20, 0, 0x41, 8, 0x6a, 0x21, 0,
		0x20, 1, 0x41, 1, 0x6b, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b}
	result := wasm.I64
	switch kind {
	case "i32":
		body[2] = 0x7f
		body[3] = 0x41
		body = bytes.Replace(body, []byte{0x29, 0, 0, 0x7c}, []byte{0x28, 0, 0, 0x6a}, 1)
		body = bytes.Replace(body, []byte{0x41, 8, 0x6a}, []byte{0x41, 4, 0x6a}, 1)
		result = wasm.I32
	case "xor":
		body = bytes.Replace(body, []byte{0x7c, 0x21, 2}, []byte{0x85, 0x21, 2}, 1)
	case "add-minus-one":
		body = bytes.Replace(body, []byte{0x41, 1, 0x6b}, []byte{0x41, 0x7f, 0x6a}, 1)
	case "tee-drop":
		body = bytes.Replace(body, []byte{0x21, 1, 0x0c, 0}, []byte{0x22, 1, 0x1a, 0x0c, 0}, 1)
	case "header-eq":
		body = bytes.Replace(body, []byte{0x20, 1, 0x45}, []byte{0x20, 1, 0x41, 0, 0x46}, 1)
	}
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{result}, body)
}

func TestExperimentalReductionForms(t *testing.T) {
	saved := shared.ReductionForms
	defer func() { shared.ReductionForms = saved }()
	for _, kind := range []string{"i32", "xor", "add-minus-one", "tee-drop", "header-eq"} {
		t.Run(kind, func(t *testing.T) {
			m := experimentalReductionModule(t, kind)
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			setup := func(mem []byte) {
				for i := range mem {
					mem[i] = byte(i*13 + 9)
				}
			}
			for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 512, 8192} {
				shared.ReductionForms = false
				want, wm, we := runMemAmd64(t, m, setup, 1, n)
				shared.ReductionForms = true
				got, gm, ge := runMemAmd64(t, m, setup, 1, n)
				if got != want || (ge != nil) != (we != nil) || !bytes.Equal(gm, wm) {
					t.Fatal(n, got, want, ge, we)
				}
			}
			if diagnosticsEnabled {
				shared.ReductionForms = true
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
				if err != nil {
					t.Fatal(err)
				}
				cm.CodeImage.Close()
				t.Log(kind, stats.Funcs[0].Peephole)
				if kind != "header-eq" && stats.Funcs[0].Peephole["counted-loop-bounds-hoist"] != 1 {
					t.Fatal("prototype not admitted")
				}
			}
		})
	}
}
