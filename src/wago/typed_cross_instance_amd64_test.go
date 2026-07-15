//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func typedCrossInstanceProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}))),
	)
}

func typedCrossInstanceConsumerModule() []byte {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	importEntry = append(importEntry, 0x00)
	importEntry = append(importEntry, wasmtest.ULEB(1)...)
	declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0xd2, 0x00, // ref.func imported function 0
			0x14, 0x01, // call_ref shifted structural type 1
			0x0b,
		}))),
	)
}

func TestStagedTypedCrossInstanceCallRefRetainsProducer(t *testing.T) {
	producerCompiled := stagedTypedStorageCompile(t, typedCrossInstanceProducerModule())
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	export, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatalf("export producer function: %v", err)
	}
	consumerCompiled := stagedTypedStorageCompile(t, typedCrossInstanceConsumerModule())
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}})
	if err != nil {
		t.Fatalf("instantiate typed call_ref consumer: %v", err)
	}
	if got := tableTestCallI32(t, consumer, "run", I32(41)); got != 42 {
		t.Fatalf("cross-instance call_ref result = %d, want 42", got)
	}

	if err := producer.Close(); err != nil {
		t.Fatalf("logical producer close: %v", err)
	}
	producer.lifeMu.Lock()
	producerReleased := producer.resourcesClosed
	producer.lifeMu.Unlock()
	if producerReleased {
		t.Fatal("typed cross-instance descriptor did not retain producer resources")
	}
	if got := tableTestCallI32(t, consumer, "run", I32(99)); got != 100 {
		t.Fatalf("call_ref after logical producer close = %d, want 100", got)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	producer.lifeMu.Lock()
	producerReleased = producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !producerReleased {
		t.Fatal("typed cross-instance producer remained retained after consumer close")
	}
}

func typedLocalCallRefModule() []byte {
	declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd2, 0x00, 0x14, 0x01, 0x0b}),
		)),
	)
}

func BenchmarkStagedTypedCrossInstanceCallRef(b *testing.B) {
	producerCompiled := stagedTypedStorageCompile(b, typedCrossInstanceProducerModule())
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("f")
	if err != nil {
		b.Fatal(err)
	}
	consumerCompiled := stagedTypedStorageCompile(b, typedCrossInstanceConsumerModule())
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}})
	if err != nil {
		b.Fatal(err)
	}
	defer consumer.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := consumer.Invoke("run", I32(41)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestStagedTypedSnapshotPolicyRejectsCodecRoundTrip(t *testing.T) {
	compiled := stagedTypedStorageCompile(t, typedLocalCallRefModule())
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal typed call_ref module: %v", err)
	}
	if _, err := Load(blob); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("public load of staged typed artifact = %v, want fail-closed required-feature error", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload typed call_ref module: %v", err)
	}
	defer loaded.Close()

	for _, c := range []*Compiled{compiled, &loaded} {
		_, err := Capture(c, SnapshotOptions{})
		if err == nil || !strings.Contains(err.Error(), "typed function references") {
			t.Fatalf("Capture typed call_ref module = %v, want explicit descriptor snapshot rejection", err)
		}
	}
}
