//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCNumericInlineLeafBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  []byte
		local []byte
		want  bool
	}{
		{"add", []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}, nil, true},
		{"division", []byte{0x41, 42, 0x20, 0, 0x6d, 0x0b}, nil, true},
		{"select", []byte{0x41, 1, 0x41, 2, 0x20, 0, 0x1b, 0x0b}, nil, true},
		{"integer-local", []byte{0x20, 0, 0x21, 1, 0x20, 1, 0x0b}, []byte{1, 1, 0x7f}, true},
		{"float-local", []byte{0x20, 0, 0x0b}, []byte{1, 1, 0x7d}, false},
		{"reference-local", []byte{0x20, 0, 0x0b}, []byte{1, 1, 0x6d}, false},
		{"call", []byte{0x20, 0, 0x10, 0, 0x0b}, nil, false},
		{"memory", []byte{0x20, 0, 0x28, 2, 0, 0x0b}, nil, false},
		{"global", []byte{0x23, 0, 0x0b}, nil, false},
		{"allocation", []byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0x0b}, nil, false},
		{"if", []byte{0x20, 0, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x0b}, nil, false},
		{"loop", []byte{0x03, 0x40, 0x0b, 0x20, 0, 0x0b}, nil, false},
		{"unreachable", []byte{0x00, 0x0b}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			locals := tc.local
			if locals == nil {
				locals = []byte{0}
			}
			body := append(append([]byte(nil), locals...), tc.body...)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			if got := gcNumericInlineLeaf(m, 0); got != tc.want {
				t.Fatalf("numeric leaf = %t, want %t", got, tc.want)
			}
		})
	}
}
