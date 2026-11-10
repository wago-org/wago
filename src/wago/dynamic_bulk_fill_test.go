//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func dynamicBulkFillModule(shared bool, index byte) []byte {
	memories := [][]byte{{1, 1, 1}, {1, 1, 1}}
	memories = memories[:index+1]
	if shared {
		memories[index][0] = 3
	}
	body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 11, index, 0x20, 1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec(memories...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fill", 0, 0), wasmtest.ExportEntry("memory", 2, uint32(index)))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestDynamicBulkFillBoundaries(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, abi := range []bool{false, true} {
				t.Run(fmt.Sprintf("shared%t/index%d/abi%t", shared, index, abi), func(t *testing.T) {
					features := CoreFeaturesV2
					if index != 0 {
						features |= CoreFeatureMultiMemory
					}

					if shared {
						features |= CoreFeatureThreads
					}
					if !platformCoreFeatures().IsEnabled(features) {
						t.Skip("memory shape is unavailable on this platform")
					}
					c, err := NewRuntimeConfig().WithCoreFeatures(features).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", abi).Compile(dynamicBulkFillModule(shared, index))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					cases := []*Compiled{c}
					if !guardPageBuilt {

						cases = append(cases, c)
					}
					for _, compiled := range cases {
						in, err := Instantiate(compiled)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						memory, err := in.ExportedMemory("memory")
						if err != nil {
							t.Fatal(err)
						}
						mem := memory.UnsafeBytes()
						before := bytes.Repeat([]byte{0x5a}, len(mem))
						want := bytes.Clone(before)
						for _, n := range []int{0, 1, 7, 8, 15, 16, 31, 32, 33, 63, 64, 65, 95, 96, 127, 128, 129, 255, 256, 257, 1023, 1024, 4096, 65535, 65536} {
							for _, dst := range []int{0, 1, 7, 15, 31, len(mem) - n} {
								if dst+n > len(mem) {
									continue
								}
								for _, value := range []uint32{0, 0x1ff, 0x80000042} {
									copy(mem, before)
									copy(want, before)
									for i := dst; i < dst+n; i++ {
										want[i] = byte(value)
									}
									got, err := in.Invoke("fill", uint64(dst)|0xfedcba9800000000, uint64(value)|0xfedcba9800000000, uint64(n)|0xfedcba9800000000)
									if err != nil || len(got) != 1 || uint32(got[0]) != value || !bytes.Equal(mem, want) {
										t.Fatalf("dst%d/n%d/value%x: result%v err%v memoryEqual%t", dst, n, value, got, err, bytes.Equal(mem, want))
									}
								}
							}
							copy(mem, before)
							_, err := in.Invoke("fill", uint64(len(mem)-n+1), 0xff, uint64(n))
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds || !bytes.Equal(mem, before) {
								t.Fatalf("n%d: missing pre-write bounds trap: %v", n, err)
							}
						}
					}
				})
			}
		}
	}
}
