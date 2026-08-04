//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func arm64DirectTailProducerModule() []byte {
	body := []byte{
		0x20, 0x00, 0x45,
		0x04, 0x7f,
		0x41, 0x07,
		0x05,
		0x20, 0x00, 0x41, 0x01, 0x6b,
		0x12, 0x00,
		0x0b, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func arm64DirectTailConsumerModule() []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	imp = append(imp, byte(wasm.ExternFunc))
	imp = append(imp, wasmtest.ULEB(0)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("nested", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x12, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x41, 0x05, 0x6a, 0x0b}),
		)),
	)
}

func TestTailCallARM64DynamicHostAndCrossInstance(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureTailCall)
	consumerCode, err := Compile(cfg, arm64DirectTailConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()

	host, err := Instantiate(consumerCode, InstantiateOptions{Imports: Imports{
		"env.f": HostFunc(func(_ HostModule, params, results []uint64) { results[0] = params[0] + 1 }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := host.Invoke("run", 41); err != nil || !reflect.DeepEqual(got, []uint64{42}) {
		host.Close()
		t.Fatalf("host tail = %v, %v", got, err)
	}
	if got, err := host.Invoke("nested", 41); err != nil || !reflect.DeepEqual(got, []uint64{47}) {
		host.Close()
		t.Fatalf("nested host tail = %v, %v", got, err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}

	providerCode, err := Compile(cfg, arm64DirectTailProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	provider, err := Instantiate(providerCode, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := provider.ExportedFunc("f")
	if err != nil {
		provider.Close()
		t.Fatal(err)
	}
	consumer, err := Instantiate(consumerCode, InstantiateOptions{Imports: Imports{"env.f": exported}})
	if err != nil {
		provider.Close()
		t.Fatal(err)
	}
	if got, err := consumer.Invoke("run", 1_000_000); err != nil || !reflect.DeepEqual(got, []uint64{7}) {
		consumer.Close()
		provider.Close()
		t.Fatalf("cross tail = %v, %v", got, err)
	}
	if got, err := consumer.Invoke("nested", 1_000_000); err != nil || !reflect.DeepEqual(got, []uint64{12}) {
		consumer.Close()
		provider.Close()
		t.Fatalf("nested cross tail = %v, %v", got, err)
	}
	if err := provider.Close(); err != nil {
		consumer.Close()
		t.Fatal(err)
	}
	if got, err := consumer.Invoke("nested", 10); err != nil || !reflect.DeepEqual(got, []uint64{12}) {
		consumer.Close()
		t.Fatalf("retained cross tail = %v, %v", got, err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
}
