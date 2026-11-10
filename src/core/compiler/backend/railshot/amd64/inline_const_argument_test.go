//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestInlineI32ConstantArgumentSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	withInlineEnabled(t, func() {
		for _, tc := range []struct {
			name string
			body []byte
			leaf []byte
			want uint32
			hits int
		}{
			{"right", []byte{0x20, 0, 0x41, 7, 0x10, 1}, nil, 12, 1},
			{"left", []byte{0x41, 7, 0x20, 0, 0x10, 1}, nil, 12, 1},
			{"both", []byte{0x41, 5, 0x41, 7, 0x10, 1}, nil, 12, 1},
			{"wrapped", []byte{0x20, 0, 0x41, 0x7f, 0x10, 1}, nil, 4, 1},
			{"dynamic", []byte{0x20, 0, 0x20, 1, 0x10, 1}, nil, 16, 0},
			{"mixed", []byte{0x20, 0, 0x41, 7, 0x10, 1, 0x20, 0, 0x20, 1, 0x10, 1, 0x6a}, nil, 28, 1},
			{"different-leaf", []byte{0x20, 0, 0x41, 7, 0x10, 1}, []byte{0, 0x20, 0, 0x20, 1, 0x6b, 0x0b}, 0xfffffffe, 0},
			{"declared-local", []byte{0x20, 0, 0x41, 7, 0x10, 1}, []byte{1, 1, 0x7f, 0x20, 0, 0x20, 1, 0x6a, 0x0b}, 12, 0},
		} {
			t.Run(tc.name, func(t *testing.T) {
				leaf := tc.leaf
				if leaf == nil {
					leaf = []byte{0, 0x20, 0, 0x20, 1, 0x6a, 0x0b}
				}
				body := append([]byte{0}, tc.body...)
				body = append(body, 0x0b)
				m := modFuncs(t,
					funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body},
					funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
				)
				if got := uint32(runAmd64u(t, m, 5, 11)); got != tc.want {
					t.Fatalf("got %d, want %d", got, tc.want)
				}
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
				if err != nil {
					t.Fatal(err)
				}
				defer cm.CodeImage.Close()
				if got := stats.Funcs[0].Peephole["inline-i32-add-argument"]; got != tc.hits {
					t.Fatalf("specializations=%d, want%d", got, tc.hits)
				}
			})
		}
	})
}
