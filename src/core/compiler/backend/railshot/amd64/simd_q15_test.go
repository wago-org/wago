//go:build linux && amd64 && !tinygo

package amd64

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"golang.org/x/sys/cpu"
)

var q15Profiles = []struct {
	name      string
	features  shared.AMD64Features
	supported func() bool
}{
	{"sse2", 0, func() bool { return true }},
	{"ssse3", shared.AMD64SSSE3, func() bool { return cpu.X86.HasSSSE3 }},
	{"avx", shared.AMD64SSSE3 | shared.AMD64AVX, func() bool { return cpu.X86.HasSSSE3 && cpu.X86.HasAVX }},
}

// q15Module builds a dependent chain. The checking form also returns the raw
// relaxed result and both inputs, with optional live vectors below the operands.
func q15Module(tb testing.TB, repeats, live int, check, alias bool) *wasm.Module {
	tb.Helper()
	params := make([]wasm.ValType, 2+live)
	for i := range params {
		params[i] = wasm.V128
	}
	results := []wasm.ValType{wasm.V128}
	body := []byte{0}
	for i := 0; i < live; i++ {
		body = append(body, 0x20, byte(2+i))
		body = append(body, v128ConstBytes([16]byte{85, 85, 85, 85, 85, 85, 85, 85, 85, 85, 85, 85, 85, 85, 85, 85})...)
		body = append(body, simdOp(81)...) // computed live value, not a lazy local.get
	}
	rhs := byte(1)
	if alias {
		rhs = 0
	}
	body = append(body, 0x20, 0)
	for i := 0; i < repeats; i++ {
		body = append(body, 0x20, rhs)
		body = append(body, simdOp(130)...)
	}
	if check {
		body = append(body, 0x20, 0, 0x20, rhs)
		body = append(body, simdOp(273)...)
		body = append(body, 0x20, 0, 0x20, 1)
		results = append(results, wasm.V128, wasm.V128, wasm.V128)
	}
	for i := 0; i < live; i++ {
		results = append(results, wasm.V128)
	}
	body = append(body, 0x0b)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := frontend.DecodeValidate(raw)
	if err != nil {
		tb.Fatal(err)
	}
	return m
}

func q15Runner(tb testing.TB, m *wasm.Module, features shared.AMD64Features) (args, results []byte, call func(), size int) {
	tb.Helper()
	cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, DeferCodeMapping: true})
	if err != nil {
		tb.Fatal(err)
	}
	if cm.RequiredAMD64Features & ^uint32(features) != 0 {
		tb.Fatalf("unexpected features: %x", cm.RequiredAMD64Features)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { eng.Close() })
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { jm.Close() })
	ar, err := runtime.NewArena(4096)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { ar.Close() })
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { runtime.Unmap(mem) })
	args, results, trap := ar.Alloc(512), ar.Alloc(512), ar.Alloc(runtime.TrapBufferBytes)
	address, linear := entry+uintptr(cm.Entry[0]), jm.LinearMemory()
	return args, results, func() {
		if err := eng.Call(address, args, linear, trap, results); err != nil {
			tb.Fatal(err)
		}
	}, len(cm.Code)
}

func TestSIMDQ15EmulationConsts(t *testing.T) {
	got := emulationConsts(130)
	want := [2]uint64{0x8000800080008000, 0x8000800080008000}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("Q15 constants = %x, want only %x", got, want)
	}
}

