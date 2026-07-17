//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func referenceEncodingFuncType(refType []byte) []byte {
	out := []byte{0x60, 0x00, 0x01}
	return append(out, refType...)
}

func referenceEncodingProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(referenceEncodingFuncType([]byte{0x70}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x0b}))),
	)
}

func referenceEncodingConsumerModule() []byte {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc))
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(referenceEncodingFuncType([]byte{0x63, 0x70}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func referenceEncodingTypedCallsModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			referenceEncodingFuncType([]byte{0x70}),
			referenceEncodingFuncType([]byte{0x63, 0x70}),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1),
		)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("indirect", 0, 1),
			wasmtest.ExportEntry("call_ref", 0, 2),
			wasmtest.ExportEntry("return_call_ref", 0, 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xd0, 0x70, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x11, 0x01, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x14, 0x01, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x15, 0x01, 0x0b}),
		)),
	)
}

func TestReferenceEncodingFormDoesNotAffectTypedCalls(t *testing.T) {
	producerCompiled := stagedTypedStorageCompile(t, referenceEncodingProducerModule())
	consumerCompiled := stagedTypedStorageCompile(t, referenceEncodingConsumerModule())
	if got, want := producerCompiled.FuncTypeID[0], consumerCompiled.FuncTypeID[0]; got != want {
		t.Fatalf("shorthand/explicit native type keys = %#x/%#x", got, want)
	}
	blob, err := producerCompiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal shorthand producer: %v", err)
	}
	var reloaded Compiled
	if err := reloaded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("reload shorthand producer: %v", err)
	}
	defer reloaded.Close()
	if got, want := reloaded.FuncTypeID[0], consumerCompiled.FuncTypeID[0]; got != want {
		t.Fatalf("reloaded shorthand/explicit native type keys = %#x/%#x", got, want)
	}
	producer, err := instantiateCore(&reloaded, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate reloaded shorthand producer: %v", err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatalf("export shorthand function: %v", err)
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}})
	if err != nil {
		t.Fatalf("link explicit function import: %v", err)
	}
	defer consumer.Close()
	if got, err := consumer.Invoke("run"); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("cross-instance shorthand/explicit call = %v, %v", got, err)
	}

	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), referenceEncodingTypedCallsModule())
	if err != nil {
		t.Fatalf("compile typed shorthand/explicit calls: %v", err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate typed shorthand/explicit calls: %v", err)
	}
	defer in.Close()
	for _, name := range []string{"indirect", "call_ref", "return_call_ref"} {
		if got, err := in.Invoke(name); err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("%s shorthand/explicit call = %v, %v", name, got, err)
		}
	}
}

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

func typedNestedReferenceProducerModule() []byte {
	nested := []byte{0x60}
	nested = append(nested, wasmtest.Vec(append([]byte{0x64}, wasmtest.ULEB(0)...))...)
	nested = append(nested, wasmtest.Vec()...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			nested,
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func typedNestedReferenceConsumerModule() []byte {
	nested := []byte{0x60}
	nested = append(nested, wasmtest.Vec(append([]byte{0x64}, wasmtest.ULEB(1)...))...)
	nested = append(nested, wasmtest.Vec()...)
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc))
	importEntry = append(importEntry, wasmtest.ULEB(2)...)
	declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(1))...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			nested,
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0x01, 0x10, 0x00, 0x0b}),
		)),
	)
}

func TestStagedTypedNestedStructuralImportRetainsProducer(t *testing.T) {
	producerCompiled := stagedTypedStorageCompile(t, typedNestedReferenceProducerModule())
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate nested producer: %v", err)
	}
	export, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatalf("export nested producer: %v", err)
	}
	consumerCompiled := stagedTypedStorageCompile(t, typedNestedReferenceConsumerModule())
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}})
	if err != nil {
		t.Fatalf("instantiate shifted nested consumer: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("logical producer close: %v", err)
	}
	if _, err := consumer.Invoke("run"); err != nil {
		t.Fatalf("nested structural import after producer close: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	producer.lifeMu.Lock()
	released := producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !released {
		t.Fatal("nested structural producer remained retained after consumer close")
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

func typedNullControlModule() []byte {
	typedControl := []byte{0x60}
	typedControl = append(typedControl, wasmtest.Vec(encodedNullableIndexedRef(0))...)
	typedControl = append(typedControl, wasmtest.Vec([]byte{0x7f})...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			typedControl,
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x64, 0x00, // block (result (ref type 0))
			0x20, 0x00, // local.get 0
			0xd6, 0x00, // br_on_non_null 0
			0x41, 0x01, 0x0f, // null fallthrough consumes the reference
			0x0b,
			0x1a,       // drop the taken non-null branch result
			0x41, 0x02, // non-null result
			0x0b,
		}))),
	)
}

func BenchmarkStagedTypedNullControl(b *testing.B) {
	compiled := stagedTypedStorageCompile(b, typedNullControlModule())
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("run", 0); err != nil || len(got) != 1 || uint32(got[0]) != 1 {
			b.Fatalf("null control result=%v err=%v", got, err)
		}
	}
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
	for name, module := range map[string][]byte{
		"call_ref":     typedLocalCallRefModule(),
		"null_control": typedNullControlModule(),
	} {
		t.Run(name, func(t *testing.T) {
			compiled := stagedTypedStorageCompile(t, module)
			if compiled.requiredFeatures&CoreFeatureTypedFunctionReferences == 0 {
				t.Fatal("typed control/call artifact omitted its required feature bit")
			}
			blob, err := compiled.MarshalBinary()
			if err != nil {
				t.Fatalf("marshal typed module: %v", err)
			}
			if _, err := Load(blob); err == nil || !strings.Contains(err.Error(), "required feature") {
				t.Fatalf("public load of staged typed artifact = %v, want fail-closed required-feature error", err)
			}
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatalf("reload typed module: %v", err)
			}
			defer loaded.Close()
			if loaded.requiredFeatures&CoreFeatureTypedFunctionReferences == 0 {
				t.Fatal("codec reload lost typed required feature bit")
			}

			for _, c := range []*Compiled{compiled, &loaded} {
				_, err := Capture(c, SnapshotOptions{})
				if err == nil || !strings.Contains(err.Error(), "typed function references") {
					t.Fatalf("Capture typed module = %v, want explicit descriptor snapshot rejection", err)
				}
			}
		})
	}
}
