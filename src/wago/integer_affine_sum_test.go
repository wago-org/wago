//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/big"
	"testing"
)

func integerAffineSumModule(wide, fenced bool, a, b uint64) []byte {
	return integerAffineSumModuleWithHelper(wide, fenced, a, b, false)
}

func integerAffineSumModuleWithHelper(wide, fenced bool, a, b uint64, helper bool) []byte {
	typ, c, add, mul, lt := wasm.I32, byte(0x41), byte(0x6a), byte(0x6c), byte(0x49)
	if wide {
		typ, c, add, mul, lt = wasm.I64, 0x42, 0x7c, 0x7e, 0x54
	}
	valueType := byte(0x7f)
	if wide {
		valueType = 0x7e
	}
	constant := func(v uint64) []byte {
		if wide {
			return append([]byte{c}, wasmtest.SLEB64(int64(v))...)
		}
		return append([]byte{c}, wasmtest.SLEB32(int32(v))...)
	}
	body := []byte{0x20, 1, 0x03, 0x40, 0x20, 1}
	if fenced {
		body = append(body, 0x02, valueType, 0xfe, 3, 0)
	}
	body = append(body, 0x20, 2)
	if helper {
		body = append(body, 0x10, 0)
	} else {
		body = append(body, constant(a)...)
		body = append(body, mul)
		body = append(body, constant(b)...)
		body = append(body, add)
	}
	if fenced {
		body = append(body, 0x0b)
	}
	body = append(body, add, 0x21, 1, 0x20, 2, c, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, lt, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	code := append([]byte{1, 1, valueType}, body...)
	ft := wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ, typ})
	types, functions, bodies := [][]byte{ft}, [][]byte{wasmtest.ULEB(0)}, [][]byte{append(wasmtest.ULEB(uint32(len(code))), code...)}
	index := uint32(0)
	if helper {
		index = 1
		types = append(types, wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))
		leaf := []byte{0x20, 0}
		leaf = append(leaf, constant(a)...)
		leaf = append(leaf, mul)
		leaf = append(leaf, constant(b)...)
		leaf = append(leaf, add, 0x0b)
		functions = [][]byte{wasmtest.ULEB(1), wasmtest.ULEB(0)}
		bodies = append([][]byte{wasmtest.Code(leaf)}, bodies...)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(functions...)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, index))), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestIntegerAffineSumPublicExecution(t *testing.T) {
	testIntegerAffineSum(t, false)
}

func TestIntegerAffineSumPureHelperPublicExecution(t *testing.T) {
	testIntegerAffineSum(t, true)
}

func testIntegerAffineSum(t *testing.T, helper bool) {
	for _, wide := range []bool{false, true} {
		for _, fenced := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					for _, coefficients := range [][2]uint64{{17, 3}, {0, 9}, {1, 0}, {^uint64(0), 7}, {0x8000000000000001, 0xabcdef0123456789}} {
						a, b := coefficients[0], coefficients[1]
						t.Run(fmt.Sprintf("wide=%t/fenced=%t/opt=%t/abi=%t/a=%x/b=%x", wide, fenced, enabled, regABI, a, b), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureThreads).WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", regABI)
							c, err := cfg.Compile(integerAffineSumModuleWithHelper(wide, fenced, a, b, helper))
							if err != nil {
								t.Fatal(err)
							}
							defer c.Close()
							for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
								in, err := Instantiate(compiled)
								if err != nil {
									t.Fatal(err)
								}
								defer in.Close()
								bounds := []uint64{0, 1, 2, 3, 15, 63, 4096}
								if enabled {
									if wide {
										bounds = append(bounds, 0x7fffffffffffffff, 0x8000000000000000, ^uint64(0))
									} else {
										bounds = append(bounds, 0x7fffffff, 0x80000000, 0xffffffff)
									}
								}
								for _, n := range bounds {
									seed := uint64(0xfedcba9876543210)
									prefix := seed
									mask := ^uint64(0)
									if !wide {
										mask = 0xffffffff
										prefix &= mask
									}
									k := n
									if k == 0 {
										k = 1
									}
									N := new(big.Int).SetUint64(n)
									triangular := new(big.Int).Mul(N, new(big.Int).Sub(new(big.Int).Set(N), big.NewInt(1)))
									triangular.Quo(triangular, big.NewInt(2))
									expected := new(big.Int).Mul(triangular, new(big.Int).SetUint64(a&mask))
									expected.Add(expected, new(big.Int).Mul(new(big.Int).SetUint64(k), new(big.Int).SetUint64(b&mask)))
									expected.Add(expected, new(big.Int).SetUint64(seed&mask))
									expected.And(expected, new(big.Int).SetUint64(mask))
									got, err := in.Invoke("sum", n, seed)
									if err != nil || len(got) != 3 || got[0] != prefix || got[1] != expected.Uint64() || got[2] != k {
										t.Fatalf("n=%x got=%v err=%v want=[%x %x %x]", n, got, err, prefix, expected.Uint64(), k)
									}
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestIntegerAffineSumPreservesStatefulHelperCalls(t *testing.T) {
	m, err := wasm.DecodeModule(integerAffineSumModuleWithHelper(false, false, 3, 1, true))
	if err != nil {
		t.Fatal(err)
	}
	leaf := append([]byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0}, m.Code[0].BodyBytes...)
	caller := append([]byte{1, 1, 0x7f}, m.Code[1].BodyBytes...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(0), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 1, 0x41, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 1), wasmtest.ExportEntry("calls", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(leaf), append(wasmtest.ULEB(uint32(len(caller))), caller...), wasmtest.Code([]byte{0x23, 0, 0x0b}))),
	)
	for _, inline := range []bool{false, true} {
		for _, sum := range []bool{false, true} {
			c, err := NewRuntimeConfig().WithOptimization("inline", inline).WithOptimization("integer-sum-loop", sum).Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
				in, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for _, n := range []uint64{0, 1, 4, 63} {
					got, err := in.Invoke("sum", n, 7)
					k := n
					if k == 0 {
						k = 1
					}
					want := uint64(7) + 3*n*(n-1)/2 + k
					if err != nil || len(got) != 3 || got[0] != 7 || got[1] != want || got[2] != k {
						t.Fatalf("n=%d sum=%v err=%v want=%d", n, got, err, want)
					}
				}
				got, err := in.Invoke("calls")
				if err != nil || len(got) != 1 || got[0] != 69 {
					t.Fatalf("inline=%t sum=%t calls=%v err=%v", inline, sum, got, err)
				}
			}
		}
	}
}
