//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCArrayFillResolverHints(t *testing.T) {
	for _, tc := range []struct {
		name          string
		storage       byte
		count         int32
		dynamic, open bool
		want          uint32
	}{
		{name: "i32-boundary", storage: 0x7f, count: 16, want: 1},
		{name: "i64-boundary", storage: 0x7e, count: 8, want: 1},
		{name: "i64-too-wide", storage: 0x7e, count: 9},
		{name: "too-many", storage: 0x7f, count: 17},
		{name: "negative", storage: 0x7f, count: -1},
		{name: "dynamic", storage: 0x7f, dynamic: true},
		{name: "open", storage: 0x7f, count: 4, open: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			composite := []byte{0x5e, tc.storage, 1}
			if tc.open {
				composite = append([]byte{0x50, 0}, composite...)
			}
			typ := byte(0x7f)
			if tc.storage == 0x7e {
				typ = 0x7e
			}
			prefix := []byte{0x20, 0, 0x20, 1, 0x20, 2}
			count := []byte{0x41, byte(tc.count) & 0x7f}
			ci := wasm.Instruction{Kind: wasm.InstrI32Const, I32: tc.count}
			if tc.dynamic {
				count = []byte{0x20, 1}
				ci = wasm.Instruction{Kind: wasm.InstrLocalGet, Index: 1}
			}
			expr := append(append(append([]byte(nil), prefix...), count...), 0xfb, 0x10, 0, 0x0b)
			body := append([]byte{0}, expr...)
			data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(composite, []byte{0x60, 3, 0x63, 0, 0x7f, typ, 0})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			for _, ast := range []bool{false, true} {
				if ast {
					m.Code[0].BodyBytes = nil
					m.Code[0].Body = wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrLocalGet, Index: 0}, {Kind: wasm.InstrLocalGet, Index: 1}, {Kind: wasm.InstrLocalGet, Index: 2}, ci, {Kind: wasm.InstrArrayFill, Index: 0}}}
				}
				hints, _, _, err := computeModuleHintsWithWorkersPolicy(m, 0, 0, 1, nil, true, currentCodegenPolicy())
				if err != nil {
					t.Fatal(err)
				}
				if got := hints[0].gcResolverSiteCount(); got != tc.want {
					t.Fatalf("AST=%v sites=%d, want %d", ast, got, tc.want)
				}
			}
		})
	}
}
