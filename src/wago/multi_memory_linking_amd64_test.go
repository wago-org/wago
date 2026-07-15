//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func memoryImportEntry(module, name string, limits ...byte) []byte {
	entry := append(wasmtest.Name(module), wasmtest.Name(name)...)
	entry = append(entry, 0x02)
	return append(entry, limits...)
}

func officialMultiMemoryProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x02, 0x05}, // mem1: 2..5
			[]byte{0x00, 0x00},       // mem2: 0, no maximum
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("mem1", 2, 0),
			wasmtest.ExportEntry("mem2", 2, 1),
		)),
	)
}

func nativeMultiMemoryProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x02, 0x05},
			[]byte{0x00, 0x00},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", 0, 0),
			wasmtest.ExportEntry("mem1", 2, 0),
			wasmtest.ExportEntry("mem2", 2, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x0b}))),
	)
}

func officialMultiMemoryConsumerModule() []byte {
	types := wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	))
	imports := wasmtest.Section(2, wasmtest.Vec(
		memoryImportEntry("M", "mem1", 0x01, 0x01, 0x06),
		memoryImportEntry("M", "mem2", 0x00, 0x00),
	))
	funcs := make([][]byte, 0, 8)
	for i := 0; i < 4; i++ {
		funcs = append(funcs, wasmtest.ULEB(0))
	}
	for i := 0; i < 4; i++ {
		funcs = append(funcs, wasmtest.ULEB(1))
	}
	exports := make([][]byte, 0, 8)
	for i := 0; i < 4; i++ {
		exports = append(exports, wasmtest.ExportEntry("size"+string(rune('1'+i)), 0, uint32(i)))
	}
	for i := 0; i < 4; i++ {
		exports = append(exports, wasmtest.ExportEntry("grow"+string(rune('1'+i)), 0, uint32(4+i)))
	}
	codes := make([][]byte, 0, 8)
	for i := byte(0); i < 4; i++ {
		codes = append(codes, wasmtest.Code([]byte{0x3f, i, 0x0b}))
	}
	for i := byte(0); i < 4; i++ {
		codes = append(codes, wasmtest.Code([]byte{0x20, 0x00, 0x40, i, 0x0b}))
	}
	return wasmtest.Module(
		types,
		imports,
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x00, 0x03},
			[]byte{0x01, 0x04, 0x05},
		)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func TestStagedMultiMemoryOfficialImportGrowLinking(t *testing.T) {
	producerCompiled := stagedMultiMemoryCompile(t, officialMultiMemoryProducerModule())
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	consumerCompiled := stagedMultiMemoryCompile(t, officialMultiMemoryConsumerModule())
	keys := consumerCompiled.MemoryImports()
	if len(keys) != 2 || keys[0] != "M.mem1" || keys[1] != "M.mem2" {
		t.Fatalf("memory imports = %v, want [M.mem1 M.mem2]", keys)
	}
	imports := make(Imports, len(keys))
	for _, key := range keys {
		field := strings.TrimPrefix(key, "M.")
		memory, err := producer.ExportedMemory(field)
		if err != nil {
			t.Fatalf("resolve registered memory %q: %v", key, err)
		}
		imports[key] = memory
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatalf("instantiate consumer: %v", err)
	}
	consumer2, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatalf("instantiate second registered-memory consumer: %v", err)
	}

	for i, want := range []int32{2, 0, 3, 4} {
		if got := tableTestCallI32(t, consumer, "size"+string(rune('1'+i))); got != want {
			t.Fatalf("initial memory %d size = %d, want %d", i, got, want)
		}
	}
	if got := tableTestCallI32(t, consumer, "grow1", I32(1)); got != 2 {
		t.Fatalf("grow imported memory 1 = %d, want 2", got)
	}
	if got := tableTestCallI32(t, consumer, "grow2", I32(10)); got != 0 {
		t.Fatalf("grow imported memory 2 = %d, want 0", got)
	}
	if got1, got2 := tableTestCallI32(t, consumer2, "size1"), tableTestCallI32(t, consumer2, "size2"); got1 != 3 || got2 != 10 {
		t.Fatalf("second consumer imported growth visibility = %d,%d, want 3,10", got1, got2)
	}
	if got := tableTestCallI32(t, consumer, "grow3", I32(3)); got != 3 {
		t.Fatalf("grow local memory 3 = %d, want 3", got)
	}
	if got := tableTestCallI32(t, consumer, "grow4", I32(1)); got != 4 {
		t.Fatalf("grow local memory 4 = %d, want 4", got)
	}
	if got := tableTestCallI32(t, consumer, "grow4", I32(1)); got != -1 {
		t.Fatalf("grow local memory 4 past max = %d, want -1", got)
	}
	for i, want := range []int32{3, 10, 6, 5} {
		if got := tableTestCallI32(t, consumer, "size"+string(rune('1'+i))); got != want {
			t.Fatalf("grown memory %d size = %d, want %d", i, got, want)
		}
	}

	if err := producer.Close(); err != nil {
		t.Fatalf("logical producer close: %v", err)
	}
	producer.lifeMu.Lock()
	producerResourcesClosed := producer.resourcesClosed
	producer.lifeMu.Unlock()
	if producerResourcesClosed {
		t.Fatal("producer resources closed while imported memories remain live")
	}
	if got := tableTestCallI32(t, consumer, "size1"); got != 3 {
		t.Fatalf("consumer lost grown imported memory after producer close: %d", got)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	producer.lifeMu.Lock()
	producerResourcesClosed = producer.resourcesClosed
	producer.lifeMu.Unlock()
	if producerResourcesClosed {
		t.Fatal("producer resources closed while second memory importer remained live")
	}
	if got := tableTestCallI32(t, consumer2, "size1"); got != 3 {
		t.Fatalf("second consumer lost imported memory after first close: %d", got)
	}
	if err := consumer2.Close(); err != nil {
		t.Fatalf("second consumer close: %v", err)
	}
	producer.lifeMu.Lock()
	producerResourcesClosed = producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !producerResourcesClosed {
		t.Fatal("producer resources retained after final memory importer closed")
	}
}

