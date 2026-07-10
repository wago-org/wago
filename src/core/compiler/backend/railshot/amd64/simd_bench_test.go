//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

var (
	benchSIMDV128Sink   [16]byte
	benchSIMDModuleSink *encoderamd64.CompiledModule
)

func benchmarkSIMDV128Body(b *testing.B, body []byte) {
	b.Helper()
	m := benchSIMDDecodeModule(b, body)
	cm, err := CompileModule(m)
	if err != nil {
		b.Fatalf("amd64 compile: %v", err)
	}
	benchSIMDModuleSink = cm
	eng, err := runtime.NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		b.Fatal(err)
	}
	defer jm.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		b.Fatal(err)
	}
	defer ar.Close()
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		b.Fatalf("map: %v", err)
	}
	defer runtime.Unmap(mem)

	serArgs := ar.Alloc(256)
	results := ar.Alloc(256)
	trap := ar.Alloc(8)
	callEntry := entry + uintptr(cm.Entry[0])
	lin := jm.LinearMemory()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := eng.Call(callEntry, serArgs, lin, trap, results); err != nil {
			b.Fatalf("call: %v", err)
		}
		copy(benchSIMDV128Sink[:], results[:16])
	}
}

func benchSIMDDecodeModule(tb testing.TB, body []byte) *wasm.Module {
	tb.Helper()
	modBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(modBytes)
	if err != nil {
		tb.Fatalf("decode benchmark module: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatalf("validate benchmark module: %v", err)
	}
	if err := frontend.RejectUnsupported(m); err != nil {
		tb.Fatalf("support benchmark module: %v", err)
	}
	return m
}

func benchSIMDUnaryBody(a [16]byte, sub uint32) []byte {
	body := []byte{0x00}
	body = append(body, v128ConstBytes(a)...)
	body = append(body, simdOp(sub)...)
	body = append(body, 0x0b)
	return body
}

func benchSIMDShiftBody(a [16]byte, count int32, sub uint32) []byte {
	body := []byte{0x00}
	body = append(body, v128ConstBytes(a)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(count)...)
	body = append(body, simdOp(sub)...)
	body = append(body, 0x0b)
	return body
}

func BenchmarkSIMDF32x4MinOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		f32x4Bytes(1, -2, 3.5, 4),
		f32x4Bytes(2, -3, 3, 5),
		232,
	))
}

func BenchmarkSIMDF32x4MinEdges(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		f32x4Bits(0x7fc00001, 0x7fa00001, 0x00000000, 0x80000000),
		f32x4Bits(0x3f800000, 0xff800000, 0x80000000, 0x00000000),
		232,
	))
}

func BenchmarkSIMDF32x4MaxOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		f32x4Bytes(1, -2, 3.5, 4),
		f32x4Bytes(2, -3, 3, 5),
		233,
	))
}

func BenchmarkSIMDF32x4MaxEdges(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		f32x4Bits(0x7fc00001, 0x7fa00001, 0x00000000, 0x80000000),
		f32x4Bits(0x3f800000, 0x7f800000, 0x80000000, 0x00000000),
		233,
	))
}

func BenchmarkSIMDF64x2MinOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		f64x2Bytes(1.25, -2.5),
		f64x2Bytes(2.5, -3.25),
		244,
	))
}

func BenchmarkSIMDF64x2MinEdges(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		f64x2Bits(0x7ff8000000000001, 0x8000000000000000),
		f64x2Bits(0x7ff0000000000000, 0x0000000000000000),
		244,
	))
}

func BenchmarkSIMDF64x2MaxOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		f64x2Bytes(1.25, -2.5),
		f64x2Bytes(2.5, -3.25),
		245,
	))
}

func BenchmarkSIMDF64x2MaxEdges(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		f64x2Bits(0x7ff8000000000001, 0x8000000000000000),
		f64x2Bits(0xfff0000000000000, 0x0000000000000000),
		245,
	))
}

func BenchmarkSIMDI32x4TruncSatF32x4SOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f32x4Bytes(1.25, -2.75, 12345.5, -9876.5), 248))
}

