//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"math/bits"
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func wideArithmeticModule(sub byte, body []byte) []byte {
	n := 2
	if sub == 19 || sub == 20 {
		n = 4
	}
	params := make([]wasm.ValType, n)
	for i := range params {
		params[i] = wasm.I64
	}
	if body == nil {
		for i := 0; i < n; i++ {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, 0xfc, sub, 0x0b)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I64, wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func wideArithmeticExpected(sub byte, p []uint64) (lo, hi uint64) {
	switch sub {
	case 19:
		var c uint64
		lo, c = bits.Add64(p[0], p[2], 0)
		hi, _ = bits.Add64(p[1], p[3], c)
	case 20:
		var c uint64
		lo, c = bits.Sub64(p[0], p[2], 0)
		hi, _ = bits.Sub64(p[1], p[3], c)
	default:
		hi, lo = bits.Mul64(p[0], p[1])
		if sub == 21 {
			if int64(p[0]) < 0 {
				hi -= p[1]
			}
			if int64(p[1]) < 0 {
				hi -= p[0]
			}
		}
	}
	return
}

func TestWideArithmeticExecutionAndArtifact(t *testing.T) {
	for _, sub := range []byte{19, 20, 21, 22} {
		t.Run(string(rune('A'+sub-19)), func(t *testing.T) {
			// Default config: the proposal does not require a new user opt-in.
			c, err := Compile(wideArithmeticModule(sub, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var blob bytes.Buffer
			if _, err := c.WriteTo(&blob); err != nil {
				t.Fatal(err)
			}
			var loaded Compiled
			if _, err := loaded.ReadFrom(bytes.NewReader(blob.Bytes())); err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			for _, compiled := range []*Compiled{c, &loaded} {
				in, err := Instantiate(compiled, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				boundary := []uint64{0, 1, 2, 0x7fffffffffffffff, 0x8000000000000000, ^uint64(0)}
				rng := rand.New(rand.NewSource(128))
				for i := 0; i < 512; i++ {
					p := []uint64{rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64()}
					if i < len(boundary)*len(boundary) {
						p[0], p[1] = boundary[i%len(boundary)], boundary[i/len(boundary)]
						p[2], p[3] = boundary[(i+1)%len(boundary)], boundary[(i+3)%len(boundary)]
					}
					if sub >= 21 {
						p = p[:2]
					}
					lo, hi := wideArithmeticExpected(sub, p)
					got, err := in.Invoke("run", p...)
					if err != nil {
						t.Fatal(err)
					}
					if len(got) != 2 || got[0] != lo || got[1] != hi {
						t.Fatalf("op %d (%x): got %x, want [%x %x]", sub, p, got, lo, hi)
					}
				}
				// Warmed guest calls do not allocate per operation.
				p := []uint64{1, 2, 3, 4}
				if sub >= 21 {
					p = p[:2]
				}
				if n := testing.AllocsPerRun(100, func() {
					if _, err := in.Invoke("run", p...); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatalf("allocs/call = %g", n)
				}
			}
		})
	}
}

func BenchmarkWideArithmeticInvoke(b *testing.B) {
	for _, sub := range []byte{19, 20, 21, 22} {
		b.Run(string(rune('A'+sub-19)), func(b *testing.B) {
			c, err := Compile(wideArithmeticModule(sub, nil))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			p := []uint64{^uint64(0), 7, 9, 11}
			if sub >= 21 {
				p = p[:2]
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := in.Invoke("run", p...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
