//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func constantFillModule(n int, shared bool, index uint32) []byte {
	memories := [][]byte{{1, 1, 2}, {1, 1, 2}}
	memories = memories[:index+1]
	if shared {
		memories[index][0] = 3
	}
	body := []byte{0x20, 0, 0x20, 1, 0x41}
	if n < 64 {
		body = append(body, byte(n))
	} else {
		body = append(body, byte(n)|0x80, 0)
	}
	body = append(body, 0xfc, 11, byte(index), 0x20, 1, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(5, wasmtest.Vec(memories...)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fill", 0, 0), wasmtest.ExportEntry("memory", 2, index))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestBulkFillVectorAllLengths(t *testing.T) {
	for n := 15; n <= 65; n++ {
		for _, shared := range []bool{false, true} {
			for _, index := range []uint32{0, 1} {
				for _, enabled := range []bool{false, true} {
					t.Run(fmt.Sprintf("n=%d/shared=%t/index=%d/vector=%t", n, shared, index, enabled), func(t *testing.T) {
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
						c, err := NewRuntimeConfig().WithCoreFeatures(features).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("bulk-fill-vector", enabled).Compile(constantFillModule(n, shared, index))
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
							mem, err := in.ExportedMemory("memory")
							if err != nil {
								t.Fatal(err)
							}
							bytes := mem.UnsafeBytes()
							for _, value := range []uint64{0, 1, 0x80, 0x1ff, 0xabcdef42, 0xffffffff} {
								for _, dst := range []int{3, len(bytes) - n} {
									for i := range bytes {
										bytes[i] = 0x5a
									}
									got, err := in.Invoke("fill", uint64(dst), value)
									if err != nil || len(got) != 1 || got[0] != value {
										t.Fatalf("value=%x: %v %v", value, got, err)
									}
									for i, b := range bytes {
										want := byte(0x5a)
										if i >= dst && i < dst+n {
											want = byte(value)
										}
										if b != want {
											t.Fatalf("dst=%d byte[%d]=%x want=%x", dst, i, b, want)
										}
									}
								}
							}
							dst := len(bytes) - n + 1
							for i := range bytes {
								bytes[i] = 0x5a
							}
							_, err = in.Invoke("fill", uint64(dst), 0xff)
							var trap *TrapError
							if !errors.As(err, &trap) {
								t.Fatalf("expected bounds trap, got %v", err)
							}
							for i, b := range bytes {
								if b != 0x5a {
									t.Fatalf("write before bounds trap at %d", i)
								}
							}
						}
					})
				}
			}
		}
	}
}
