//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// WebAssembly/spec#2312, test/core/relaxed-simd/relaxed_dot_product.wast
// at e22ec2272bfbc06b995b00c1f1172a61d613fdd8. Wago chooses signed operands
// with i16 wrapping for dot and full i32 intermediates for dot-add.
func TestSIMDRelaxedDotUpstreamCorners(t *testing.T) {
	a := i8x16Bytes(-128, -128, -128, -128, -128, -128, -128, -128, 127, 127, 127, 127, 127, 127, 127, 127)
	b := i8x16Bytes(-128, -128, -128, -128, -127, -127, -127, -127, -128, -128, -128, -128, -1, -1, -1, -1)
	t.Run("dot", func(t *testing.T) {
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(a, b, 274))
		want := i16x8Bytes(-32768, -32768, 32512, 32512, -32512, -32512, -254, -254)
		if got := runAmd64V128(t, m, nil); got != want {
			t.Fatalf("dot = %x, want %x", got, want)
		}
	})
	t.Run("dot-add", func(t *testing.T) {
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128TernaryBody(a, b, i32x4Bytes(1, 2, 3, 4), 275))
		want := i32x4Bytes(65537, 65026, -65021, -504)
		if got := runAmd64V128(t, m, nil); got != want {
			t.Fatalf("dot-add = %x, want %x", got, want)
		}
	})
}

func BenchmarkSIMDRelaxedDotCompile(b *testing.B) {
	body := v128TernaryBody(i8x16Bytes(1, -2, 3, -4, 120, -120, 7, -7, 10, 20, -30, 40, 50, -60, 70, -80), i8x16Bytes(2, 3, -4, -5, 10, 11, -12, -13, 1, -2, 3, -4, 5, -6, 7, -8), i32x4Bytes(1000, 2000, -3000, 4000), 275)
	m := benchSIMDDecodeModule(b, body)
	cm, err := CompileModule(m)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		benchSIMDModuleSink = cm
	}
	b.ReportMetric(float64(len(cm.Code)), "code-B")
}

func relaxedDotSignedOracle(a, b, c [16]byte, add bool) (out [16]byte) {
	if add {
		for lane := 0; lane < 4; lane++ {
			sum := binary.LittleEndian.Uint32(c[4*lane:])
			for j := 0; j < 4; j++ {
				i := 4*lane + j
				sum += uint32(int32(int8(a[i])) * int32(int8(b[i])))
			}
			binary.LittleEndian.PutUint32(out[4*lane:], sum)
		}
	} else {
		for lane := 0; lane < 8; lane++ {
			i := 2 * lane
			sum := int32(int8(a[i]))*int32(int8(b[i])) + int32(int8(a[i+1]))*int32(int8(b[i+1]))
			binary.LittleEndian.PutUint16(out[2*lane:], uint16(sum))
		}
	}
	return
}

