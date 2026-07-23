package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScalarConstExprInlineStackAndSpill(t *testing.T) {
	shallow := []byte{0x41, 0x02, 0x41, 0x03, 0x6c, 0x0b}
	if bits, _, err := evalScalarConstExprProgram(shallow, wasm.I32, nil); err != nil || bits != 6 {
		t.Fatalf("shallow expression = %d, %v; want 6", bits, err)
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		if _, _, err := evalScalarConstExprProgram(shallow, wasm.I32, nil); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatalf("shallow expression allocations = %v, want 0", allocs)
	}

	deep := make([]byte, 0, 32)
	for i := 0; i < 9; i++ {
		deep = append(deep, 0x41, 0x01)
	}
	for i := 1; i < 9; i++ {
		deep = append(deep, 0x6a)
	}
	deep = append(deep, 0x0b)
	if bits, _, err := evalScalarConstExprProgram(deep, wasm.I32, nil); err != nil || bits != 9 {
		t.Fatalf("spilled expression = %d, %v; want 9", bits, err)
	}
}

func BenchmarkScalarConstExprInlineStack(b *testing.B) {
	expr := []byte{0x41, 0x02, 0x41, 0x03, 0x6c, 0x0b}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := evalScalarConstExprProgram(expr, wasm.I32, nil); err != nil {
			b.Fatal(err)
		}
	}
}
