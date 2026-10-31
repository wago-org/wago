//go:build (linux || darwin || windows) && arm64 && !tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func bulkCopySpanModule(constant int) []byte {
	body := []byte{0x20, 0, 0x20, 1}
	if constant < 0 {
		body = append(body, 0x20, 2)
	} else {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(constant))...)
	}
	body = append(body, 0xfc, 10, 0, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestBulkCopyVectorAndWideSpans(t *testing.T) {
	constants := []int{-1}
	for n := 0; n <= 65; n++ {
		constants = append(constants, n)
	}
	for _, constant := range constants {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("constant=%d/on=%t", constant, enabled), func(t *testing.T) {
				c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).
					WithOptimization("bulk-copy-vector", enabled).
					WithOptimization("bulk-memory-loops", enabled).Compile(bulkCopySpanModule(constant))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				mem := in.Memory().UnsafeBytes()
				before := make([]byte, len(mem))
				want := make([]byte, len(mem))
				for i := range before {
					before[i] = byte(i*29 + i>>8)
				}
				lengths := []int{0, 1, 15, 16, 17, 31, 32, 33, 63, 64, 65, 127, 128, 129, 255, 256, 257, 1023, 4096, 32767, 32768, 32769, 65535, 65536}
				if constant >= 0 {
					lengths = []int{constant}
				}
				for _, n := range lengths {
					for alignment := 0; alignment < 16; alignment++ {
						for _, delta := range []int{-65, -33, -16, -1, 0, 1, 15, 16, 31, 33, 63, 65, 129} {
							src, dst := 256+alignment, 256+alignment+delta
							if src+n > len(mem) || dst+n > len(mem) {
								continue
							}
							copy(mem, before)
							copy(want, before)
							copy(want[dst:dst+n], want[src:src+n])
							_, err := in.Invoke("copy", uint64(dst)|0xaabbccdd00000000, uint64(src)|0xaabbccdd00000000, uint64(n)|0xaabbccdd00000000)
							if err != nil || !bytes.Equal(mem, want) {
								t.Fatalf("n=%d src=%d dst=%d: err=%v memoryEqual=%t", n, src, dst, err, bytes.Equal(mem, want))
							}
						}
						for _, pair := range [][2]int{{len(mem) - n, 0}, {0, len(mem) - n}} {
							dst, src := pair[0], pair[1]
							copy(mem, before)
							copy(want, before)
							copy(want[dst:dst+n], want[src:src+n])
							if _, err := in.Invoke("copy", uint64(dst), uint64(src), uint64(n)); err != nil || !bytes.Equal(mem, want) {
								t.Fatalf("boundary n=%d src=%d dst=%d: %v", n, src, dst, err)
							}
						}
					}
					for _, pair := range [][2]int{{len(mem) - n + 1, 0}, {0, len(mem) - n + 1}} {
						copy(mem, before)
						_, err := in.Invoke("copy", uint64(pair[0]), uint64(pair[1]), uint64(n))
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds || !bytes.Equal(mem, before) {
							t.Fatalf("n=%d invalid span %v: err=%v memoryEqual=%t", n, pair, err, bytes.Equal(mem, before))
						}
					}
				}
			})
		}
	}
}
