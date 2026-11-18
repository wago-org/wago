//go:build linux && amd64

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCAllocationInTypedReferenceBlock(t *testing.T) {
	for _, opcode := range []byte{0x02, 0x03, 0x04, 0x1f} {
		for _, reference := range []byte{0x63, 0x64} {
			t.Run(fmt.Sprintf("op%x/ref%x", opcode, reference), func(t *testing.T) {
				types := [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}
				for len(types) <= 106 {
					types = append(types, []byte{0x5f, 0x01, 0x7f, 0x00})
				}
				var body []byte
				if opcode == 0x04 {
					body = append(body, 0x41, 0x01)
				}
				body = append(body, opcode, reference, 0xea, 0x00)
				if opcode == 0x1f {
					body = append(body, 0x00)
				}
				body = append(body, 0x41, 0x07, 0xfb, 0x00, 0x6a) // struct.new 106
				if opcode == 0x04 {
					body = append(body, 0x05, 0x41, 0x07, 0xfb, 0x00, 0x6a)
				}
				body = append(body, 0x0b, 0xfb, 0x02, 0x6a, 0x00, 0x0b)
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(types...)),
					wasmtest.Section(3, wasmtest.Vec([]byte{0})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
				)
				compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				if status := compiled.GCNativeRootAdmission(); !status.Required || !status.Exact {
					t.Fatalf("typed block lost exact GC roots: %+v", status)
				}
				instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 32 << 20, NurseryBytes: 1 << 20}})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				results, err := instance.Invoke("run")
				if err != nil || len(results) != 1 || results[0] != 7 {
					t.Fatalf("run = %v, %v; want [7]", results, err)
				}
			})
		}
	}
}