func BenchmarkSIMDI32x4TruncSatF32x4SEdges(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f32x4Bits(0x7fc00000, 0x7f800000, 0xff800000, 0x4f000000), 248))
}

func BenchmarkSIMDI32x4TruncSatF32x4UOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f32x4Bytes(1.25, 2.75, 12345.5, 9876.5), 249))
}

func BenchmarkSIMDI32x4TruncSatF32x4UEdges(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f32x4Bits(0x7fc00000, 0x7f800000, 0xbf800000, 0x4f800000), 249))
}

func BenchmarkSIMDI32x4TruncSatF64x2SZeroOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f64x2Bytes(12345.5, -9876.5), 252))
}

func BenchmarkSIMDI32x4TruncSatF64x2SZeroEdges(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f64x2Bits(0x7ff8000000000001, 0xfff0000000000000), 252))
}

func BenchmarkSIMDI32x4TruncSatF64x2UZeroOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f64x2Bytes(12345.5, 9876.5), 253))
}

func BenchmarkSIMDI32x4TruncSatF64x2UZeroEdges(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f64x2Bits(0x7ff8000000000001, 0x41f0000000000000), 253))
}

func BenchmarkSIMDI32x4ConvertF32x4SOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(i32x4Bytes(1, -2, 123456, -98765), 250))
}

func BenchmarkSIMDI32x4ConvertF32x4UOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(i32x4Bytes(1, -1, 2147483647, -2147483648), 251))
}

func BenchmarkSIMDF64x2ConvertLowI32x4SOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(i32x4Bytes(1, -2, 123456, -98765), 254))
}

func BenchmarkSIMDF64x2ConvertLowI32x4UOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(i32x4Bytes(1, -1, 2147483647, -2147483648), 255))
}

func BenchmarkSIMDDemoteF64x2ZeroOrdinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f64x2Bytes(1.25, -2.5), 94))
}

func BenchmarkSIMDDemoteF64x2ZeroEdges(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f64x2Bits(0x7ff8000000000001, 0xfff0000000000000), 94))
}

func BenchmarkSIMDPromoteLowF32x4Ordinary(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f32x4Bytes(1.25, -2.5, 3.75, -4.5), 95))
}

func BenchmarkSIMDPromoteLowF32x4Edges(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDUnaryBody(f32x4Bits(0x7fc00001, 0xff800000, 0x00000000, 0x80000000), 95))
}

func BenchmarkSIMDI64x2Mul(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		i64x2Bytes(123456789, -987654321),
		i64x2Bytes(17, -23),
		213,
	))
}

func BenchmarkSIMDI64x2ShrS(b *testing.B) {
	benchmarkSIMDV128Body(b, benchSIMDShiftBody(i64x2Bytes(-1, -9223372036854775808), 13, 204))
}

func BenchmarkSIMDI64x2SignedCmpLtS(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		i64x2Bytes(-10, 99),
		i64x2Bytes(5, 99),
		216,
	))
}

func BenchmarkSIMDI64x2SignedCmpGeS(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		i64x2Bytes(-10, 99),
		i64x2Bytes(5, 99),
		219,
	))
}

func BenchmarkSIMDRelaxedDotI16x8I8x16I7x16S(b *testing.B) {
	benchmarkSIMDV128Body(b, v128BinaryBody(
		i8x16Bytes(1, -2, 3, -4, 120, -120, 7, -7, 10, 20, -30, 40, 50, -60, 70, -80),
		i8x16Bytes(2, 3, -4, -5, 10, 11, -12, -13, 1, -2, 3, -4, 5, -6, 7, -8),
		274,
	))
}

func BenchmarkSIMDRelaxedDotI32x4I8x16I7x16AddS(b *testing.B) {
	benchmarkSIMDV128Body(b, v128TernaryBody(
		i8x16Bytes(1, -2, 3, -4, 120, -120, 7, -7, 10, 20, -30, 40, 50, -60, 70, -80),
		i8x16Bytes(2, 3, -4, -5, 10, 11, -12, -13, 1, -2, 3, -4, 5, -6, 7, -8),
		i32x4Bytes(1000, 2000, -3000, 4000),
		275,
	))
}