func TestSIMDRelaxedDotSignedLanes(t *testing.T) {
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, sub := range []uint32{274, 275} {
			t.Run(fmt.Sprintf("features=%x/op=%d", features, sub), func(t *testing.T) {
				params := []wasm.ValType{wasm.V128, wasm.V128}
				body := []byte{0, 0x20, 0, 0x20, 1}
				if sub == 275 {
					params = append(params, wasm.V128)
					body = append(body, 0x20, 2)
				}
				body = append(body, simdOp(sub)...)
				body = append(body, 0x0b)
				m := mod1(t, params, []wasm.ValType{wasm.V128}, body)
				cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				eng, err := runtime.NewEngine()
				if err != nil {
					t.Fatal(err)
				}
				defer eng.Close()
				jm, err := runtime.NewJobMemory(65536)
				if err != nil {
					t.Fatal(err)
				}
				defer jm.Close()
				ar, err := runtime.NewArena(4096)
				if err != nil {
					t.Fatal(err)
				}
				defer ar.Close()
				mem, entry, err := runtime.MapCode(cm.Code)
				if err != nil {
					t.Fatal(err)
				}
				defer runtime.Unmap(mem)
				args, results, trap := ar.Alloc(256), ar.Alloc(256), ar.Alloc(runtime.TrapBufferBytes)
				check := func(a, b, c [16]byte) {
					t.Helper()
					copy(args, a[:])
					copy(args[16:], b[:])
					copy(args[32:], c[:])
					want := relaxedDotSignedOracle(a, b, c, sub == 275)
					if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(results[:16], want[:]) {
						t.Fatalf("a=%x b=%x c=%x: got %x want %x", a, b, c, results[:16], want)
					}
				}
				c := i32x4Bytes(0, -1, math.MaxInt32, math.MinInt32)
				// Every signed-byte product, duplicated into each pair; this includes
				// the only overflowing signed i16 pair, (-128*-128)*2.
				for x := 0; x < 256; x++ {
					for y := 0; y < 256; y++ {
						var a, b [16]byte
						for i := range a {
							a[i], b[i] = byte(x), byte(y)
						}
						check(a, b, c)
					}
				}
				rng := rand.New(rand.NewSource(2312))
				for i := 0; i < 256; i++ {
					var a, b, c [16]byte
					rng.Read(a[:])
					rng.Read(b[:])
					rng.Read(c[:])
					check(a, b, c)
				}
			})
		}
	}
}

func TestSIMDRelaxedDotPreservesLiveInputs(t *testing.T) {
	requireCompilerDiagnostics(t)
	a := i8x16Bytes(-128, -128, 127, 127, -128, -128, 1, 2, -128, -128, 127, 127, -128, -128, 1, 2)
	b := i8x16Bytes(-128, -128, -128, -128, -127, -127, 3, 4, -128, -128, -128, -128, -127, -127, 3, 4)
	c := i32x4Bytes(math.MaxInt32, math.MinInt32, -1, 1)
	inputs := [][16]byte{a, b, c}
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, sub := range []uint32{274, 275} {
			t.Run(fmt.Sprintf("features=%x/op=%d", features, sub), func(t *testing.T) {
				body := []byte{1, 4, 0x7b}
				for i, v := range inputs {
					body = append(body, v128ConstBytes(v)...)
					body = append(body, 0x21, byte(i))
				}
				var live [12][16]byte
				for i := range live {
					live[i] = i32x4Bytes(int32(i*17+1), -int32(i*31+1), int32(i*47+1), -int32(i*63+1))
					body = append(body, v128ConstBytes(live[i])...)
					body = append(body, simdOp(77)...) // materialize live values before the dot
					for j := range live[i] {
						live[i][j] = ^live[i][j]
					}
				}
				body = append(body, 0x20, 0, 0x20, 1)
				if sub == 275 {
					body = append(body, 0x20, 2)
				}
				body = append(body, simdOp(sub)...)
				body = append(body, 0x21, 3)
				check := func(want [16]byte) {
					body = append(body, v128ConstBytes(want)...)
					body = append(body, simdOp(81)...)
					body = append(body, simdOp(83)...)
					body = append(body, 0x04, 0x40, 0x00, 0x0b)
				}
				for i := len(live) - 1; i >= 0; i-- {
					check(live[i])
				}
				for i, v := range inputs {
					body = append(body, 0x20, byte(i))
					check(v)
				}
				body = append(body, 0x20, 3, 0x0b)
				m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
				opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"v128-pins": true}}
				if got, want := runAmd64V128WithOptions(t, m, nil, opts), relaxedDotSignedOracle(a, b, c, sub == 275); got != want {
					t.Fatalf("got %x want %x", got, want)
				}
				var stats ModuleStats
				opts.Stats = &stats
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				if stats.Funcs[0].PinnedLocals == 0 {
					t.Fatal("fixture did not pin inputs")
				}
				t.Logf("pinned locals=%d spills=%d code bytes=%d", stats.Funcs[0].PinnedLocals, stats.Funcs[0].Spills, len(cm.Code))
			})
		}
	}
}
