//go:build linux && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestExceptionSnapshotPreservesWideFPLocalsARM64(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), exceptionSnapshotModuleARM64())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	args := make([]uint64, 24)
	for i := range args {
		args[i] = math.Float64bits(float64(i) + 0.125)
	}
	args[8], args[9] = 1<<63, 0x7ff8000000001234
	got, err := in.Invoke("run", args...)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(args) {
		t.Fatalf("results = %d, want %d", len(got), len(args))
	}
	for i := range args {
		if got[i] != args[i] {
			t.Errorf("local %d = %#x, want %#x", i, got[i], args[i])
		}
	}
}

func exceptionSnapshotModuleARM64() []byte {
	const n = 24
	params := make([]wasm.ValType, n)
	for i := range params {
		params[i] = wasm.F64
	}
	body := []byte{0x02, 0x40, 0x1f, 0x40, 1, 0, 0, 0, 0x10, 0, 0x0b, 0x0b}
	for i := 0; i < n; i++ {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(params, params))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x08, 0, 0x0b}), wasmtest.Code(body))),
	)
}
