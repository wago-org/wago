//go:build (amd64 || arm64 || (linux && riscv64)) && !tinygo

package wago

import (
	"bytes"
	"math/bits"
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

var wideArithmeticNames = [...]string{"add128", "sub128", "mul_wide_s", "mul_wide_u"}

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
		t.Run(wideArithmeticNames[sub-19], func(t *testing.T) {
			// Artifact round trips require explicit bounds, including guard-page builds.
			// Composition and boundary tests also cover default-config admission.
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), wideArithmeticModule(sub, nil))
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
		b.Run(wideArithmeticNames[sub-19], func(b *testing.B) {
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

func TestWideArithmeticComposition(t *testing.T) {
	for _, sub := range []byte{19, 20, 21, 22} {
		n := 2
		if sub < 21 {
			n = 4
		}
		for _, mode := range []string{"local-preserved", "deferred-div", "pressure", "block", "chain"} {
			if mode == "chain" && sub >= 21 {
				continue
			}
			var body []byte
			if mode == "pressure" {
				for i := 0; i < 20; i++ {
					body = append(body, 0x20, 0)
				}
			}
			for i := 0; i < n; i++ {
				body = append(body, 0x20, byte(i))
				if mode == "deferred-div" {
					body = append(body, 0x42, 3, 0x80)
				}
			}
			if mode == "block" {
				body = append(body, 0x02, 0)
			}
			body = append(body, 0xfc, sub)
			if mode == "block" {
				body = append(body, 0x0b)
			}
			if mode == "local-preserved" {
				body = append(body, 0x20, 0, 0x7c)
			}
			if mode == "pressure" {
				body = append(body, 0x1a)
				for i := 0; i < 20; i++ {
					body = append(body, 0x7c)
				}
				body = append(body, 0x42, 0)
			}
			if mode == "chain" {
				body = append(body, 0x20, 2, 0x20, 3, 0xfc, sub)
			}
			body = append(body, 0x0b)
			c, err := Compile(wideArithmeticModule(sub, body))
			if err != nil {
				t.Fatalf("%d %s compile: %v", sub, mode, err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			rng := rand.New(rand.NewSource(654))
			for j := 0; j < 100; j++ {
				p := []uint64{rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64()}
				if n == 2 {
					p = p[:2]
				}
				effective := append([]uint64(nil), p...)
				if mode == "deferred-div" {
					for i := range effective {
						effective[i] /= 3
					}
				}
				lo, hi := wideArithmeticExpected(sub, effective)
				if mode == "local-preserved" {
					hi += p[0]
				}
				if mode == "pressure" {
					lo += 20 * p[0]
					hi = 0
				}
				if mode == "chain" {
					lo, hi = wideArithmeticExpected(sub, []uint64{lo, hi, p[2], p[3]})
				}
				got, err := in.Invoke("run", p...)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != 2 || got[0] != lo || got[1] != hi {
					t.Fatalf("%d %s %x: got %x want [%x %x]", sub, mode, p, got, lo, hi)
				}
			}
		}
	}
}

func TestWideArithmeticCarryBorrowBoundary(t *testing.T) {
	max := ^uint64(0)
	for _, tc := range []struct {
		sub    byte
		p      []uint64
		lo, hi uint64
	}{
		{19, []uint64{max, 0, 1, 0}, 0, 1},
		{19, []uint64{max, max, 1, 0}, 0, 0},
		{19, []uint64{max, 9, 2, 30}, 1, 40},
		{20, []uint64{0, 1, 1, 0}, max, 0},
		{20, []uint64{0, 0, 1, 0}, max, max},
		{20, []uint64{0, 0, 0, 1}, 0, max},
		{21, []uint64{1 << 63, max}, 1 << 63, 0},
		{21, []uint64{1 << 63, 1 << 63}, 0, 1 << 62},
		{21, []uint64{max, 2}, max - 1, max},
		{22, []uint64{1 << 63, 2}, 0, 1},
		{22, []uint64{max, max}, 1, max - 1},
	} {
		c, err := Compile(wideArithmeticModule(tc.sub, nil))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		got, err := in.Invoke("run", tc.p...)
		if err != nil || len(got) != 2 || got[0] != tc.lo || got[1] != tc.hi {
			t.Fatalf("%s %x: got %x, %v; want [%x %x]", wideArithmeticNames[tc.sub-19], tc.p, got, err, tc.lo, tc.hi)
		}
	}
}
