//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64ImmediateReferenceTestsAndCasts(t *testing.T) {
	for _, heap := range []wasm.AbsHeapType{wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone} {
		for _, nullable := range []bool{false, true} {
			for _, null := range []bool{false, true} {
				for _, cast := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/nullable%t/null%t/cast%t", heap, nullable, null, cast), func(t *testing.T) {
						body := []byte{0x41, 7, 0xfb, 0x1c}
						if null {
							body = []byte{0xd0, byte(wasm.HeapAny)}
						}
						op := byte(20)
						if cast {
							op = 22
						}
						if nullable {
							op++
						}
						body = append(body, 0xfb, op, byte(heap))
						if cast {
							body = append(body, 0x1a, 0x41, 1)
						}
						body = append(body, 0x0b)
						source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
						c, err := NewRuntimeConfig().Compile(source)
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()

						want := !null && (heap == wasm.HeapAny || heap == wasm.HeapEq || heap == wasm.HeapI31) || null && nullable
						for _, candidate := range []*Compiled{c} {
							if candidate.nativeGCABIRequirement() != 0 || candidate.needsRuntimeGCCollectorDomain() {
								t.Fatal("immediate-only module acquired collector ABI/domain")
							}
							in, err := Instantiate(candidate, InstantiateOptions{})
							if err != nil {
								t.Fatal(err)
							}
							got, err := in.Invoke("run")
							in.Close()
							if cast && !want {
								if err == nil {
									t.Fatal("invalid immediate cast succeeded")
								}
								continue
							}
							expected := uint64(0)
							if want {
								expected = 1
							}
							if err != nil || len(got) != 1 || got[0] != expected {
								t.Fatalf("immediate reference result=%v,%v, want %d", got, err, expected)
							}
						}
					})
				}
			}
		}
	}
}
