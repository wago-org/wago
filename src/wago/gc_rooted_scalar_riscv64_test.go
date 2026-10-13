//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"errors"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rootedScalarFunctionBeforeStructModule() []byte {
	body := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x01, 0xfb, 0x02, 0x01, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			[]byte{0x5f, 0x01, 0x7f, 0x00},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarFunctionBeforeStructExecutes(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), rootedScalarFunctionBeforeStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || !slices.Equal(got, []uint64{42}) {
		t.Fatalf("run()=%v err=%v want=[42]", got, err)
	}
}

func TestRV64FreshRootedScalarReceipt(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), typedSelectReferenceModule(130, true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) {
		t.Fatal("fresh rooted GC compiler receipt is missing")
	}
	if err := railshotValidateCompiled(view); err != nil {
		t.Fatal(err)
	}

	mutated := *view
	mutated.code = slices.Clone(view.code)
	mutated.code[len(mutated.code)-1] ^= 1
	if rv64FreshRootedGC(&mutated) {
		t.Fatal("mutated code retained the fresh compiler receipt")
	}
	if err := railshotValidateCompiled(&mutated); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatalf("mutated code admission = %v", err)
	}

	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err == nil {
		_ = loaded.Close()
		t.Fatal("generic rooted RV64 artifact unexpectedly retained a fresh compiler receipt")
	}
}
