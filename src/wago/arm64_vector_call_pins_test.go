//go:build (linux || darwin || windows) && arm64 && !tinygo

package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestARM64VectorLocalAcrossHostCalls(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "indirect"}[indirect], func(t *testing.T) {
			value := [16]byte{1, 3, 5, 7, 9, 11, 13, 15, 2, 4, 6, 8, 10, 12, 14, 16}
			body := []byte{1, 1, 0x7b, 0xfd, 0x0c}
			body = append(body, value[:]...)
			body = append(body, 0x21, 0) // local.set 0
			if indirect {
				body = append(body, 0x41, 0, 0x11, 0, 0)
			} else {
				body = append(body, 0x10, 0)
			}
			// Read both halves after the host transition.
			body = append(body, 0x20, 0, 0xfd, 0x1d, 0, 0x20, 0, 0xfd, 0x1d, 1, 0x85, 0x0b)
			imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0, 0)
			sections := [][]byte{
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
				wasmtest.Section(2, wasmtest.Vec(imp)),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			}
			if indirect {
				sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})))
			}
			sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))))
			if indirect {
				sections = append(sections, wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))))
			}
			code := append(wasmtest.ULEB(uint32(len(body))), body...)
			sections = append(sections, wasmtest.Section(10, wasmtest.Vec(code)))
			compiled, err := Compile(NewRuntimeConfig(), wasmtest.Module(sections...))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			imports := NewImports()
			calls := 0
			imports.HostFunc("env", "f", func() { calls++ })
			instance, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			want := binary.LittleEndian.Uint64(value[:8]) ^ binary.LittleEndian.Uint64(value[8:])
			if err != nil || len(got) != 1 || got[0] != want || calls != 1 {
				t.Fatalf("result %v, calls %d, err %v; want %#x and one call", got, calls, err, want)
			}
		})
	}
}
