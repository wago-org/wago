//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A reference result type has a prefix followed by a signed heap-type index.
// Type index zero used to remain in the opcode stream and execute as unreachable.
func typedSelectReferenceModule(typeIndex uint32, nullable bool, same bool) []byte {
	types := make([][]byte, typeIndex+1)
	for i := range types {
		types[i] = []byte{0x5f, 0x01, 0x7f, 0x00} // struct (field i32)
	}
	types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	ref := append([]byte{0x64}, wasmtest.SLEB32(int32(typeIndex))...)
	body := append([]byte{0x01, 0x02}, ref...)  // two non-null reference locals
	body = append(body, 0x41, 0x0b, 0xfb, 0x00) // struct.new with field 11
	body = append(body, wasmtest.ULEB(typeIndex)...)
	body = append(body, 0x21, 0x01, 0x41, 0x16, 0xfb, 0x00) // local 1; struct.new with field 22
	body = append(body, wasmtest.ULEB(typeIndex)...)
	body = append(body, 0x21, 0x02, 0x20, 0x01, 0x20)
	if same {
		body = append(body, 0x01)
	} else {
		body = append(body, 0x02)
	}
	body = append(body, 0x20, 0x00, 0x1c, 0x01) // condition; select with one declared result
	if nullable {
		ref[0] = 0x63
	}
	body = append(body, ref...)
	if same {
		body = append(body, 0x20, 0x01, 0xd3) // ref.eq against the original
	} else {
		body = append(body, 0xfb, 0x02) // struct.get field 0
		body = append(body, wasmtest.ULEB(typeIndex)...)
		body = append(body, 0x00)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(typeIndex+1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestTypedSelectReferenceImmediates(t *testing.T) {
	for _, index := range []uint32{0, 130} {
		for _, nullable := range []bool{false, true} {
			for _, same := range []bool{true, false} {
				t.Run(fmt.Sprintf("index=%d/nullable=%t/same=%t", index, nullable, same), func(t *testing.T) {
					compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), typedSelectReferenceModule(index, nullable, same))
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					instance, err := Instantiate(compiled, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer instance.Close()
					for _, condition := range []uint32{0, 1, 0xffffffff} {
						got, err := instance.Invoke("run", I32(int32(condition)))
						want := uint64(11)
						if condition == 0 {
							want = 22
						}
						if same {
							want = 1
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("condition %#x: got %v, error %v; want [%d]", condition, got, err, want)
						}
					}
				})
			}
		}
	}
}
