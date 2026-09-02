//go:build (linux || darwin || windows) && (amd64 || arm64)

package oracle

import (
	"context"
	"errors"
	"reflect"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func fuzzTestConst(value int32) []byte {
	return append([]byte{0x41}, wasmtest.SLEB32(value)...)
}

func TestHarnessRecordsPreStartInstantiationFailureWithoutState(t *testing.T) {
	harness := NewHarness()
	caseState, err := harness.Begin(0x5eed, "memory-import-limit-mismatch")
	if err != nil {
		t.Fatal(err)
	}
	observation, err := caseState.FinishInstantiationFailure(errors.New("imported memory __fuzz.state_memory limits do not match"))
	if err != nil {
		_ = caseState.Close()
		t.Fatal(err)
	}
	want := []Event{{"schema", Schema}, {"outcome", "instantiation-failed", "memory-import-limit-mismatch"}}
	if !reflect.DeepEqual(observation.Events, want) {
		t.Fatalf("events = %#v, want %#v", observation.Events, want)
	}
	if observation.Hash == "" || len(observation.JSON) == 0 {
		t.Fatalf("observation hash/json are empty: %#v", observation)
	}
	if err := caseState.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessIgnoresProviderInstantiationBeforeCase(t *testing.T) {
	harness := NewHarness()
	set, err := harness.PluginSet()
	if err != nil {
		t.Fatal(err)
	}
	runtime := wago.NewRuntime()
	defer runtime.Close()
	if err := runtime.LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	module, err := runtime.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	instance, err := runtime.Instantiate(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	caseState, err := harness.Begin(0x5eed)
	if err != nil {
		t.Fatal(err)
	}
	if err := caseState.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessRejectsUnrelatedPreStartFailure(t *testing.T) {
	harness := NewHarness()
	caseState, err := harness.Begin(0x5eed, "table-import-limit-mismatch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := caseState.FinishInstantiationFailure(errors.New("unrelated host failure")); err == nil {
		_ = caseState.Close()
		t.Fatal("unrelated failure was accepted")
	}
	if err := caseState.Close(); err != nil {
		t.Fatal(err)
	}
}

func fuzzTestCall(index uint32) []byte {
	return append([]byte{0x10}, wasmtest.ULEB(index)...)
}

func fuzzTestImport(name string, typeIndex uint32) []byte {
	out := append(wasmtest.Name("__fuzz"), wasmtest.Name(name)...)
	out = append(out, 0x00)
	return append(out, wasmtest.ULEB(typeIndex)...)
}

func fuzzTestMemoryImport(name string) []byte {
	out := append(wasmtest.Name("__fuzz"), wasmtest.Name(name)...)
	return append(out, 0x02, 0x01, 0x01, 0x02)
}

func fuzzTestTableImport(name string) []byte {
	out := append(wasmtest.Name("__fuzz"), wasmtest.Name(name)...)
	return append(out, 0x01, 0x70, 0x01, 0x04, 0x08)
}

func fuzzTestModule() []byte {
	start := append(fuzzTestConst(0x1234), fuzzTestCall(0)...)
	start = append(start, fuzzTestConst(7)...)
	start = append(start, fuzzTestConst(-1)...)
	start = append(start, fuzzTestCall(1)...)
	start = append(start, 0x0b)
	global := wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b})
	table := []byte{0x70, 0x01, 0x01, 0x01}
	memory := []byte{0x01, 0x01, 0x01}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x02}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			fuzzTestImport("mark", 0),
			fuzzTestImport("observe_i32", 1),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(5, wasmtest.Vec(memory)),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("__fuzz_func_2", 0, 2),
			wasmtest.ExportEntry("__fuzz_table_0", 1, 0),
			wasmtest.ExportEntry("__fuzz_memory_0", 2, 0),
			wasmtest.ExportEntry("__fuzz_global_0", 3, 0),
		)),
		wasmtest.Section(8, wasmtest.ULEB(2)),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(start))),
	)
}

