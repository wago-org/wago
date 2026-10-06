//go:build amd64 && (linux || darwin || windows) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func compileLoopBoundary(t testing.TB, f loopBoundaryFixture) (*Compiled, *Instance) {
	t.Helper()
	raw := watToWasm(t, f.WAT())
	c, e := Compile(nil, raw)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	in, e := Instantiate(c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { in.Close() })
	return c, in
}

func TestLoopBoundarySemanticsAMD64(t *testing.T) {
	inputs := [][2]float64{{1.25, 2}, {math.SmallestNonzeroFloat64, 1.5}, {math.Float64frombits(1 << 52), 0.5}, {0, 1}, {math.Copysign(0, -1), 1}, {math.Inf(1), 0}, {math.Inf(-1), 2}, {math.NaN(), 1}}
	for _, f := range loopBoundaryFixtures {
		t.Run(f.Name, func(t *testing.T) {
			_, in := compileLoopBoundary(t, f)
			for _, args := range inputs {
				for _, n := range []uint32{0, 1, 17, 4096} {
					for _, enabled := range []uint64{0, 1} {
						got, e := in.Invoke("run", math.Float64bits(args[0]), math.Float64bits(args[1]), uint64(n), enabled)
						want := f.Want(args[0], args[1], n, enabled != 0)
						if e != nil || len(got) != 1 || !loopBoundaryEqual(got[0], want) {
							t.Fatalf("args=%v n=%d enabled=%d got=%x err=%v want=%x", args, n, enabled, got, e, want)
						}
					}
				}
			}
		})
	}
}

func BenchmarkLoopBoundaryExecuteAMD64(b *testing.B) {
	for _, f := range loopBoundaryFixtures {
		for _, subnormal := range []bool{false, true} {
			for _, n := range []uint32{64, 4096} {
				b.Run(fmt.Sprintf("%s/subnormal=%v/n=%d", f.Name, subnormal, n), func(b *testing.B) {
					c, in := compileLoopBoundary(b, f)
					fn, e := in.WasmFunc("run")
					if e != nil {
						b.Fatal(e)
					}
					a, d := 1.25, 2.0
					if subnormal {
						a, d = math.SmallestNonzeroFloat64, 1.5
					}
					want := f.Want(a, d, n, true)
					aa, dd := math.Float64bits(a), math.Float64bits(d)
					got, e := fn.Invoke(aa, dd, uint64(n), 1)
					if e != nil || len(got) != 1 || !loopBoundaryEqual(got[0], want) {
						b.Fatal(got, e, want)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						got, e = fn.Invoke(aa, dd, uint64(n), 1)
						if e != nil || got[0] != want {
							b.Fatal(got, e, want)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(c.CodeSize()), "code-B")
				})
			}
		}
	}
}

func BenchmarkLoopBoundaryCompileAMD64(b *testing.B) {
	for _, f := range loopBoundaryFixtures {
		b.Run(f.Name, func(b *testing.B) {
			raw := watToWasm(b, f.WAT())
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, e := Compile(nil, raw)
				if e != nil {
					b.Fatal(e)
				}
				c.Close()
			}
		})
	}
}

func TestLoopBoundaryZeroIterationTrapsAMD64(t *testing.T) {
	const wat = `(module (func (export "run") (param $a i64) (param $b i64) (param $n i32) (param $enabled i32) (result i64)
 (local $value i64) (local $i i32)
 local.get $enabled if (result i64)
 block $exit (result i64)
 local.get $a local.get $b i64.div_s
 loop $again (param i64) (result i64)
 local.set $value
 local.get $i local.get $n i32.ge_u if local.get $value br $exit end
 local.get $i i32.const 1 i32.add local.set $i
 local.get $value br $again
 end end
 else i64.const 0 end))`
	raw := watToWasm(t, wat)
	c, e := Compile(nil, raw)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	in, e := Instantiate(c)
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	for _, n := range []uint64{0, 1, 17} {
		for _, enabled := range []uint64{0, 1} {
			for _, tc := range []struct {
				a, b, want uint64
				trap       TrapCode
			}{
				{100, 7, 14, 0}, {100, 0, 0, TrapDivZero}, {1 << 63, ^uint64(0), 0, TrapDivOverflow},
			} {
				got, e := in.Invoke("run", tc.a, tc.b, n, enabled)
				want, trap := tc.want, tc.trap
				if enabled == 0 {
					want, trap = 0, 0
				}
				if trap != 0 {
					var actual *TrapError
					if !errors.As(e, &actual) || actual.Code != trap {
						t.Fatal(n, enabled, tc, got, e)
					}
				} else if e != nil || len(got) != 1 || got[0] != want {
					t.Fatal(n, enabled, tc, got, e)
				}
			}
		}
	}
}
