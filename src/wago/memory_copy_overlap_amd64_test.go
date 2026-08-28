//go:build amd64 && !tinygo

package wago

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestMemoryCopyBackwardVectorTiers(t *testing.T) {
	body := []byte{
		0x41, 0x01, // i32.const 1 (dst)
		0x41, 0x00, // i32.const 0 (src)
		0x20, 0x00, // local.get 0 (n)
		0xfc, 0x0a, 0x00, 0x00, // memory.copy 0 0
		0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copy", 0, 0),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)

	for _, n := range []int{96, 127, 128, 129, 256, 1023, 1024, 1025, 4096} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
			if err != nil {
				t.Fatal(err)
			}
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()

			memory := instance.Memory().UnsafeBytes()
			for i := 0; i <= n; i++ {
				memory[i] = byte(i*31 + 7)
			}
			want := append([]byte(nil), memory[:n+1]...)
			copy(want[1:], want[:n])
			if _, err := instance.Invoke("copy", I32(int32(n))); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(memory[:n+1], want) {
				t.Fatalf("memory.copy overlap mismatch for n=%d", n)
			}
		})
	}
}
