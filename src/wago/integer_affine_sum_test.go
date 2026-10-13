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
	body = append(body, constant(a)...)
	body = append(body, mul)
	body = append(body, constant(b)...)
	body = append(body, add)
	if fenced {
		body = append(body, 0x0b)
	}
	body = append(body, add, 0x21, 1, 0x20, 2, c, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, lt, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	code := append([]byte{1, 1, valueType}, body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ, typ}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
}

func TestIntegerAffineSumPublicExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, fenced := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					for _, coefficients := range [][2]uint64{{17, 3}, {0, 9}, {1, 0}, {^uint64(0), 7}, {0x8000000000000001, 0xabcdef0123456789}} {
						a, b := coefficients[0], coefficients[1]
						t.Run(fmt.Sprintf("wide=%t/fenced=%t/opt=%t/abi=%t/a=%x/b=%x", wide, fenced, enabled, regABI, a, b), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", regABI)
							c, err := cfg.Compile(integerAffineSumModule(wide, fenced, a, b))
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