func fuzzTrappingTestModule() []byte {
	identity := []byte{0x20, 0x00, 0x0b}
	start := append(fuzzTestConst(42), 0x24, 0x00)
	start = append(start, fuzzTestConst(0)...)
	start = append(start, fuzzTestConst(42)...)
	start = append(start, 0x36, 0x02, 0x00)
	start = append(start, fuzzTestConst(0x6a4)...)
	start = append(start, fuzzTestCall(0)...)
	start = append(start, 0x00, 0x0b)
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x01}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			fuzzTestImport("mark", 0),
			wasmtest.GlobalImportEntry("__fuzz", "state_global_i32", wasm.I32, true),
			fuzzTestMemoryImport("state_memory"),
			fuzzTestTableImport("state_table"),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("__fuzz_func_0", 0, 1),
			wasmtest.ExportEntry("__fuzz_table_0", 1, 0),
			wasmtest.ExportEntry("__fuzz_memory_0", 2, 0),
			wasmtest.ExportEntry("__fuzz_global_0", 3, 0),
		)),
		wasmtest.Section(8, wasmtest.ULEB(2)),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(identity), wasmtest.Code(start))),
	)
}

func TestHarnessRecordsStartAndCompleteState(t *testing.T) {
	harness := NewHarness()
	set, err := harness.PluginSet()
	if err != nil {
		t.Fatal(err)
	}
	runtime := wago.NewRuntime()
	defer runtime.Close()
	if err := runtime.LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	module, err := runtime.Compile(fuzzTestModule())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	caseState, err := harness.Begin(0x5eed)
	if err != nil {
		t.Fatal(err)
	}
	instance, executionErr := runtime.Instantiate(context.Background(), module, caseState.InstantiateOptions()...)
	if executionErr != nil {
		_ = caseState.Close()
		t.Fatal(executionErr)
	}
	observation, err := caseState.Finish(module.Metadata(), instance, executionErr)
	if err != nil {
		_ = instance.Close()
		_ = caseState.Close()
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := caseState.Close(); err != nil {
		t.Fatal(err)
	}
	if len(observation.Events) != 7 {
		t.Fatalf("events = %#v", observation.Events)
	}
	wantPrefix := []Event{
		{"schema", Schema},
		{"mark", "00001234"},
		{"observe_i32", "00000007", "ffffffff"},
		{"outcome", "returned"},
		{"global", 0, []string{"__fuzz_global_0"}, "i32", "00000000"},
	}
	if !reflect.DeepEqual(observation.Events[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("event prefix = %#v, want %#v", observation.Events[:len(wantPrefix)], wantPrefix)
	}
	if got := observation.Events[len(observation.Events)-1]; !reflect.DeepEqual(got, Event{"table", 0, []string{"__fuzz_table_0"}, 1, []string{"funcidx:2"}}) {
		t.Fatalf("table event = %#v", got)
	}
	if observation.Hash == "" || len(observation.JSON) == 0 {
		t.Fatalf("observation hash/json are empty: %#v", observation)
	}
}

func TestHarnessRetainsAndClosesImportedStateAfterStartTrap(t *testing.T) {
	harness := NewHarness()
	set, err := harness.PluginSet()
	if err != nil {
		t.Fatal(err)
	}
	runtime := wago.NewRuntime()
	defer runtime.Close()
	if err := runtime.LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	module, err := runtime.Compile(fuzzTrappingTestModule())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	caseState, err := harness.Begin(0x5eed)
	if err != nil {
		t.Fatal(err)
	}
	instance, executionErr := runtime.Instantiate(context.Background(), module, caseState.InstantiateOptions()...)
	if instance != nil || executionErr == nil {
		t.Fatalf("instance, error = %v, %v; want nil, trap", instance, executionErr)
	}
	observation, err := caseState.Finish(module.Metadata(), instance, executionErr)
	if err != nil {
		_ = caseState.Close()
		t.Fatal(err)
	}
	if got := observation.Events[len(observation.Events)-1]; !reflect.DeepEqual(got, Event{"table", 0, []string{"__fuzz_table_0"}, 4, []string{"non-null", "null", "null", "null"}}) {
		_ = caseState.Close()
		t.Fatalf("table event = %#v", got)
	}
	if err := caseState.Close(); err != nil {
		t.Fatal(err)
	}
}
