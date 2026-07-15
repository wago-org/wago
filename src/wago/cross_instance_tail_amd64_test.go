//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func directCrossTailProducerModule() []byte {
	body := []byte{
		0x20, 0x00, 0x41, 0x7f, 0x46, // local.get 0; i32.const -1; i32.eq
		0x04, 0x40, 0x00, 0x0b, // if; unreachable; end
		0x20, 0x00, 0x45, // local.get 0; i32.eqz
		0x04, 0x7f, // if (result i32)
		0x41, 0x07, // i32.const 7
		0x05,                         // else
		0x20, 0x00, 0x41, 0x01, 0x6b, // n - 1
		0x12, 0x00, // return_call 0
		0x0b, // end if
		0x0b, // end function
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", 0, 0),
			wasmtest.ExportEntry("trap", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(body),
			wasmtest.Code([]byte{0x00, 0x0b}),
		)),
	)
}

func directCrossTailConsumerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(funcImportEntry("env", "f", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("nested", 0, 2),
			wasmtest.ExportEntry("repeat", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x12, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x41, 0x05, 0x6a, 0x0b}),
			wasmtest.Code([]byte{
				0x02, 0x40, // block
				0x03, 0x40, // loop
				0x20, 0x00, 0x45, 0x0d, 0x01, // break when n == 0
				0x41, 0x00, 0x10, 0x01, 0x1a, // run(0); drop
				0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n--
				0x0c, 0x00, 0x0b, 0x0b, // continue; end loop/block
				0x41, 0x07, 0x0b,
			}),
		)),
	)
}

func instantiateDirectCrossTail(t testing.TB, exportName string) (*Instance, *Instance) {
	t.Helper()
	producerCompiled, err := compileStagedTail(directCrossTailProducerModule())
	if err != nil {
		t.Fatalf("compile direct-tail producer: %v", err)
	}
	t.Cleanup(func() { _ = producerCompiled.Close() })
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate direct-tail producer: %v", err)
	}
	export, err := producer.ExportedFunc(exportName)
	if err != nil {
		_ = producer.Close()
		t.Fatalf("export direct-tail producer: %v", err)
	}
	consumerCompiled, err := compileStagedTail(directCrossTailConsumerModule())
	if err != nil {
		_ = producer.Close()
		t.Fatalf("compile direct-tail consumer: %v", err)
	}
	t.Cleanup(func() { _ = consumerCompiled.Close() })
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}})
	if err != nil {
		_ = producer.Close()
		t.Fatalf("instantiate direct-tail consumer: %v", err)
	}
	return producer, consumer
}

func TestStagedDirectCrossInstanceReturnCall(t *testing.T) {
	if _, err := Compile(nil, directCrossTailConsumerModule()); err == nil || !strings.Contains(err.Error(), "tail-call disabled") {
		t.Fatalf("public direct cross-tail compile error = %v, want fail-closed feature rejection", err)
	}
	compiled, err := compileStagedTail(directCrossTailConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tail-call") {
		t.Fatalf("direct cross-tail snapshot error = %v", err)
	}

	producer, consumer := instantiateDirectCrossTail(t, "f")
	if got := tableTestCallI32(t, consumer, "run", I32(1_000_000)); got != 7 {
		t.Fatalf("million-step direct cross tail = %d, want 7", got)
	}
	if got := tableTestCallI32(t, consumer, "nested", I32(1_000_000)); got != 12 {
		t.Fatalf("nested direct cross-tail continuation = %d, want 12", got)
	}
	if got := tableTestCallI32(t, consumer, "repeat", I32(10_000)); got != 7 {
		t.Fatalf("repeated direct cross-tail transfer = %d, want 7", got)
	}
	if _, err := consumer.Invoke("nested", I32(-1)); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("nested direct cross-tail trap = %v", err)
	}
	if got := tableTestCallI32(t, consumer, "nested", I32(10)); got != 12 {
		t.Fatalf("direct cross-tail continuation did not recover after trap: %d", got)
	}

	if err := producer.Close(); err != nil {
		t.Fatalf("logical producer close: %v", err)
	}
	producer.lifeMu.Lock()
	released := producer.resourcesClosed
	producer.lifeMu.Unlock()
	if released {
		t.Fatal("direct cross-tail consumer did not retain producer resources")
	}
	if got := tableTestCallI32(t, consumer, "nested", I32(10)); got != 12 {
		t.Fatalf("direct cross tail after producer close = %d, want 12", got)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	producer.lifeMu.Lock()
	released = producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !released {
		t.Fatal("producer resources remained retained after direct-tail consumer close")
	}

	trapProducer, trapConsumer := instantiateDirectCrossTail(t, "trap")
	if _, err := trapConsumer.Invoke("run", I32(0)); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("direct cross-tail trap = %v", err)
	}
	if _, err := trapConsumer.Invoke("nested", I32(0)); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("nested direct cross-tail trap = %v", err)
	}
	if err := trapConsumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := trapProducer.Close(); err != nil {
		t.Fatal(err)
	}
}

func directCrossTailPairProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pair", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0x42, 0x09, 0x0b}))),
	)
}

func directCrossTailPairConsumerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I64}))),
		wasmtest.Section(2, wasmtest.Vec(funcImportEntry("env", "pair", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("nested", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x12, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x0b}),
		)),
	)
}

func TestStagedDirectCrossInstanceReturnCallTwoIntegerResults(t *testing.T) {
	producerCompiled, err := compileStagedTail(directCrossTailPairProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCompiled.Close()
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	export, err := producer.ExportedFunc("pair")
	if err != nil {
		producer.Close()
		t.Fatal(err)
	}
	consumerCompiled, err := compileStagedTail(directCrossTailPairConsumerModule())
	if err != nil {
		producer.Close()
		t.Fatal(err)
	}
	defer consumerCompiled.Close()
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.pair": export}})
	if err != nil {
		producer.Close()
		t.Fatal(err)
	}
	defer consumer.Close()
	defer producer.Close()
	for _, name := range []string{"run", "nested"} {
		got, err := consumer.Invoke(name, I32(1))
		if err != nil || len(got) != 2 || int32(got[0]) != 7 || got[1] != 9 {
			t.Fatalf("%s direct cross-tail pair = %v, err=%v, want [7 9]", name, got, err)
		}
	}
}

func directCrossTailFloatProducerModule() []byte {
	body := []byte{
		0x20, 0x00, 0x41, 0x7f, 0x46, // n == -1
		0x04, 0x40, 0x00, 0x0b, // if; unreachable; end
		0x20, 0x00, 0x45, // n == 0
		0x04, 0x7c, // if (result f64)
		0x20, 0x01, // value
		0x05,                         // else
		0x20, 0x00, 0x41, 0x01, 0x6b, // n - 1
		0x20, 0x01, // value
		0x12, 0x00, // return_call 0
		0x0b, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func directCrossTailFloatConsumerModule() []byte {
	add := make([]byte, 8)
	binary.LittleEndian.PutUint64(add, math.Float64bits(1.5))
	nested := []byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x01, 0x44}
	nested = append(nested, add...)
	nested = append(nested, 0xa0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(2, wasmtest.Vec(funcImportEntry("env", "f", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("nested", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x12, 0x00, 0x0b}),
			wasmtest.Code(nested),
		)),
	)
}

func instantiateDirectCrossTailFloat(t testing.TB) (*Instance, *Instance) {
	t.Helper()
	producerCompiled, err := compileStagedTail(directCrossTailFloatProducerModule())
	if err != nil {
		t.Fatalf("compile float direct-tail producer: %v", err)
	}
	t.Cleanup(func() { _ = producerCompiled.Close() })
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate float direct-tail producer: %v", err)
	}
	export, err := producer.ExportedFunc("f")
	if err != nil {
		_ = producer.Close()
		t.Fatal(err)
	}
	consumerCompiled, err := compileStagedTail(directCrossTailFloatConsumerModule())
	if err != nil {
		_ = producer.Close()
		t.Fatalf("compile float direct-tail consumer: %v", err)
	}
	t.Cleanup(func() { _ = consumerCompiled.Close() })
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}})
	if err != nil {
		_ = producer.Close()
		t.Fatalf("instantiate float direct-tail consumer: %v", err)
	}
	return producer, consumer
}

