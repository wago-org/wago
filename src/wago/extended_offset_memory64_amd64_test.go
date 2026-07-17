//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func localI64GlobalMemory64OffsetModule() []byte {
	memory := append([]byte{0x05}, uleb64(1)...)
	memory = append(memory, uleb64(2)...)
	segment := []byte{0x00, 0x23, 0x00, 0x42, 0x01, 0x7c, 0x0b, 0x01, 'x'}
	return wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec(memory)),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, false, []byte{0x42, 0x03, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(11, wasmtest.Vec(segment)),
	)
}

func TestMemory64ActiveOffsetUsesLocalImmutableI64Global(t *testing.T) {
	compiled, err := compileStagedMemory64(localI64GlobalMemory64OffsetModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	blob, err := marshalCompiled(compiled)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	// Private staged execution bits are intentionally not serialized.
	loaded.memoryDir.stagedMemory64 = true
	in, err := Instantiate(&loaded, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := in.Memory().Bytes()[4]; got != 'x' {
		t.Fatalf("memory[4] = %d, want %d", got, 'x')
	}
}
