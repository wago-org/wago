//go:build linux && amd64 && !tinygo && wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func signalMultiMemoryProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x01},
			[]byte{0x01, 0x01, 0x01},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("load1", 0, 0),
			wasmtest.ExportEntry("memory1", 2, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x42, 0x01, 0x00, 0x0b}),
		)),
	)
}

func signalMemoryImporterModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x01, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}),
		)),
	)
}

func signalMemory64LoadModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x05, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}),
		)),
	)
}

func TestCore3SignalsMultiMemoryUsesGuardedExportableMappings(t *testing.T) {
	cfg := NewRuntimeConfig().
		WithCoreFeatures(CoreFeaturesV3).
		WithBoundsChecks(BoundsChecksSignalsBased)
	producerCode, err := Compile(cfg, signalMultiMemoryProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	producer, err := Instantiate(producerCode, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if result, err := producer.Invoke("load1", I32(0)); err != nil || AsI32(result[0]) != 0 {
		t.Fatalf("indexed memory load = %v, %v", result, err)
	}
	if _, err := producer.Invoke("load1", I32(65536)); err == nil {
		t.Fatal("indexed memory out-of-bounds load did not trap")
	}

	exported, err := producer.ExportedMemory("memory1")
	if err != nil {
		t.Fatal(err)
	}
	if guarded, _ := exported.importShape(); !guarded {
		t.Fatal("nonzero owned memory is not guard-backed for later memory-0 import")
	}
	consumerCode, err := Compile(cfg, signalMemoryImporterModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	consumer, err := Instantiate(consumerCode, InstantiateOptions{Imports: Imports{"env.memory": exported}})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if result, err := consumer.Invoke("load", I32(0)); err != nil || AsI32(result[0]) != 0 {
		t.Fatalf("re-imported memory load = %v, %v", result, err)
	}
}

func TestCore3SignalsMemory64RetainsFullWidthExplicitCheck(t *testing.T) {
	cfg := NewRuntimeConfig().
		WithCoreFeatures(CoreFeaturesV3).
		WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := Compile(cfg, signalMemory64LoadModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if result, err := in.Invoke("load", I64(0)); err != nil || AsI32(result[0]) != 0 {
		t.Fatalf("memory64 in-bounds load = %v, %v", result, err)
	}
	for _, address := range []uint64{65536, 1 << 32, ^uint64(0)} {
		if _, err := in.Invoke("load", I64(int64(address))); err == nil {
			t.Fatalf("memory64 address %#x did not trap", address)
		}
	}
}