func TestStagedMultiMemoryNativeProducerTenantRebindsContext(t *testing.T) {
	producerCompiled := stagedMultiMemoryCompile(t, nativeMultiMemoryProducerModule())
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate native producer: %v", err)
	}
	defer producer.Close()
	m1, err := producer.ExportedMemory("mem1")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := producer.ExportedMemory("mem2")
	if err != nil {
		t.Fatal(err)
	}
	consumerCompiled := stagedMultiMemoryCompile(t, officialMultiMemoryConsumerModule())
	defer consumerCompiled.Close()
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M.mem1": m1, "M.mem2": m2}})
	if err != nil {
		t.Fatalf("instantiate against executable memory owner: %v", err)
	}
	defer consumer.Close()
	if got := tableTestCallI32(t, producer, "f"); got != 0 {
		t.Fatalf("producer call after tenant install = %d, want 0", got)
	}
	if got := tableTestCallI32(t, consumer, "size1"); got != 2 {
		t.Fatalf("consumer imported memory size = %d, want 2", got)
	}
	if got := tableTestCallI32(t, consumer, "size3"); got != 3 {
		t.Fatalf("consumer private memory size = %d, want 3", got)
	}

	var wg sync.WaitGroup
	errs := make(chan string, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			got, err := producer.Invoke("f")
			if err != nil || len(got) != 1 || int32(got[0]) != 0 {
				errs <- "producer result changed during native-context rebinding"
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			got, err := consumer.Invoke("size3")
			if err != nil || len(got) != 1 || int32(got[0]) != 3 {
				errs <- "consumer private directory changed during native-context rebinding"
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Fatal(msg)
	}
}

func importedCallMultiMemoryModule() []byte {
	funcImport := append(append(wasmtest.Name("M"), wasmtest.Name("f")...), 0x00, 0x00)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(funcImport, memoryImportEntry("M", "mem1", 0x01, 0x01, 0x05))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func TestStagedMultiMemoryNativeImportedCallRebindsContext(t *testing.T) {
	producerCompiled := stagedMultiMemoryCompile(t, nativeMultiMemoryProducerModule())
	defer producerCompiled.Close()
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	memory, err := producer.ExportedMemory("mem1")
	if err != nil {
		t.Fatal(err)
	}
	fn, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	consumerCompiled := stagedMultiMemoryCompile(t, importedCallMultiMemoryModule())
	defer consumerCompiled.Close()
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M.f": fn, "M.mem1": memory}})
	if err != nil {
		t.Fatalf("instantiate native imported-call consumer: %v", err)
	}
	defer consumer.Close()
	if got := tableTestCallI32(t, consumer, "call"); got != 0 {
		t.Fatalf("native imported call = %d, want 0", got)
	}
	if got := tableTestCallI32(t, producer, "f"); got != 0 {
		t.Fatalf("producer context after imported call = %d, want 0", got)
	}
}

func missingSecondMemoryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(
			memoryImportEntry("env", "first", 0x01, 0x01, 0x02),
			memoryImportEntry("env", "missing", 0x01, 0x01, 0x02),
		)),
	)
}

func TestStagedMultiMemoryFailedLinkIsAtomic(t *testing.T) {
	compiled := stagedMultiMemoryCompile(t, missingSecondMemoryModule())
	memory, err := NewMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instantiateCore(compiled, InstantiateOptions{Imports: Imports{"env.first": memory}}); err == nil || !strings.Contains(err.Error(), "missing imported memory") {
		t.Fatalf("instantiate with missing second memory = %v, want explicit missing-import error", err)
	}
	if err := memory.Close(); err != nil {
		t.Fatalf("failed link leaked first memory attachment: %v", err)
	}
}

func multiMemorySnapshotStartModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("tick")...), 0x00, 0x00))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02}, []byte{0x01, 0x01, 0x02})),
		wasmtest.Section(8, wasmtest.ULEB(0)),
	)
}

func TestStagedMultiMemorySnapshotPolicyRejectsBeforeMutation(t *testing.T) {
	compiled := stagedMultiMemoryCompile(t, multiMemorySnapshotStartModule())
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal staged compiled module: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload staged compiled module: %v", err)
	}
	defer loaded.Close()

	for _, c := range []*Compiled{compiled, &loaded} {
		calls := 0
		_, err := Capture(c, SnapshotOptions{Imports: Imports{"env.tick": HostFunc(func(HostModule, []uint64, []uint64) { calls++ })}})
		if err == nil || !strings.Contains(err.Error(), "multiple memories") {
			t.Fatalf("Capture multi-memory module = %v, want explicit policy rejection", err)
		}
		if calls != 0 {
			t.Fatalf("snapshot rejection ran start function %d time(s)", calls)
		}
	}
}
