//go:build arm64

package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestHostI32ResultCanonicalBeforeMemoryAddressUseARM64(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(portableFuncImportEntry("env", "address", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", 0, 1),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x10, 0x00, // call host address
			0x28, 0x02, 0x00, // i32.load align=4 offset=0
			0x0b,
		}))),
	)
	compiled, err := NewRuntimeConfig().Compile(module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.address": HostFunc(func(_ HostModule, _, results []uint64) {
			results[0] = 1<<32 | 4 // an i32 host result with adversarial upper bits
		}),
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	binary.LittleEndian.PutUint32(in.Memory().UnsafeBytes()[4:], 42)
	got, err := in.Invoke("f")
	if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("f() = %v, %v; want [42], nil", got, err)
	}
}
