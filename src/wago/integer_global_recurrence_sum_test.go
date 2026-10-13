//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type integerRecurrenceCase struct {
	a, b, c, d, e int64
	old           bool
}

func integerGlobalRecurrenceModule(wide bool, tc integerRecurrenceCase) []byte {
	typ, ct, add, mul, lt, div := wasm.I32, byte(0x41), byte(0x6a), byte(0x6c), byte(0x49), byte(0x6e)
	if wide {
		typ, ct, add, mul, lt, div = wasm.I64, 0x42, 0x7c, 0x7e, 0x54, 0x80
	}
	con := func(v int64) []byte {
		if wide {
			return append([]byte{ct}, wasmtest.SLEB64(v)...)
		}
		return append([]byte{ct}, wasmtest.SLEB32(int32(v))...)
	}
	// A deferred division must trap before any reduction store. An old global
	// stack value must survive the final canonical global setter.
	body := []byte{0x23, 0}
	body = append(body, con(1)...)
	body = append(body, 0x20, 2, div, 0x1a, 0x03, 0x40, 0x20, 1, 0x02, wasm.MustEncodeValType(typ))
	if tc.old {
		body = append(body, 0x23, 0)
	}
	body = append(body, 0x23, 0, 0x20, 3)
	body = append(body, con(tc.a)...)
	body = append(body, mul)
	body = append(body, con(tc.b)...)
	body = append(body, add, add, 0x24, 0)
	if !tc.old {
		body = append(body, 0x23, 0)
	}
	body = append(body, con(tc.c)...)
	body = append(body, mul, 0x20, 3)
	body = append(body, con(tc.d)...)
	body = append(body, mul, add)
	body = append(body, con(tc.e)...)
	body = append(body, add, 0x0b, add, 0x21, 1, 0x20, 3)
	body = append(body, con(1)...)
	body = append(body, add, 0x21, 3, 0x20, 3, 0x20, 0, lt, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 3, 0x23, 0, 0x0b)
	code := append([]byte{1, 1, wasm.MustEncodeValType(typ)}, body...)
	init := append([]byte{wasm.MustEncodeValType(typ), 1}, con(41)...)
	init = append(init, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ, typ, typ}), wasmtest.FuncType([]wasm.ValType{typ}, nil), wasmtest.FuncType(nil, []wasm.ValType{typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(init)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0), wasmtest.ExportEntry("set", 0, 1), wasmtest.ExportEntry("global", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...), wasmtest.Code([]byte{0x20, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x23, 0, 0x0b}))))
}

// Use exact integers with division before modular reduction as an independent
// oracle; execution uses the inverse-of-three identity instead.
func integerRecurrenceOracle(n, gin, seed uint64, wide bool, tc integerRecurrenceCase) (sum, global uint64) {
	if n == 0 {
		n = 1
	}
	N := new(big.Int).SetUint64(n)
	triangle := new(big.Int).Mul(N, new(big.Int).Sub(new(big.Int).Set(N), big.NewInt(1)))
	triangle.Quo(triangle, big.NewInt(2))
	cubic := new(big.Int).Mul(new(big.Int).Set(triangle), new(big.Int).Sub(new(big.Int).Set(N), big.NewInt(2)))
	cubic.Quo(cubic, big.NewInt(3))
	G := new(big.Int).SetUint64(gin)
	end := new(big.Int).Add(new(big.Int).Set(G), new(big.Int).Mul(big.NewInt(tc.a), triangle))
	end.Add(end, new(big.Int).Mul(big.NewInt(tc.b), N))
	// Sum of old g: N*g + A*choose(N,3) + B*T(N).
	gs := new(big.Int).Mul(N, G)
	gs.Add(gs, new(big.Int).Mul(big.NewInt(tc.a), cubic))
	gs.Add(gs, new(big.Int).Mul(big.NewInt(tc.b), triangle))
	if !tc.old {
		gs.Add(gs, new(big.Int).Mul(big.NewInt(tc.a), triangle))
		gs.Add(gs, new(big.Int).Mul(big.NewInt(tc.b), N))
	}
	acc := new(big.Int).Mul(big.NewInt(tc.c), gs)
	acc.Add(acc, new(big.Int).Mul(big.NewInt(tc.d), triangle))
	acc.Add(acc, new(big.Int).Mul(big.NewInt(tc.e), N))
	acc.Add(acc, new(big.Int).SetUint64(seed))
	bits := uint(32)
	if wide {
		bits = 64
	}
	mod := new(big.Int).Lsh(big.NewInt(1), bits)
	return acc.Mod(acc, mod).Uint64(), end.Mod(end, mod).Uint64()
}

func TestIntegerGlobalRecurrenceSumExecution(t *testing.T) {
	cases := []integerRecurrenceCase{{1, 0, 1, 0, 0, false}, {3, 5, 2, 7, 11, false}, {-1, 3, -2, 1, -7, false}, {0, 1, 1, 0, 0, false}, {0, 0, 1, 0, 0, false}, {7, -3, 0, 5, 11, false}, {3, 5, 2, 7, 11, true}}
	for _, wide := range []bool{false, true} {
		for j, tc := range cases {
			for _, enabled := range []bool{false, true} {
				for _, abi := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide=%t/case=%d/opt=%t/abi=%t", wide, j, enabled, abi), func(t *testing.T) {
						c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", abi).Compile(integerGlobalRecurrenceModule(wide, tc))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						for _, product := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
							in, err := Instantiate(product)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							bounds := []uint64{0, 1, 2, 3, 4, 7, 63, 4096}
							mask := uint64(0xffffffff)
							if wide {
								mask = ^uint64(0)
							}
							if enabled {
								bounds = append(bounds, mask/2, mask/2+1, mask-1, mask)
							}
							for k, n := range bounds {
								gin := (uint64(0xdeadbeefcafebabe) + uint64(k)*0x123456789) & mask
								seed := uint64(0xfffffff1) & mask
								if _, err := in.Invoke("set", gin); err != nil {
									t.Fatal(err)
								}
								wantSum, wantGlobal := integerRecurrenceOracle(n, gin, seed, wide, tc)
								count := n
								if count == 0 {
									count = 1
								}
								want := []uint64{gin, wantSum, count, wantGlobal}
								got, err := in.Invoke("sum", n, seed, 1)
								if err != nil || len(got) != len(want) {
									t.Fatalf("n=%x got=%v err=%v", n, got, err)
								}
								for j := range want {
									if got[j] != want[j] {
										t.Fatalf("n=%x result%d=%x expected=%x", n, j, got[j], want[j])
									}
								}
								g, err := in.Invoke("global")
								if err != nil || len(g) != 1 || g[0] != wantGlobal {
									t.Fatalf("published global=%v err=%v", g, err)
								}
								if _, err := in.Invoke("sum", 2, seed, 0); err == nil {
									t.Fatal("earlier division did not trap")
								}
								g, err = in.Invoke("global")
								if err != nil || len(g) != 1 || g[0] != wantGlobal {
									t.Fatalf("trap changed global=%v err=%v", g, err)
								}
							}
						}
					})
				}
			}
		}
	}
}