func TestSIMDQ15ResultCorrection(t *testing.T) {
	for _, profile := range q15Profiles {
		t.Run(profile.name, func(t *testing.T) {
			if !profile.supported() {
				t.Skip("host lacks selected ISA")
			}
			args, result, call, _ := q15Runner(t, q15Module(t, 1, 0, true, false), profile.features)
			check := func(a, b [16]byte) {
				copy(args, a[:])
				copy(args[16:], b[:])
				call()
				for lane := 0; lane < 8; lane++ {
					x := int64(int16(binary.LittleEndian.Uint16(a[2*lane:])))
					y := int64(int16(binary.LittleEndian.Uint16(b[2*lane:])))
					rounded := (x*y + (1 << 14)) >> 15
					strict := rounded
					if strict > 32767 {
						strict = 32767
					}
					if strict < -32768 {
						strict = -32768
					}
					for i, want := range []int16{int16(strict), int16(rounded)} {
						got := int16(binary.LittleEndian.Uint16(result[16*i+2*lane:]))
						if got != want {
							t.Fatalf("opcode=%d lane=%d inputs=(%d,%d): got %d, want %d", i, lane, x, y, got, want)
						}
					}
				}
				if string(result[32:48]) != string(a[:]) || string(result[48:64]) != string(b[:]) {
					t.Fatal("clobbered live input")
				}
			}
			// Every signed word against rounding boundaries, both extreme signs and
			// their neighbors. Swapping inputs checks both destructive operand orders.
			for _, edge := range []int16{-32768, -32767, -16385, -16384, -16383, -1, 0, 1, 16383, 16384, 16385, 32766, 32767} {
				for start := -32768; start <= 32767; start += 8 {
					var a, b [16]byte
					for lane := 0; lane < 8; lane++ {
						binary.LittleEndian.PutUint16(a[2*lane:], uint16(int16(start+lane)))
						binary.LittleEndian.PutUint16(b[2*lane:], uint16(edge))
					}
					check(a, b)
					check(b, a)
				}
			}
			rng := rand.New(rand.NewSource(130))
			for i := 0; i < 512; i++ {
				var a, b [16]byte
				rng.Read(a[:])
				rng.Read(b[:])
				check(a, b)
			}
		})
	}
}

func TestSIMDQ15LiveVectors(t *testing.T) {
	for _, profile := range q15Profiles {
		for _, alias := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/alias=%v", profile.name, alias), func(t *testing.T) {
				if !profile.supported() {
					t.Skip("host lacks selected ISA")
				}
				const live = 12
				args, result, call, _ := q15Runner(t, q15Module(t, 3, live, true, alias), profile.features)
				a := i16x8Bytes(-32768, -32767, -16384, -1, 0, 1, 16384, 32767)
				b := i16x8Bytes(-32768, -32768, 16384, -32768, 32767, -1, -16384, 32767)
				copy(args, a[:])
				copy(args[16:], b[:])
				for i := 32; i < (live+2)*16; i++ {
					args[i] = byte(i * 13)
				}
				call()
				for i := 0; i < live*16; i++ {
					if result[i] != args[32+i]^85 {
						t.Fatal("clobbered live vectors")
					}
				}
				if string(result[(live+2)*16:(live+4)*16]) != string(args[:32]) {
					t.Fatal("clobbered inputs")
				}
				for lane := 0; lane < 8; lane++ {
					x := int32(int16(binary.LittleEndian.Uint16(a[lane*2:])))
					y := int32(int16(binary.LittleEndian.Uint16(b[lane*2:])))
					if alias {
						y = x
					}
					want := x
					for i := 0; i < 3; i++ {
						want = (want*y + 16384) >> 15
						if want > 32767 {
							want = 32767
						}
					}
					if got := int16(binary.LittleEndian.Uint16(result[live*16+lane*2:])); got != int16(want) {
						t.Fatalf("lane=%d: got %d, want %d", lane, got, want)
					}
				}
			})
		}
	}
}

func BenchmarkSIMDQ15(b *testing.B) {
	for _, profile := range q15Profiles {
		for _, repeats := range []int{1, 64} {
			b.Run(fmt.Sprintf("%s/ops=%d", profile.name, repeats), func(b *testing.B) {
				m := q15Module(b, repeats, 0, false, false)
				b.Run("compile", func(b *testing.B) {
					opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: profile.features, DeferCodeMapping: true}
					size := 0
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							b.Fatal(err)
						}
						size = len(cm.Code)
					}
					b.ReportMetric(float64(size), "code-bytes")
				})
				b.Run("execute", func(b *testing.B) {
					if !profile.supported() {
						b.Skip("host lacks selected ISA")
					}
					args, _, call, size := q15Runner(b, m, profile.features)
					a := i16x8Bytes(-32768, -32767, -16384, -1, 0, 1, 16384, 32767)
					copy(args, a[:])
					copy(args[16:], a[:])
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						call()
					}
					b.ReportMetric(float64(size), "code-bytes")
				})
			})
		}
	}
}
