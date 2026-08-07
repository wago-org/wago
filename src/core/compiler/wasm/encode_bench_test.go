package wasm

import "testing"

var encodedExprSink []byte

func BenchmarkEncodeExprFastTables(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		instrs := make([]Instruction, 256)
		for i := range instrs {
			instrs[i].Kind = InstrNop
		}
		expr := Expr{Instrs: instrs}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out, err := EncodeExpr(expr)
			if err != nil {
				b.Fatal(err)
			}
			encodedExprSink = out
		}
	})
	b.Run("memory", func(b *testing.B) {
		instrs := make([]Instruction, 256)
		for i := range instrs {
			instrs[i] = Instruction{Kind: InstrI32Load, ext: &instrExt{MemArg: MemArg{Align: 2, Offset: 4}}}
		}
		expr := Expr{Instrs: instrs}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out, err := EncodeExpr(expr)
			if err != nil {
				b.Fatal(err)
			}
			encodedExprSink = out
		}
	})
}
