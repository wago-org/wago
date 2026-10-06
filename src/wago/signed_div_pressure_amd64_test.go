//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Seven eager carriers exhaust the available registers while the overflow
// guard needs its INT64_MIN constant. All carriers remain live after division.
func signedDivPressureModule(wide bool, carriers int, remainder, pair bool) []byte {
	typ, div, add, constant, reinterpret := wasm.I32, byte(0x6d), byte(0x6a), byte(0x41), []byte{0xbe, 0xbc}
	if wide {
		typ, div, add, constant, reinterpret = wasm.I64, 0x7f, 0x7c, 0x42, []byte{0xbf, 0xbd}
	}
	op := div
	if remainder {
		op += 2
	}
	body := []byte{2, 1, 0x7f, 1, wasm.MustEncodeValType(typ), // counter and accumulator
		0x02, 0x40, 0x03, 0x40, 0x20, 3, 0x20, 2, 0x4f, 0x0d, 1}
	for j := 0; j < carriers; j++ {
		body = append(body, 0x20, 0)
		body = append(body, reinterpret...)
	}
	body = append(body, 0x20, 0, 0x20, 1, op)
	if pair {
		body = append(body, 0x20, 0, 0x20, 1, div+2, add)
	}
	for j := 0; j < carriers; j++ {
		body = append(body, add)
	}
	body = append(body, 0x20, 4, add, 0x21, 4, 0x20, 0, constant, 1, add, 0x21, 0,
		0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b, 0x20, 4, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func signedDivPressureWant(a, b uint64, n, carriers int, wide, remainder, pair bool) (uint64, TrapCode) {
	mask, min := ^uint64(0), int64(-1<<63)
	if !wide {
		mask, min = 0xffffffff, -1<<31
	}
	a &= mask
	b &= mask
	var total uint64
	for i := 0; i < n; i++ {
		x, y := int64(a), int64(b)
		if !wide {
			x, y = int64(int32(a)), int64(int32(b))
		}
		if y == 0 {
			return 0, TrapDivZero
		}
		overflow := x == min && y == -1
		if overflow && !remainder {
			return 0, TrapDivOverflow
		}
		var r, q int64
		if !overflow {
			q, r = x/y, x%y
		}
		v := uint64(q)
		if remainder {
			v = uint64(r)
		} else if pair {
			v += uint64(r)
		}
		total = (total + v + uint64(carriers)*a) & mask
		a = (a + 1) & mask
	}
	return total, 0
}

func TestSignedDivPreservesLiveValuesAMD64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, carriers := range []int{0, 7, 9} {
			for _, mode := range []string{"div", "rem", "pair"} {
				remainder, pair := mode == "rem", mode == "pair"
				t.Run(fmt.Sprintf("wide=%v/carriers=%d/%s", wide, carriers, mode), func(t *testing.T) {
					c, err := Compile(nil, signedDivPressureModule(wide, carriers, remainder, pair))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					min, minus := uint64(1<<31), uint64(0xffffffff)
					if wide {
						min, minus = 1<<63, ^uint64(0)
					}
					for _, args := range [][2]uint64{{123456789, 37}, {123456789, 1}, {123456789, minus}, {minus - 122, 37}, {min, 37}, {min, minus}, {123, 0}, {min, 0}} {
						for _, n := range []int{0, 1, 17} {
							want, trap := signedDivPressureWant(args[0], args[1], n, carriers, wide, remainder, pair)
							got, err := in.Invoke("run", args[0], args[1], uint64(n))
							if trap != 0 {
								var te *TrapError
								if !errors.As(err, &te) || te.Code != trap {
									t.Fatalf("args=%x n=%d: %v, %v; want trap %v", args, n, got, err, trap)
								}
							} else if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("args=%x n=%d: %v, %v; want %d", args, n, got, err, want)
							}
						}
					}
				})
			}
		}
	}
}

func BenchmarkSignedDivPressureAMD64(b *testing.B) {
	for _, carriers := range []int{0, 7} {
		for _, mode := range []string{"div", "rem", "pair"} {
			b.Run(fmt.Sprintf("carriers=%d/%s", carriers, mode), func(b *testing.B) {
				remainder, pair := mode == "rem", mode == "pair"
				c, err := Compile(nil, signedDivPressureModule(true, carriers, remainder, pair))
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					b.Fatal(err)
				}
				want, _ := signedDivPressureWant(123456789, 37, 4096, carriers, true, remainder, pair)
				got, err := fn.Invoke(123456789, 37, 4096)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("result %v, %v; want %d", got, err, want)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					got, err = fn.Invoke(123456789, 37, 4096)
					if err != nil || got[0] != want {
						b.Fatal(got, err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(c.CodeSize()), "code-B")
			})
		}
	}
}
