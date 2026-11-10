//go:build linux && amd64 && !tinygo

package wago

import (
	"bytes"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestConstantStoreAddressBoundsAndTrapOrder(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, size := range []int{1, 2, 4, 8} {
			for _, olderTrap := range []bool{false, true} {
				for _, span := range [][2]uint32{{0, 0}, {31, 0}, {65536 - uint32(size), 0}, {65537 - uint32(size), 0}, {0xffffffff, 0}, {0, 0xffffffff}, {65520, 7}} {
					address, offset := span[0], span[1]
					var body []byte
					if olderTrap {
						body = append(body, 0x41)
						body = append(body, wasmtest.SLEB32(65536)...)
						body = append(body, 0x2d, 0, 0)
					}
					body = append(body, 0x41)
					body = append(body, wasmtest.SLEB32(int32(address))...)
					body = append(body, 0x20, 0)
					op := byte(0x37)
					switch size {
					case 1:
						op = 0x3c
					case 2:
						op = 0x3d
					case 4:
						op = 0x3e
					}
					body = append(body, op, 0)
					body = append(body, wasmtest.ULEB(offset)...)
					if olderTrap {
						body = append(body, 0x1a)
					}
					body = append(body, 0x0b)
					module := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil))),
						wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
						wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("store", 0, 0))),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
					)
					c, err := NewRuntimeConfig().WithOptimization("memory-min-bounds", enabled).Compile(module)
					if err != nil {
						t.Fatal(err)
					}
					in, err := Instantiate(c)
					if err != nil {
						c.Close()
						t.Fatal(err)
					}
					mem := in.Memory().UnsafeBytes()
					for i := range mem {
						mem[i] = 0x5a
					}
					want := bytes.Clone(mem)
					start := uint64(address) + uint64(offset)
					valid := !olderTrap && start+uint64(size) <= uint64(len(mem))
					const value = uint64(0xfedcba9876543210)
					if valid {
						for j := 0; j < size; j++ {
							want[int(start)+j] = byte(value >> (8 * j))
						}
					}
					_, err = in.Invoke("store", value)
					var trap *TrapError
					equal := bytes.Equal(mem, want)
					in.Close()
					c.Close()
					if valid && err != nil || !valid && (!errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds) || !equal {
						t.Fatalf("on=%t size=%d oldTrap=%t span=%v err=%v memoryEqual=%t", enabled, size, olderTrap, span, err, equal)
					}
				}
			}
		}
	}
}