func TestStagedDirectCrossInstanceReturnCallMixedFloat(t *testing.T) {
	if _, err := Compile(nil, directCrossTailFloatConsumerModule()); err == nil || !strings.Contains(err.Error(), "tail-call disabled") {
		t.Fatalf("public float direct-tail compile error = %v", err)
	}
	producer, consumer := instantiateDirectCrossTailFloat(t)
	value := math.Float64bits(3.25)
	for _, tc := range []struct {
		name string
		want float64
	}{
		{name: "run", want: 3.25},
		{name: "nested", want: 4.75},
	} {
		got, err := consumer.Invoke(tc.name, I32(1_000_000), value)
		if err != nil || len(got) != 1 || math.Float64frombits(got[0]) != tc.want {
			t.Fatalf("%s mixed-float direct tail = %v, err=%v, want %v", tc.name, got, err, tc.want)
		}
	}
	for i := 0; i < 10_000; i++ {
		got, err := consumer.Invoke("run", I32(0), value)
		if err != nil || len(got) != 1 || got[0] != value {
			t.Fatalf("repeated mixed-float direct tail %d = %v, err=%v", i, got, err)
		}
	}
	if _, err := consumer.Invoke("nested", I32(-1), value); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("mixed-float direct-tail trap = %v", err)
	}
	if got, err := consumer.Invoke("nested", I32(1), value); err != nil || len(got) != 1 || math.Float64frombits(got[0]) != 4.75 {
		t.Fatalf("mixed-float direct tail did not recover = %v, err=%v", got, err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("logical float producer close: %v", err)
	}
	producer.lifeMu.Lock()
	released := producer.resourcesClosed
	producer.lifeMu.Unlock()
	if released {
		t.Fatal("mixed-float direct-tail consumer did not retain producer")
	}
	if got, err := consumer.Invoke("run", I32(1), value); err != nil || len(got) != 1 || got[0] != value {
		t.Fatalf("mixed-float direct tail after producer close = %v, err=%v", got, err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	producer.lifeMu.Lock()
	released = producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !released {
		t.Fatal("mixed-float producer remained retained after consumer close")
	}
}

func floatParamIntegerResultCrossTailModule(imported bool) []byte {
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.I32})))}
	if imported {
		sections = append(sections,
			wasmtest.Section(2, wasmtest.Vec(funcImportEntry("env", "f", 0))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x12, 0x00, 0x0b}))),
		)
	} else {
		sections = append(sections,
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0x0b}))),
		)
	}
	return wasmtest.Module(sections...)
}

func TestStagedDirectCrossInstanceReturnCallKeepsOtherFloatShapesGated(t *testing.T) {
	producerCompiled, err := compileStagedTail(floatParamIntegerResultCrossTailModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer producerCompiled.Close()
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	consumerCompiled, err := compileStagedTail(floatParamIntegerResultCrossTailModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCompiled.Close()
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}}); err == nil || !strings.Contains(err.Error(), "unsupported cross-instance tail ABI") {
		t.Fatalf("unproven float direct-tail shape error = %v", err)
	}
}

func oversizedDirectCrossTailModule(imported bool) []byte {
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32})))}
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(funcImportEntry("env", "f", 0))))
		body := make([]byte, 0, 27)
		for i := byte(0); i < 8; i++ {
			body = append(body, 0x20, i)
		}
		body = append(body, 0x12, 0x00, 0x0b)
		sections = append(sections,
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
	} else {
		sections = append(sections,
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
		)
	}
	return wasmtest.Module(sections...)
}

func TestStagedDirectCrossInstanceReturnCallRejectsOversizedSignature(t *testing.T) {
	producerCompiled, err := compileStagedTail(oversizedDirectCrossTailModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer producerCompiled.Close()
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	export, err := producer.ExportedFunc("f")
	if err != nil {
		producer.Close()
		t.Fatal(err)
	}
	consumerCompiled, err := compileStagedTail(oversizedDirectCrossTailModule(true))
	if err != nil {
		producer.Close()
		t.Fatal(err)
	}
	defer consumerCompiled.Close()
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}}); err == nil || !strings.Contains(err.Error(), "unsupported cross-instance tail ABI") {
		t.Fatalf("oversized direct cross-tail error = %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("failed link retained producer: %v", err)
	}
}

func BenchmarkStagedDirectCrossInstanceReturnCallMixedFloat(b *testing.B) {
	producer, consumer := instantiateDirectCrossTailFloat(b)
	defer consumer.Close()
	defer producer.Close()
	value := math.Float64bits(3.25)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := consumer.Invoke("run", I32(0), value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedDirectCrossInstanceReturnCall(b *testing.B) {
	producer, consumer := instantiateDirectCrossTail(b, "f")
	defer consumer.Close()
	defer producer.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := consumer.Invoke("run", I32(0)); err != nil {
			b.Fatal(err)
		}
	}
}
