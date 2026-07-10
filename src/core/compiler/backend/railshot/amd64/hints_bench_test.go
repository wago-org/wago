//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var benchFuncHints funcHints

func BenchmarkScanFuncHints(b *testing.B) {
	bodyBytes := []byte{
		0x20, 0x00, // local.get 0
		0x21, 0x01, // local.set 1
		0x23, 0x02, // global.get 2
		0x24, 0x03, // global.set 3
		0x28, 0x02, 0x10, // i32.load align=2 offset=16
		0xfc, 0x0a, 0x00, 0x00, // memory.copy dstmem=0 srcmem=0
		0x10, 0x07, // call 7
		0x0b,
	}
	bodyAST := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrLocalGet, Index: 0},
		{Kind: wasm.InstrLocalSet, Index: 1},
		{Kind: wasm.InstrGlobalGet, Index: 2},
		{Kind: wasm.InstrGlobalSet, Index: 3},
		{Kind: wasm.InstrI32Load},
		{Kind: wasm.InstrMemoryCopy},
		{Kind: wasm.InstrCall, Index: 7},
	}}

	b.Run("byte-backed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			h, err := scanBodyBytes(bodyBytes, 8, 4, 7)
			if err != nil {
				b.Fatal(err)
			}
			benchFuncHints = h
		}
	})
	b.Run("ast", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchFuncHints = scanBody(bodyAST, 8, 4, 7)
		}
	})
}
