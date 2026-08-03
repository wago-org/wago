package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestAbstractGCRefNullGlobalInitializers(t *testing.T) {
	requireCompleteCore3Backend(t)
	globals := make([][]byte, 0, 4)
	for _, heap := range []byte{byte(wasm.HeapEq), byte(wasm.HeapI31), byte(wasm.HeapStruct), byte(wasm.HeapArray)} {
		globals = append(globals, []byte{heap, 0x01, 0xd0, heap, 0x0b}) // (global (mut <heap>ref) (ref.null <heap>))
	}
	module := wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(globals...)))
	compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(module)
	if err != nil {
		t.Fatalf("compile abstract ref.null globals: %v", err)
	}
	defer compiled.Close()
	if got := len(compiled.Globals); got != len(globals) {
		t.Fatalf("compiled global count = %d, want %d", got, len(globals))
	}
	loaded := roundTripCompiled(t, compiled)
	defer loaded.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate abstract ref.null globals: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
}
