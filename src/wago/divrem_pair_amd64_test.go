//go:build amd64 && (linux || darwin || windows) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
)

func divRemPairArithmetic(a, b uint64, wide, signed, rem bool) (uint64, TrapCode) {
	if !wide {
		a &= 0xffffffff
		b &= 0xffffffff
	}
	if b == 0 {
		return 0, TrapDivZero
	}
	if !signed {
		if rem {
			return a % b, 0
		}
		return a / b, 0
	}
	x, y := int64(a), int64(b)
	min := int64(-1 << 63)
	if !wide {
		x, y = int64(int32(a)), int64(int32(b))
		min = -1 << 31
	}
	if x == min && y == -1 {
		if rem {
			return 0, 0
		}
		return 0, TrapDivOverflow
	}
	v := x / y
	if rem {
		v = x % y
	}
	if !wide {
		return uint64(uint32(v)), 0
	}
	return uint64(v), 0
}

func compileDivRemPair(t testing.TB, b []byte) (*Compiled, *Instance) {
	t.Helper()
	c, e := Compile(nil, b)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	im := NewImports()
	im.HostFunc("env", "observe", func(HostCall) {})
	in, e := Instantiate(c, InstantiateOptions{Imports: im})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { in.Close() })
	return c, in
}

func TestDivRemPairAMD64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			for _, rev := range []bool{false, true} {
				for _, drop := range []bool{false, true} {
					if rev && drop {
						continue
					}
					f := divRemPairFixture{Wide: wide, Signed: signed, RemFirst: rev, DropQuotient: drop}
					t.Run(fmt.Sprintf("wide=%v/signed=%v/remfirst=%v/drop=%v", wide, signed, rev, drop), func(t *testing.T) {
						original := f.Module()
						_, instance := compileDivRemPair(t, original)
						edge := []uint64{0, 1, 2, 3, 7, 1 << 31, (1 << 31) - 1, 1<<32 - 1, 1 << 63, 1<<63 - 1, ^uint64(0), ^uint64(0) - 1}
						check := func(a, b uint64) {
							q, qt := divRemPairArithmetic(a, b, wide, signed, false)
							r, rt := divRemPairArithmetic(a, b, wide, signed, true)
							want := []uint64{q, r}
							trap := qt
							if rev {
								want = []uint64{r, q}
								trap = rt
								if trap == 0 {
									trap = qt
								}
							}
							if drop {
								want = []uint64{r}
							}
							got, err := instance.Invoke("run", a, b, 0)
							if trap != 0 {
								var te *TrapError
								if !errors.As(err, &te) || te.Code != trap {
									t.Fatalf("%x,%x = %v %v; want trap %v", a, b, got, err, trap)
								}
							} else {
								if err != nil || len(got) != len(want) {
									t.Fatalf("%x,%x = %v %v; want %v", a, b, got, err, want)
								}
								for i := range got {
									if got[i] != want[i] {
										t.Fatalf("%x,%x = %v; want %v", a, b, got, want)
									}
								}
							}
						}
						for _, a := range edge {
							for _, b := range edge {
								check(a, b)
							}
						}
						rng := rand.New(rand.NewSource(831))
						for i := 0; i < 512; i++ {
							check(rng.Uint64(), rng.Uint64())
						}
					})
				}
			}
		}
	}
}

func divRemPairLoopWant(f divRemPairFixture, a, b uint64, n int) uint64 {
	mask := ^uint64(0)
	if !f.Wide {
		mask = 0xffffffff
	}
	var total uint64
	for i := 0; i < n; i++ {
		q, _ := divRemPairArithmetic(a, b, f.Wide, f.Signed, false)
		r, _ := divRemPairArithmetic(a, b, f.Wide, f.Signed, true)
		v := (q + r) & mask
		if f.Loop == "pressure" {
			v = (v + 7*a) & mask
		}
		if f.Loop == "latency-biased" {
			v = (v + 1048576) & mask
		}
		total = (total + v) & mask
		if f.Loop == "latency" || f.Loop == "latency-biased" {
			a = v
		} else {
			a = (a + 1) & mask
		}
	}
	return total
}

func checkDivRemPairLoop(t *testing.T, f divRemPairFixture) {
	t.Helper()
	_, in := compileDivRemPair(t, f.Module())
	for _, n := range []int{0, 1, 2, 17, 4096} {
		got, e := in.Invoke("run", 123456789, 37, uint64(n))
		want := divRemPairLoopWant(f, 123456789, 37, n)
		if e != nil || len(got) != 1 || got[0] != want {
			t.Error(f, n, got, e, want)
		}
	}
}

func TestDivRemPairLoopsAMD64(t *testing.T) {
	for _, f := range divRemPairFixtures() {
		t.Run(divRemPairFixtureName(f), func(t *testing.T) { checkDivRemPairLoop(t, f) })
	}
}

func divRemPairFixtures() []divRemPairFixture {
	var out []divRemPairFixture
	for _, wide := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			for _, loop := range []string{"latency", "latency-biased", "throughput", "pressure"} {
				out = append(out, divRemPairFixture{Wide: wide, Signed: signed, Loop: loop})
			}
		}
	}
	return out
}

func divRemPairFixtureName(f divRemPairFixture) string {
	width := 32
	if f.Wide {
		width = 64
	}
	sign := "u"
	if f.Signed {
		sign = "s"
	}
	return fmt.Sprintf("i%d%s/%s", width, sign, f.Loop)
}

func BenchmarkDivRemPairAMD64(b *testing.B) {
	for _, f := range divRemPairFixtures() {
		b.Run(divRemPairFixtureName(f), func(b *testing.B) {
			c, in := compileDivRemPair(b, f.Module())
			fn, e := in.WasmFunc("run")
			if e != nil {
				b.Fatal(e)
			}
			want := divRemPairLoopWant(f, 123456789, 37, 4096)
			got, e := fn.Invoke(123456789, 37, 4096)
			if e != nil || got[0] != want {
				b.Fatal(got, e, want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, e = fn.Invoke(123456789, 37, 4096)
				if e != nil || got[0] != want {
					b.Fatal(got, e)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(c.CodeSize()), "code-B")
		})
	}
}
