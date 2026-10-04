package wagobench

import (
	"crypto/sha256"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type sharingFixture struct {
	name  string
	bytes []byte
	want  uint64
}

func sharingU(v int) []byte {
	var b []byte
	for {
		x := byte(v & 127)
		v >>= 7
		if v != 0 {
			x |= 128
		}
		b = append(b, x)
		if v == 0 {
			return b
		}
	}
}
func sharingS(v int) []byte {
	var b []byte
	for {
		x := byte(v & 127)
		v >>= 7
		done := (v == 0 && x&64 == 0) || (v == -1 && x&64 != 0)
		if !done {
			x |= 128
		}
		b = append(b, x)
		if done {
			return b
		}
	}
}
func sharingModule(bodies [][]byte, locals int) []byte {
	b := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	sec := func(id byte, p []byte) { b = append(b, id); b = append(b, sharingU(len(p))...); b = append(b, p...) }
	sec(1, []byte{1, 96, 1, 127, 1, 127})
	p := sharingU(len(bodies))
	for range bodies {
		p = append(p, 0)
	}
	sec(3, p)
	sec(7, []byte{1, 3, 'r', 'u', 'n', 0, 0})
	p = sharingU(len(bodies))
	for _, body := range bodies {
		d := []byte{0}
		if locals > 0 {
			d = append([]byte{1}, sharingU(locals)...)
			d = append(d, 127)
		}
		d = append(d, body...)
		p = append(p, sharingU(len(d))...)
		p = append(p, d...)
	}
	sec(10, p)
	return b
}
func sharingFixtures() []sharingFixture {
	small := []byte{32, 0, 65, 1, 106, 11}
	pressure := []byte{}
	for k := 1; k <= 40; k++ {
		pressure = append(pressure, 32, 0, 65)
		pressure = append(pressure, sharingS(k)...)
		pressure = append(pressure, 106, 34, 1)
	}
	for k := 1; k < 40; k++ {
		pressure = append(pressure, 106)
	}
	pressure = append(pressure, 11)
	large := []byte{}
	for k := 0; k < 1000; k++ {
		large = append(large, 32, 0, 65, 1, 106, 33, 0)
	}
	large = append(large, 32, 0, 11)
	deep := []byte{}
	for k := 0; k < 24; k++ {
		deep = append(deep, 2, 127)
	}
	deep = append(deep, 32, 0)
	for k := 0; k < 25; k++ {
		deep = append(deep, 11)
	}
	manylocals := []byte{}
	for k := 1; k <= 192; k++ {
		manylocals = append(manylocals, 32, 0, 33)
		manylocals = append(manylocals, sharingU(k)...)
	}
	manylocals = append(manylocals, 32)
	manylocals = append(manylocals, sharingU(192)...)
	manylocals = append(manylocals, 11)
	join := []byte{32, 0, 4, 127, 65, 7, 34, 1, 5, 65, 9, 34, 1, 11, 32, 1, 106, 11}
	fallback := []byte{32, 0, 65, 2, 109, 11}
	many := make([][]byte, 512)
	for k := range many {
		many[k] = small
	}
	mixed := append([][]byte{large}, many...)
	return []sharingFixture{{"small", sharingModule([][]byte{small}, 1), 4}, {"pressure", sharingModule([][]byte{pressure}, 1), 940}, {"join", sharingModule([][]byte{join}, 1), 14}, {"large", sharingModule([][]byte{large}, 1), 1003}, {"deep", sharingModule([][]byte{deep}, 1), 3}, {"locals", sharingModule([][]byte{manylocals}, 192), 3}, {"many", sharingModule(many, 1), 4}, {"large_then_small", sharingModule(mixed, 1), 1003}, {"fallback", sharingModule([][]byte{fallback}, 1), 1}}
}
func TestSharingFixtures(t *testing.T) {
	for _, f := range sharingFixtures() {
		t.Run(f.name, func(t *testing.T) {
			c, e := wago.Compile(wago.NewRuntimeConfig().WithFunctionWorkers(1), f.bytes)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			in, e := wago.Instantiate(c)
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			got, e := in.Invoke("run", 3)
			if e != nil || len(got) != 1 || got[0] != f.want {
				t.Fatalf("got %v %v want %d", got, e, f.want)
			}
			m, _ := wasm.DecodeModule(f.bytes)
			t.Logf("fixture sha256=%x functions=%d module_bytes=%d", sha256.Sum256(f.bytes), len(m.Code), len(f.bytes))
		})
	}
}
func BenchmarkSharingNative(b *testing.B) {
	for _, f := range sharingFixtures() {
		b.Run(f.name, func(b *testing.B) {
			m, e := wasm.DecodeModule(f.bytes)
			if e != nil {
				b.Fatal(e)
			}
			if e = wasm.ValidateModule(m); e != nil {
				b.Fatal(e)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, e := benchCompileModuleWorkers(m, 1)
				if e != nil {
					b.Fatal(e)
				}
				if e = c.Close(); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
func BenchmarkSharingFull(b *testing.B) {
	for _, f := range sharingFixtures() {
		b.Run(f.name, func(b *testing.B) {
			cfg := wago.NewRuntimeConfig().WithFunctionWorkers(1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, e := wago.Compile(cfg, f.bytes)
				if e != nil {
					b.Fatal(e)
				}
				if e = c.Close(); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
func BenchmarkSharingExec(b *testing.B) {
	for _, f := range sharingFixtures() {
		b.Run(f.name, func(b *testing.B) {
			c, e := wago.Compile(wago.NewRuntimeConfig().WithFunctionWorkers(1), f.bytes)
			if e != nil {
				b.Fatal(e)
			}
			defer c.Close()
			in, e := wago.Instantiate(c)
			if e != nil {
				b.Fatal(e)
			}
			defer in.Close()
			got, e := in.Invoke("run", 3)
			if e != nil || len(got) != 1 || got[0] != f.want {
				b.Fatal(got, e)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, e = in.Invoke("run", 3)
				if e != nil || len(got) != 1 || got[0] != f.want {
					b.Fatal(got, e)
				}
			}
		})
	}
}
