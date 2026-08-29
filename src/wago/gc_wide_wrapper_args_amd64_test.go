//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

func gcWideWrapperReferenceArgumentsModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01} // (struct (field (mut i32)))
	calleeType := append([]byte{0x60}, wasmtest.ULEB(18)...)
	calleeType = append(calleeType, 0x63, 0x00) // (ref null 0)
	for range 17 {
		calleeType = append(calleeType, 0x64, 0x00) // (ref 0)
	}
	calleeType = append(calleeType, 0x01, 0x7f)                // (result i32)
	callee := []byte{0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b} // local.get 1; struct.get 0 0
	caller := []byte{0xd0, 0x00}                               // ref.null 0
	for range 17 {
		caller = append(caller, 0x23, 0x00) // global.get 0
	}
	caller = append(caller, 0x10, 0x00, 0x0b)                                     // call 0
	initializer := append([]byte{0x64, 0x00, 0x00, 0x41}, wasmtest.SLEB32(73)...) // (ref 0), immutable, i32.const 73
	initializer = append(initializer, 0xfb, 0x00, 0x00, 0x0b)                     // struct.new 0; end
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			calleeType,
			[]byte{0x60, 0x00, 0x01, 0x7f},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(initializer)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee), wasmtest.Code(caller))),
	)
}

func wideWrapperDeferredMemoryArgumentsModule() []byte {
	params := make([]byte, 14)
	for i := range params {
		params[i] = 0x7f
	}
	calleeType := append([]byte{0x60}, wasmtest.ULEB(uint32(len(params)))...)
	calleeType = append(calleeType, params...)
	calleeType = append(calleeType, 0x01, 0x7f)
	callee := []byte{0x20, 0x01, 0x0b} // local.get 1
	caller := append([]byte{0x41}, wasmtest.SLEB32(42)...)
	for range 13 {
		caller = append(caller, 0x3f, 0x00) // memory.size 0
	}
	caller = append(caller, 0x10, 0x00, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(calleeType, []byte{0x60, 0x00, 0x01, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee), wasmtest.Code(caller))),
	)
}

func crossInstanceWideWrapperModules() (provider, consumer []byte) {
	calleeType := []byte{0x60, 0x0e}
	for range 14 {
		calleeType = append(calleeType, 0x7f)
	}
	calleeType = append(calleeType, 0x01, 0x7f)
	callee := []byte{0x20, 0x01, 0x0b}
	caller := append([]byte{0x41}, wasmtest.SLEB32(42)...)
	for range 13 {
		caller = append(caller, 0x3f, 0x00)
	}
	caller = append(caller, 0x10, 0x00, 0x0b)
	provider = wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(calleeType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee))),
	)
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	importEntry = append(importEntry, 0x00, 0x00) // function import, type 0
	consumer = wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(calleeType, []byte{0x60, 0x00, 0x01, 0x7f})),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller))),
	)
	return provider, consumer
}

func TestCrossInstanceWideWrapperStagesSpillsCreatedDuringFlush(t *testing.T) {
	providerData, consumerData := crossInstanceWideWrapperModules()
	providerCode, err := Compile(NewRuntimeConfig(), providerData)
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	provider, err := instantiateCore(providerCode, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	exported, err := provider.ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	consumerCode, err := Compile(NewRuntimeConfig(), consumerData)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{Imports: Imports{"env.f": exported}})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	got, callErr := consumer.Invoke("run")
	if callErr != nil || !reflect.DeepEqual(got, []uint64{1}) {
		t.Fatalf("run = %v, %v; want [1]", got, callErr)
	}
}

func TestWideWrapperStagesSpillsCreatedDuringFlush(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig(), wideWrapperDeferredMemoryArgumentsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, callErr := in.Invoke("run")
	if callErr != nil || !reflect.DeepEqual(got, []uint64{1}) {
		t.Fatalf("run = %v, %v; want [1]", got, callErr)
	}
}

func wideWrapperDeferredFloatArgumentsModule() []byte {
	calleeType := []byte{0x60, 0x11}
	for range 17 {
		calleeType = append(calleeType, 0x7c) // f64
	}
	calleeType = append(calleeType, 0x01, 0x7c)
	callee := []byte{0x20, 0x01, 0x0b}                                     // local.get 1
	caller := []byte{0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x45, 0x40} // f64.const 42
	for range 16 {
		caller = append(caller, 0x41, 0x01, 0xb7) // i32.const 1; f64.convert_i32_s
	}
	caller = append(caller, 0x10, 0x00, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(calleeType, []byte{0x60, 0x00, 0x01, 0x7c})),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee), wasmtest.Code(caller))),
	)
}

func TestWideWrapperStagesXMMSpillsCreatedDuringFlush(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig(), wideWrapperDeferredFloatArgumentsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, callErr := in.Invoke("run")
	if callErr != nil || len(got) != 1 || got[0] != F64(1) {
		t.Fatalf("run = %v, %v; want f64(1)", got, callErr)
	}
}

func TestGCWideWrapperReferenceArgumentsPreserveLeadingValues(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcWideWrapperReferenceArgumentsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, callErr := in.Invoke("run")
	if callErr != nil || !reflect.DeepEqual(got, []uint64{73}) {
		t.Fatalf("run = %v, %v; want [73]", got, callErr)
	}
}
