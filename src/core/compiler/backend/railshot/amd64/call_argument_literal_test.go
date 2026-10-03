//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestMixedCallPreservesIntegerArgumentsWhileLoadingFloatLiterals(t *testing.T) {
	for _, n := range []int{1, 4, 8} {
		for _, wide := range []bool{false, true} {
			for _, tail := range []bool{false, true} {
				gpTypes := make([]wasm.ValType, n)
				for i := range gpTypes {
					gpTypes[i] = wasm.I64
				}
				fpType, constant, raw := wasm.F32, byte(0x43), uint64(0x7fc12345)
				if wide {
					fpType, constant, raw = wasm.F64, 0x44, 0x7ff8123456789abc
				}
				body := []byte{0}
				for i := range gpTypes {
					body = append(body, 0x20, byte(i), 0x42, byte(i+17), 0x85, 0x21, byte(i))
				}
				for i := range gpTypes {
					body = append(body, 0x20, byte(i))
				}
				body = append(body, constant)
				if wide {
					body = binary.LittleEndian.AppendUint64(body, raw)
				} else {
					body = binary.LittleEndian.AppendUint32(body, uint32(raw))
				}
				if tail {
					body = append(body, 0x12, 1)
				} else {
					body = append(body, 0x10, 1)
					for i := range gpTypes {
						body = append(body, 0x20, byte(i), 0x42, byte(i+1), 0x89, 0x85)
					}
				}
				body = append(body, 0x0b)
				helper := []byte{0, 0x20, byte(n)}
				if wide {
					helper = append(helper, 0xbd) // i64.reinterpret_f64
				} else {
					helper = append(helper, 0xbc, 0xad) // i32.reinterpret_f32; i64.extend_i32_u
				}
				for i := range gpTypes {
					helper = append(helper, 0x20, byte(i), 0x42, byte(i+1), 0x89, 0x85)
				}
				helper = append(helper, 0x0b)
				params := append(append([]wasm.ValType(nil), gpTypes...), fpType)
				m := modFuncs(t, funcDef{gpTypes, []wasm.ValType{wasm.I64}, body}, funcDef{params, []wasm.ValType{wasm.I64}, helper})
				if err := wasm.ValidateModule(m); err != nil {
					t.Fatal(err)
				}
				for _, pool := range []bool{false, true} {
					for _, lazy := range []bool{false, true} {
						t.Run(fmt.Sprintf("gp%d/wide%v/tail%v/pool%v/lazy%v", n, wide, tail, pool, lazy), func(t *testing.T) {
							cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"inline": false, "v128-const-cache": pool, "stack-reg": lazy}})
							if err != nil {
								t.Fatal(err)
							}
							defer cm.CodeImage.Close()
							for _, seed := range []uint64{0, 1, 0x123456789abcdef0, ^uint64(0)} {
								args := make([]uint64, n)
								for i := range args {
									args[i] = bits.RotateLeft64(seed, i*5) ^ uint64(i+1)
								}
								want := raw
								if tail {
									for i, v := range args {
										want ^= bits.RotateLeft64(v^uint64(i+17), i+1)
									}
								}
								if got := runCompiledAmd64u(t, cm, args...); got != want {
									t.Fatalf("seed=%x got=%x want=%x", seed, got, want)
								}
							}
						})
					}
				}
			}
		}
	}
}
