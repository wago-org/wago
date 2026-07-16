//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

func expectIndirectTrapAt(t *testing.T, in *Instance, index int32) {
	t.Helper()
	if _, err := in.Invoke("call", uint64(uint32(index))); err == nil || !strings.Contains(err.Error(), "indirect") {
		t.Fatalf("call(%d) error = %v, want indirect-call trap", index, err)
	}
}

func TestStagedOfficialMultiMemoryLinkingStoreSemantics(t *testing.T) {
	t.Run("linking0 unknown import atomic and prior table writes persist", func(t *testing.T) {
		modules := stagedOfficialMultiMemoryModules(t, "linking0")
		if len(modules) != 3 {
			t.Fatalf("linking0 emitted %d modules, want 3", len(modules))
		}
		producerCompiled := stagedMultiMemoryCompile(t, modules[0])
		defer producerCompiled.Close()
		producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate linking0 producer: %v", err)
		}
		defer producer.Close()
		table, err := producer.ExportedTable("tab")
		if err != nil {
			t.Fatal(err)
		}
		standardMemory, err := NewSharedMemory(1, 2)
		if err != nil {
			t.Fatal(err)
		}
		defer standardMemory.Close()

		unknownCompiled := stagedMultiMemoryCompile(t, modules[1])
		if _, err := instantiateCore(unknownCompiled, InstantiateOptions{Imports: Imports{
			"Mt.tab": table, "spectest.memory": standardMemory,
		}}); err == nil || !strings.Contains(err.Error(), `missing imported memory "Mt.mem"`) {
			unknownCompiled.Close()
			t.Fatalf("linking0 unknown import error = %v", err)
		}
		if err := unknownCompiled.Close(); err != nil {
			t.Fatal(err)
		}
		expectIndirectTrapAt(t, producer, 7)
		expectIndirectTrapAt(t, producer, 9)

		failedCompiled := stagedMultiMemoryCompile(t, modules[2])
		if _, err := instantiateCore(failedCompiled, InstantiateOptions{Imports: Imports{"Mt.tab": table}}); err == nil || !strings.Contains(err.Error(), "active data segment") {
			failedCompiled.Close()
			t.Fatalf("linking0 data trap error = %v", err)
		}
		if err := failedCompiled.Close(); err != nil {
			t.Fatal(err)
		}
		if got := tableTestCallI32(t, producer, "call", I32(7)); got != 0 {
			t.Fatalf("linking0 table[7]() = %d, want persisted 0", got)
		}
	})

	t.Run("linking1 safe memory consumers execute and mixed contexts fail closed", func(t *testing.T) {
		modules := stagedOfficialMultiMemoryModules(t, "linking1")
		if len(modules) != 6 {
			t.Fatalf("linking1 emitted %d modules, want 6", len(modules))
		}
		producerCompiled := stagedMultiMemoryCompile(t, modules[0])
		defer producerCompiled.Close()
		producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate linking1 producer: %v", err)
		}
		defer producer.Close()
		mem0, err := producer.ExportedMemory("mem0")
		if err != nil {
			t.Fatal(err)
		}
		mem1, err := producer.ExportedMemory("mem1")
		if err != nil {
			t.Fatal(err)
		}
		load, err := producer.ExportedFunc("load")
		if err != nil {
			t.Fatal(err)
		}

		mixedCompiled := stagedMultiMemoryCompile(t, modules[1])
		mixed, err := instantiateCore(mixedCompiled, InstantiateOptions{Imports: Imports{"Mm.load": load, "Mm.mem0": mem0}})
		if err != nil {
			mixedCompiled.Close()
			t.Fatalf("instantiate linking1 re-export-only function consumer: %v", err)
		}
		defer mixed.Close()
		defer mixedCompiled.Close()
		if got := tableTestCallI32(t, producer, "load", I32(12)); got != 2 {
			t.Fatalf("mixed consumer mutated producer before data writes: %d", got)
		}
		if got := tableTestCallI32(t, mixed, "Mm.load", I32(12)); got != 2 {
			t.Fatalf("mixed consumer re-exported load = %d, want 2", got)
		}
		if got := tableTestCallI32(t, mixed, "load", I32(12)); got != 0xf2 {
			t.Fatalf("mixed consumer private-memory load = %d, want 0xf2", got)
		}

		consumerCompiled := stagedMultiMemoryCompile(t, modules[2])
		defer consumerCompiled.Close()
		consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"Mm.mem1": mem1}})
		if err != nil {
			t.Fatalf("instantiate linking1 memory-only consumer: %v", err)
		}
		defer consumer.Close()
		if got := tableTestCallI32(t, producer, "load", I32(12)); got != 0xa7 {
			t.Fatalf("producer did not observe imported-memory data = %d, want 0xa7", got)
		}
		if got := tableTestCallI32(t, consumer, "load", I32(12)); got != 0xa7 {
			t.Fatalf("consumer imported-memory load = %d, want 0xa7", got)
		}
		if got := tableTestCallI32(t, mixed, "Mm.load", I32(12)); got != 0xa7 {
			t.Fatalf("mixed consumer re-export lost producer state = %d, want 0xa7", got)
		}
		if got := tableTestCallI32(t, mixed, "load", I32(12)); got != 0xf2 {
			t.Fatalf("mixed consumer private memory was corrupted = %d, want 0xf2", got)
		}

		boundaryCompiled := stagedMultiMemoryCompile(t, modules[3])
		boundary, err := instantiateCore(boundaryCompiled, InstantiateOptions{Imports: Imports{"Mm.mem1": mem1}})
		if err != nil {
			boundaryCompiled.Close()
			t.Fatalf("instantiate linking1 final-byte data segment: %v", err)
		}
		if err := boundary.Close(); err != nil {
			t.Fatal(err)
		}
		if err := boundaryCompiled.Close(); err != nil {
			t.Fatal(err)
		}
		for i, tc := range []struct {
			data   []byte
			key    string
			memory *Memory
		}{
			{data: modules[4], key: "Mm.mem0", memory: mem0},
			{data: modules[5], key: "Mm.mem1", memory: mem1},
		} {
			compiled := stagedMultiMemoryCompile(t, tc.data)
			if _, err := instantiateCore(compiled, InstantiateOptions{Imports: Imports{tc.key: tc.memory}}); err == nil || !strings.Contains(err.Error(), "active data segment") {
				compiled.Close()
				t.Fatalf("linking1 bounds trap %d = %v", i, err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("linking2 imported growth respects producer maximum", func(t *testing.T) {
		modules := stagedOfficialMultiMemoryModules(t, "linking2")
		if len(modules) != 2 {
			t.Fatalf("linking2 emitted %d modules, want 2", len(modules))
		}
		producerCompiled := stagedMultiMemoryCompile(t, modules[0])
		defer producerCompiled.Close()
		producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate linking2 producer: %v", err)
		}
		defer producer.Close()
		mem1, err := producer.ExportedMemory("mem1")
		if err != nil {
			t.Fatal(err)
		}
		consumerCompiled := stagedMultiMemoryCompile(t, modules[1])
		defer consumerCompiled.Close()
		consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"Mm.mem1": mem1}})
		if err != nil {
			t.Fatalf("instantiate linking2 grow consumer: %v", err)
		}
		defer consumer.Close()
		for i, tc := range []struct{ delta, want int32 }{
			{0, 1}, {2, 1}, {0, 3}, {1, 3}, {1, 4}, {0, 5}, {1, -1}, {0, 5},
		} {
			if got := tableTestCallI32(t, consumer, "grow", I32(tc.delta)); got != tc.want {
				t.Fatalf("linking2 grow step %d (%d) = %d, want %d", i, tc.delta, got, tc.want)
			}
		}
	})

	t.Run("linking3 memory ordering and trapped start persist", func(t *testing.T) {
		modules := stagedOfficialMultiMemoryModules(t, "linking3")
		if len(modules) != 6 {
			t.Fatalf("linking3 emitted %d modules, want 6", len(modules))
		}
		producerCompiled := stagedMultiMemoryCompile(t, modules[0])
		defer producerCompiled.Close()
		producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate linking3 multi-memory producer: %v", err)
		}
		defer producer.Close()
		mem1, err := producer.ExportedMemory("mem1")
		if err != nil {
			t.Fatal(err)
		}
		if got := tableTestCallI32(t, producer, "load", I32(0)); got != 0 {
			t.Fatalf("initial producer load(0) = %d, want 0", got)
		}

		unknownCompiled := stagedMultiMemoryCompile(t, modules[1])
		if _, err := instantiateCore(unknownCompiled, InstantiateOptions{Imports: Imports{
			"spectest.print": HostFunc(func(HostModule, []uint64, []uint64) {}),
			"Mm.mem1":        mem1,
		}}); err == nil || !strings.Contains(err.Error(), `missing imported table "Mm.tab"`) {
			unknownCompiled.Close()
			t.Fatalf("linking3 unknown table error = %v", err)
		}
		if err := unknownCompiled.Close(); err != nil {
			t.Fatal(err)
		}
		if got := tableTestCallI32(t, producer, "load", I32(0)); got != 0 {
			t.Fatalf("unknown import mutated memory: load(0) = %d", got)
		}

		dataTrapCompiled := stagedMultiMemoryCompile(t, modules[2])
		if _, err := instantiateCore(dataTrapCompiled, InstantiateOptions{Imports: Imports{"Mm.mem1": mem1}}); err == nil || !strings.Contains(err.Error(), "active data segment 1") {
			dataTrapCompiled.Close()
			t.Fatalf("linking3 second data segment error = %v", err)
		}
		if err := dataTrapCompiled.Close(); err != nil {
			t.Fatal(err)
		}
		if got := tableTestCallI32(t, producer, "load", I32(0)); got != 97 {
			t.Fatalf("first data segment did not persist: load(0) = %d, want 97", got)
		}
		if got := tableTestCallI32(t, producer, "load", I32(327670)); got != 0 {
			t.Fatalf("trapping data segment wrote partial bytes: load(327670) = %d", got)
		}

		mem1.Bytes()[0] = 0
		unsafeContextCompiled := stagedMultiMemoryCompile(t, modules[3])
		if _, err := instantiateCore(unsafeContextCompiled, InstantiateOptions{Imports: Imports{"Mm.mem1": mem1}}); err == nil || !strings.Contains(err.Error(), "active element segment 0 out of bounds") {
			unsafeContextCompiled.Close()
			t.Fatalf("linking3 active-element instantiation failure = %v", err)
		}
		if err := unsafeContextCompiled.Close(); err != nil {
			t.Fatal(err)
		}
		if got := tableTestCallI32(t, producer, "load", I32(0)); got != 0 {
			t.Fatalf("fail-closed basedata rejection mutated imported memory: %d", got)
		}

		storeCompiled := stagedMultiMemoryCompile(t, modules[4])
		defer storeCompiled.Close()
		store, err := instantiateCore(storeCompiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate linking3 store producer: %v", err)
		}
		defer store.Close()
		memory, err := store.ExportedMemory("memory")
		if err != nil {
			t.Fatal(err)
		}
		table, err := store.ExportedTable("table")
		if err != nil {
			t.Fatal(err)
		}
		startTrapCompiled := stagedMultiMemoryCompile(t, modules[5])
		if _, err := instantiateCore(startTrapCompiled, InstantiateOptions{Imports: Imports{
			"Ms.memory": memory, "Ms.table": table,
		}}); err == nil || !strings.Contains(err.Error(), "start function trapped") {
			startTrapCompiled.Close()
			t.Fatalf("linking3 start trap error = %v", err)
		}
		if err := startTrapCompiled.Close(); err != nil {
			t.Fatal(err)
		}
		if got := tableTestCallI32(t, store, "get memory[0]"); got != 104 {
			t.Fatalf("linking3 trapped start memory side effect = %d, want 104", got)
		}
		if got := tableTestCallI32(t, store, "get table[0]"); got != 0xdead {
			t.Fatalf("linking3 trapped start table side effect = %d, want 0xdead", got)
		}
	})
}

func importedMultiMemorySnapshotModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(
			memoryImportEntry("env", "first", 0x01, 0x01, 0x02),
			memoryImportEntry("env", "second", 0x01, 0x01, 0x02),
		)),
	)
}

func TestStagedMultiMemorySnapshotRejectsImportedShapeBeforeAttachment(t *testing.T) {
	compiled := stagedMultiMemoryCompile(t, importedMultiMemorySnapshotModule())
	defer compiled.Close()
	first, err := NewMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Capture(compiled, SnapshotOptions{Imports: Imports{"env.first": first, "env.second": second}})
	if err == nil || !strings.Contains(err.Error(), "multiple memories that are imported or shared") {
		t.Fatalf("imported multi-memory snapshot error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("snapshot rejection attached first memory: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("snapshot rejection attached second memory: %v", err)
	}
}
