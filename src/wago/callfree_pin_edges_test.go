//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func callfreePinLoopModule() []byte {
	return callfreePinLoopModuleWithLocals(13)
}

func callfreePinLoopModuleWithLocals(locals int) []byte {
	addFloat := func(body []byte, v float64) []byte {
		return binary.LittleEndian.AppendUint64(append(body, 0x44), math.Float64bits(v))
	}
	var body []byte
	for i := 1; i <= locals; i++ {
		body = addFloat(body, float64(i))
		body = append(body, 0x21, byte(i))
	}
	body = append(body, 0x03, 0x40)
	for i := 1; i <= locals; i++ {
		body = append(body, 0x20, byte(i))
		body = addFloat(body, .25)
		body = append(body, 0xa0, 0x21, byte(i))
	}
	// The vector constant reserves another XMM alongside the scalar constants.
	// With thirteen hot float locals, the loop must relinquish pins under pressure.
	body = append(body, 0xfd, 12)
	body = binary.LittleEndian.AppendUint64(body, 0x12345678)
	body = binary.LittleEndian.AppendUint64(body, 0xabcdef)
	body = append(body, 0x1a, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b)
	body = append(body, 0x20, 1)
	for i := 2; i <= locals; i++ {
		body = append(body, 0x20, byte(i), 0xa0)
	}
	body = append(body, 0xbd, 0x0b)
	code := append([]byte{1, byte(locals), 0x7c}, body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))),
	)
}

func TestCallFreePinLoopEdges(t *testing.T) {
	compiled, err := Compile(nil, callfreePinLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, n := range []uint64{1, 2, 3, 7} {
		got, err := instance.Invoke("run", n)
		want := 91 + float64(n)*13*.25
		if err != nil || len(got) != 1 || got[0] != math.Float64bits(want) {
			t.Fatalf("iterations %d: got %x, %v; want %g (%x)", n, got, err, want, math.Float64bits(want))
		}
	}
}

func BenchmarkCallFreePinLoopCompile(b *testing.B) {
	for _, tc := range []struct {
		name   string
		locals int
	}{{"ordinary", 8}, {"pressure", 13}} {
		b.Run(tc.name, func(b *testing.B) {
			module := callfreePinLoopModuleWithLocals(tc.locals)
			var size int
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				compiled, err := Compile(nil, module)
				if err != nil {
					b.Fatal(err)
				}
				size = compiled.CodeSize()
				compiled.Close()
			}
			b.ReportMetric(float64(size), "code-B")
		})
	}
}

func BenchmarkCallFreePinLoopInvoke(b *testing.B) {
	// Eight locals leave enough scratch registers to avoid the bug, providing
	// a valid before/after control for the ordinary call-free loop fast path.
	compiled, err := Compile(nil, callfreePinLoopModuleWithLocals(8))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	const iterations = 32
	want := math.Float64bits(36 + iterations*8*.25)
	if got, err := instance.Invoke("run", iterations); err != nil || len(got) != 1 || got[0] != want {
		b.Fatalf("got %x, %v; want %x", got, err, want)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := instance.Invoke("run", iterations); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(compiled.CodeSize()), "code-B")
}
