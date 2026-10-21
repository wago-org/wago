//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func indexedWaitLargeMemoryModule(address64 bool, index uint32, sub byte) []byte {
	address, expected, limits, align := wasm.I32, wasm.I32, byte(3), byte(2)
	if address64 {
		address, limits = wasm.I64, 7
	}
	if sub == 2 {
		expected, align = wasm.I64, 3
	}
	memories := make([][]byte, index+1)
	for i := range memories {
		memories[i] = []byte{1, 0, 0}
	}
	memories[index] = []byte{limits, 1, 1}
	body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, sub, align | 0x40}
	body = append(body, wasmtest.ULEB(index)...)
	body = append(body, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{address, expected, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec(memories...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestIndexedAtomicWaitLargeMemoryIndexes(t *testing.T) {
	// 24-byte directory entries:1365 is the last scaled ARM64 LDR offset;
	//1366 needs explicit address formation.4095 is the configured index ceiling.
	for _, index := range []uint32{1365, 1366, 4095} {
		for _, address64 := range []bool{false, true} {
			for _, sub := range []byte{1, 2} {
				t.Run(fmt.Sprintf("index%d/addr64%t/op%d", index, address64, sub), func(t *testing.T) {
					defer func() {
						if recovered := recover(); recovered != nil {
							t.Errorf("valid indexed wait failed to compile: %v", recovered)
						}
					}()
					c, err := indexedWaitConfig().WithMaxMemoriesPerModule(MaxMemoriesPerModuleLimit).Compile(indexedWaitLargeMemoryModule(address64, index, sub))
					if err != nil {
						t.Fatal(err)
					}
					c.Close()
				})
			}
		}
	}
}
