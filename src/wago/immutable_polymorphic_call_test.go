//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Eight distinct arguments/results stress both register banks while an unrelated
// dirty local remains below the call operands. Runtime table entries select two
// different callees; two more entries are null and out-of-bounds indices trap.
func polymorphicWideIntegerTableModule(n int, exported bool) []byte {
	params := []wasm.ValType{wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64}
	callerParams := append([]wasm.ValType{wasm.I32}, params...)
	results := append([]wasm.ValType{wasm.I64}, params[:n]...)
	caller := []byte{1, 1, 0x7e, 0x42}
	caller = append(caller, wasmtest.SLEB64(777)...)
	caller = append(caller, 0x21, 9, 0x20, 9)
	for i := byte(1); i <= 8; i++ {
		caller = append(caller, 0x20, i)
	}
	caller = append(caller, 0x20, 0, 0x11, 1, 0, 0x0b)
	bodies := [][]byte{append(wasmtest.ULEB(uint32(len(caller))), caller...)}
	for target := 0; target < 2; target++ {
		body := []byte{}
		for i := byte(0); int(i) < n; i++ {
			arg := i
			if target == 1 {
				arg = 7 - i
			}
			body = append(body, 0x20, arg, 0x42)
			body = append(body, wasmtest.SLEB64(int64((target+1)*100)+int64(i))...)
			body = append(body, 0x7c)
		}
		body = append(body, 0x0b)
		bodies = append(bodies, wasmtest.Code(body))
	}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, 0)}
	if exported {
		exports = append(exports, wasmtest.ExportEntry("table", 1, 0))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(callerParams, results), wasmtest.FuncType(params, params[:n]))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 4})),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 2, 1, 2})),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestImmutablePolymorphicWideIntegerExecution(t *testing.T) {
	for _, exported := range []bool{false, true} {
		for _, n := range []int{1, 2, 8} {
			for _, abi := range []bool{false, true} {
				for _, on := range []bool{false, true} {
					t.Run(fmt.Sprintf("exported%t/results%d/abi%t/on%t", exported, n, abi, on), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithOptimization("reg-abi", abi)
						if runtime.GOARCH == "arm64" {
							cfg = cfg.WithOptimization("immutable-table", on)
						}
						c, err := cfg.Compile(polymorphicWideIntegerTableModule(n, exported))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()

						for ci, compiled := range []*Compiled{c} {
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							for _, idx := range []uint32{0, 1, 1, 0, 2, 3, 4, 0xffffffff} {
								args := []uint64{uint64(idx) | 0xfedcba9800000000, 11, 0xffffffffffffffff, 33, 0x8000000000000000, 55, 66, 0x123456789abcdef0, 88}
								got, err := in.Invoke("run", args...)
								if idx >= 2 {
									var trap *TrapError
									if !errors.As(err, &trap) || trap.Code != TrapIndirectOutOfBounds {
										t.Fatalf("artifact%d idx%d: expected indirect trap, got %v", ci, idx, err)
									}
									continue
								}
								if err != nil || len(got) != n+1 {
									t.Fatalf("idx%d: %v %v", idx, got, err)
								}
								if got[0] != 777 {
									t.Fatalf("live prefix lost: %v", got)
								}
								for i := 0; i < n; i++ {
									arg := i + 1
									if idx == 1 {
										arg = 8 - i
									}
									want := args[arg] + uint64((idx+1)*100) + uint64(i)
									if got[i+1] != want {
										t.Fatalf("artifact%d idx%d result%d: %x, want %x", ci, idx, i, got[i+1], want)
									}
								}
							}
						}
					})
				}
			}
		}

	}

}

func TestMonomorphicCallIndirectRegisterABIModes(t *testing.T) {
	for _, abi := range []bool{false, true} {
		t.Run(fmt.Sprintf("abi%t", abi), func(t *testing.T) {
			c, err := NewRuntimeConfig().WithOptimization("reg-abi", abi).Compile(callIndirectModule(3, 1))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			for _, compiled := range []*Compiled{c} {
				in, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				got, err := in.Invoke("caller", 0, 10, 3)
				if err != nil || len(got) != 1 || got[0] != 13 {
					t.Fatalf("call returned %v, %v; want 13", got, err)
				}
				for _, idx := range []uint64{1, 3} {
					_, err := in.Invoke("caller", idx, 10, 3)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapIndirectOutOfBounds {
						t.Fatalf("idx%d: expected indirect trap, got %v", idx, err)
					}
				}
			}
		})
	}
}
