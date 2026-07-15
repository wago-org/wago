//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func sameMemoryNativeFuncImport(module, name string, typeIndex byte) []byte {
	entry := append(wasmtest.Name(module), wasmtest.Name(name)...)
	return append(entry, 0x00, typeIndex)
}

func sameMemoryNativeOwnerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x03},
			[]byte{0x01, 0x0a, 0x0a},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("step", 0, 0),
			wasmtest.ExportEntry("boom", 0, 1),
			wasmtest.ExportEntry("grow", 0, 2),
			wasmtest.ExportEntry("mem", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x3f, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),
		)),
	)
}

func sameMemoryNativeTenantModule(importModule string, privatePages byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			sameMemoryNativeFuncImport(importModule, "step", 0),
			sameMemoryNativeFuncImport(importModule, "boom", 1),
			sameMemoryNativeFuncImport(importModule, "grow", 0),
			memoryImportEntry("A", "mem", 0x01, 0x01, 0x03),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, privatePages, privatePages})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("step", 0, 3),
			wasmtest.ExportEntry("boom", 0, 4),
			wasmtest.ExportEntry("grow", 0, 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x3f, 0x01, 0x6a, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x02, 0x1a, 0x3f, 0x00, 0x0b}),
		)),
	)
}

func sameMemoryNativeTableTenantModule(importModule string, privatePages byte) []byte {
	tableImport := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	tableImport = append(tableImport, byte(wasm.ExternTable), 0x70, 0x01, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			sameMemoryNativeFuncImport(importModule, "step", 0),
			sameMemoryNativeFuncImport(importModule, "boom", 1),
			sameMemoryNativeFuncImport(importModule, "grow", 0),
			memoryImportEntry("A", "mem", 0x01, 0x01, 0x03),
			tableImport,
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(0),
			wasmtest.ULEB(0), wasmtest.ULEB(2), wasmtest.ULEB(3),
		)),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, privatePages, privatePages})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("step", 0, 3),
			wasmtest.ExportEntry("boom", 0, 4),
			wasmtest.ExportEntry("grow", 0, 5),
			wasmtest.ExportEntry("is_null", 0, 6),
			wasmtest.ExportEntry("clear", 0, 7),
			wasmtest.ExportEntry("table_size", 0, 8),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x3f, 0x01, 0x6a, 0xfc, 0x10, 0x00, 0x6a, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x02, 0x1a, 0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
		)),
	)
}

func sameMemoryNativeGlobalTenantModule(importModule string, privatePages byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			sameMemoryNativeFuncImport(importModule, "step", 0),
			sameMemoryNativeFuncImport(importModule, "boom", 1),
			sameMemoryNativeFuncImport(importModule, "grow", 0),
			memoryImportEntry("A", "mem", 0x01, 0x01, 0x03),
			wasmtest.GlobalImportEntry("env", "counter", wasm.I32, true),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, privatePages, privatePages})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("step", 0, 3),
			wasmtest.ExportEntry("boom", 0, 4),
			wasmtest.ExportEntry("grow", 0, 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x3f, 0x01, 0x6a, 0x23, 0x00, 0x6a, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x02, 0x1a, 0x3f, 0x00, 0x0b}),
		)),
	)
}

type sameMemoryNativeChain struct {
	owner, middle, root *Instance
	ownerCompiled       *Compiled
	middleCompiled      *Compiled
	rootCompiled        *Compiled
	memory              *Memory
	counter             *Global
}

func instantiateSameMemoryNativeChain(tb testing.TB) *sameMemoryNativeChain {
	tb.Helper()
	ownerCompiled := stagedMultiMemoryCompile(tb, sameMemoryNativeOwnerModule())
	owner, err := instantiateCore(ownerCompiled, InstantiateOptions{})
	if err != nil {
		tb.Fatalf("instantiate owner: %v", err)
	}
	memory, err := owner.ExportedMemory("mem")
	if err != nil {
		tb.Fatalf("export owner memory: %v", err)
	}
	ownerStep, err := owner.ExportedFunc("step")
	if err != nil {
		tb.Fatal(err)
	}
	ownerBoom, err := owner.ExportedFunc("boom")
	if err != nil {
		tb.Fatal(err)
	}
	ownerGrow, err := owner.ExportedFunc("grow")
	if err != nil {
		tb.Fatal(err)
	}

	middleCompiled := stagedMultiMemoryCompile(tb, sameMemoryNativeTenantModule("A", 20))
	middle, err := instantiateCore(middleCompiled, InstantiateOptions{Imports: Imports{
		"A.step": ownerStep, "A.boom": ownerBoom, "A.grow": ownerGrow, "A.mem": memory,
	}})
	if err != nil {
		tb.Fatalf("instantiate middle: %v", err)
	}
	middleStep, err := middle.ExportedFunc("step")
	if err != nil {
		tb.Fatal(err)
	}
	middleBoom, err := middle.ExportedFunc("boom")
	if err != nil {
		tb.Fatal(err)
	}
	middleGrow, err := middle.ExportedFunc("grow")
	if err != nil {
		tb.Fatal(err)
	}

	rootCompiled := stagedMultiMemoryCompile(tb, sameMemoryNativeTenantModule("B", 30))
	root, err := instantiateCore(rootCompiled, InstantiateOptions{Imports: Imports{
		"B.step": middleStep, "B.boom": middleBoom, "B.grow": middleGrow, "A.mem": memory,
	}})
	if err != nil {
		tb.Fatalf("instantiate root: %v", err)
	}
	return &sameMemoryNativeChain{
		owner: owner, middle: middle, root: root,
		ownerCompiled: ownerCompiled, middleCompiled: middleCompiled, rootCompiled: rootCompiled,
		memory: memory,
	}
}

func instantiateSameMemoryNativeGlobalChain(tb testing.TB) *sameMemoryNativeChain {
	tb.Helper()
	ownerCompiled := stagedMultiMemoryCompile(tb, sameMemoryNativeOwnerModule())
	owner, err := instantiateCore(ownerCompiled, InstantiateOptions{})
	if err != nil {
		tb.Fatalf("instantiate global-composition owner: %v", err)
	}
	memory, err := owner.ExportedMemory("mem")
	if err != nil {
		tb.Fatalf("export global-composition owner memory: %v", err)
	}
	ownerStep, _ := owner.ExportedFunc("step")
	ownerBoom, _ := owner.ExportedFunc("boom")
	ownerGrow, _ := owner.ExportedFunc("grow")
	counter := NewGlobalI32(7, true)

	middleCompiled := stagedMultiMemoryCompile(tb, sameMemoryNativeGlobalTenantModule("A", 20))
	middle, err := instantiateCore(middleCompiled, InstantiateOptions{Imports: Imports{
		"A.step": ownerStep, "A.boom": ownerBoom, "A.grow": ownerGrow, "A.mem": memory,
		"env.counter": GlobalImport{Global: counter},
	}})
	if err != nil {
		tb.Fatalf("instantiate global-composition middle: %v", err)
	}
	middleStep, _ := middle.ExportedFunc("step")
	middleBoom, _ := middle.ExportedFunc("boom")
	middleGrow, _ := middle.ExportedFunc("grow")

	rootCompiled := stagedMultiMemoryCompile(tb, sameMemoryNativeGlobalTenantModule("B", 30))
	root, err := instantiateCore(rootCompiled, InstantiateOptions{Imports: Imports{
		"B.step": middleStep, "B.boom": middleBoom, "B.grow": middleGrow, "A.mem": memory,
		"env.counter": GlobalImport{Global: counter},
	}})
	if err != nil {
		tb.Fatalf("instantiate global-composition root: %v", err)
	}
	return &sameMemoryNativeChain{
		owner: owner, middle: middle, root: root,
		ownerCompiled: ownerCompiled, middleCompiled: middleCompiled, rootCompiled: rootCompiled,
		memory: memory, counter: counter,
	}
}

type sameMemoryNativeTableChain struct {
	*sameMemoryNativeChain
	tableOwner    *Instance
	tableCompiled *Compiled
	table         *Table
}

func instantiateSameMemoryNativeTableChain(tb testing.TB) *sameMemoryNativeTableChain {
	tb.Helper()
	ownerCompiled := stagedMultiMemoryCompile(tb, sameMemoryNativeOwnerModule())
	owner, err := instantiateCore(ownerCompiled, InstantiateOptions{})
	if err != nil {
		tb.Fatalf("instantiate table-composition owner: %v", err)
	}
	memory, err := owner.ExportedMemory("mem")
	if err != nil {
		tb.Fatalf("export table-composition owner memory: %v", err)
	}
	ownerStep, _ := owner.ExportedFunc("step")
	ownerBoom, _ := owner.ExportedFunc("boom")
	ownerGrow, _ := owner.ExportedFunc("grow")

	tableCompiled, err := Compile(nil, soleImportedTableProducerModule())
	if err != nil {
		tb.Fatalf("compile table-composition table owner: %v", err)
	}
	tableOwner, err := instantiateCore(tableCompiled, InstantiateOptions{})
	if err != nil {
		tb.Fatalf("instantiate table-composition table owner: %v", err)
	}
	table, err := tableOwner.ExportedTable("table")
	if err != nil {
		tb.Fatalf("export table-composition table: %v", err)
	}

	middleCompiled := stagedMultiMemoryCompile(tb, sameMemoryNativeTableTenantModule("A", 20))
	middle, err := instantiateCore(middleCompiled, InstantiateOptions{Imports: Imports{
		"A.step": ownerStep, "A.boom": ownerBoom, "A.grow": ownerGrow,
		"A.mem": memory, "env.table": table,
	}})
	if err != nil {
		tb.Fatalf("instantiate table-composition middle: %v", err)
	}
	middleStep, _ := middle.ExportedFunc("step")
	middleBoom, _ := middle.ExportedFunc("boom")
	middleGrow, _ := middle.ExportedFunc("grow")

	rootCompiled := stagedMultiMemoryCompile(tb, sameMemoryNativeTableTenantModule("B", 30))
	root, err := instantiateCore(rootCompiled, InstantiateOptions{Imports: Imports{
		"B.step": middleStep, "B.boom": middleBoom, "B.grow": middleGrow,
		"A.mem": memory, "env.table": table,
	}})
	if err != nil {
		tb.Fatalf("instantiate table-composition root: %v", err)
	}
	return &sameMemoryNativeTableChain{
		sameMemoryNativeChain: &sameMemoryNativeChain{
			owner: owner, middle: middle, root: root,
			ownerCompiled: ownerCompiled, middleCompiled: middleCompiled, rootCompiled: rootCompiled,
			memory: memory,
		},
		tableOwner: tableOwner, tableCompiled: tableCompiled, table: table,
	}
}

func (c *sameMemoryNativeTableChain) close() {
	if c == nil {
		return
	}
	c.sameMemoryNativeChain.close()
	_ = c.tableOwner.Close()
	_ = c.tableCompiled.Close()
}

func (c *sameMemoryNativeChain) close() {
	if c == nil {
		return
	}
	_ = c.root.Close()
	_ = c.middle.Close()
	_ = c.owner.Close()
	_ = c.rootCompiled.Close()
	_ = c.middleCompiled.Close()
	_ = c.ownerCompiled.Close()
	if c.counter != nil {
		_ = c.counter.Close()
	}
}

func TestStagedMultiMemoryNativeSameMemoryReentryLifecycle(t *testing.T) {
	chain := instantiateSameMemoryNativeChain(t)
	defer chain.close()

	if got := tableTestCallI32(t, chain.owner, "step", I32(1)); got != 11 {
		t.Fatalf("owner private memory identity = %d, want 11", got)
	}
	if got := tableTestCallI32(t, chain.middle, "step", I32(1)); got != 31 {
		t.Fatalf("root re-entry = %d, want 31", got)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(1)); got != 61 {
		t.Fatalf("nested re-entry = %d, want 61", got)
	}
	if got := tableTestCallI32(t, chain.root, "grow", I32(1)); got != 2 {
		t.Fatalf("nested shared memory.grow visibility = %d, want 2 pages", got)
	}
	if got := tableTestCallI32(t, chain.owner, "grow", I32(0)); got != 2 {
		t.Fatalf("owner image after nested memory.grow = %d, want 2 pages", got)
	}

	if _, err := chain.root.Invoke("boom"); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("nested trap = %v, want unreachable", err)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(2)); got != 62 {
		t.Fatalf("post-trap nested re-entry = %d, want 62", got)
	}
	if got := tableTestCallI32(t, chain.owner, "step", I32(2)); got != 12 {
		t.Fatalf("post-trap owner image = %d, want 12", got)
	}

	var wg sync.WaitGroup
	errs := make(chan string, 2)
	for _, tc := range []struct {
		in   *Instance
		want int32
	}{{chain.middle, 37}, {chain.root, 67}} {
		wg.Add(1)
		go func(in *Instance, want int32) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				out, err := in.Invoke("step", I32(7))
				if err != nil || len(out) != 1 || AsI32(out[0]) != want {
					errs <- strings.TrimSpace(strings.Join([]string{"call", errorString(err)}, ": "))
					return
				}
			}
		}(tc.in, tc.want)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent re-entry: %s", err)
	}

	if err := chain.owner.Close(); err != nil {
		t.Fatalf("logical owner close: %v", err)
	}
	if err := chain.middle.Close(); err != nil {
		t.Fatalf("logical middle close: %v", err)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(3)); got != 63 {
		t.Fatalf("retained nested call after producer closes = %d, want 63", got)
	}
	for name, in := range map[string]*Instance{"owner": chain.owner, "middle": chain.middle} {
		in.lifeMu.Lock()
		closed := in.resourcesClosed
		in.lifeMu.Unlock()
		if closed {
			t.Fatalf("%s resources closed before retained root released", name)
		}
	}
	if err := chain.root.Close(); err != nil {
		t.Fatalf("root close: %v", err)
	}
	for name, in := range map[string]*Instance{"owner": chain.owner, "middle": chain.middle, "root": chain.root} {
		in.lifeMu.Lock()
		closed := in.resourcesClosed
		in.lifeMu.Unlock()
		if !closed {
			t.Fatalf("%s resources remained after final close", name)
		}
	}
}

func TestStagedMultiMemoryNativeSameMemoryImportedTableComposition(t *testing.T) {
	chain := instantiateSameMemoryNativeTableChain(t)
	defer chain.close()

	for name, compiled := range map[string]*Compiled{"middle": chain.middleCompiled, "root": chain.rootCompiled} {
		if !compiled.memoryDir.stagedSharedBasedataSafe || !compiled.memoryDir.stagedSharedBasedataNativeCalls {
			t.Fatalf("%s did not retain combined imported-table/native-call proof", name)
		}
	}
	if got := tableTestCallI32(t, chain.middle, "step", I32(1)); got != 32 {
		t.Fatalf("imported-table middle re-entry = %d, want 32", got)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(1)); got != 63 {
		t.Fatalf("imported-table nested re-entry = %d, want 63", got)
	}
	if got := tableTestCallI32(t, chain.root, "table_size"); got != 1 {
		t.Fatalf("composed imported table size = %d, want 1", got)
	}
	if got := tableTestCallI32(t, chain.root, "is_null", I32(0)); got != 0 {
		t.Fatalf("composed imported table entry null = %d, want 0", got)
	}
	if _, err := chain.root.Invoke("clear", I32(1)); err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("composed imported table OOB set = %v", err)
	}
	if got := tableTestCallI32(t, chain.root, "is_null", I32(0)); got != 0 {
		t.Fatalf("trapping composed table.set changed entry: %d", got)
	}
	if got := tableTestCallI32(t, chain.root, "grow", I32(1)); got != 2 {
		t.Fatalf("composed nested shared memory.grow = %d, want 2", got)
	}
	if got := tableTestCallI32(t, chain.owner, "grow", I32(0)); got != 2 {
		t.Fatalf("composed imported-table owner grow visibility = %d, want 2", got)
	}
	if _, err := chain.root.Invoke("boom"); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("composed imported-table nested trap = %v", err)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(2)); got != 64 {
		t.Fatalf("composed imported-table post-trap call = %d, want 64", got)
	}

	var wg sync.WaitGroup
	errs := make(chan string, 2)
	for _, tc := range []struct {
		in   *Instance
		name string
		want int32
	}{{chain.middle, "step", 38}, {chain.root, "step", 69}} {
		wg.Add(1)
		go func(in *Instance, name string, want int32) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				out, err := in.Invoke(name, I32(7))
				if err != nil || len(out) != 1 || AsI32(out[0]) != want {
					errs <- strings.TrimSpace(strings.Join([]string{"table-composed call", errorString(err)}, ": "))
					return
				}
			}
		}(tc.in, tc.name, tc.want)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent imported-table re-entry: %s", err)
	}

	if _, err := chain.middle.c.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "codec v26 cannot serialize") {
		t.Fatalf("table-composed codec binding = %v, want explicit rejection", err)
	}
	if _, err := Capture(chain.middleCompiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tables cannot be snapshotted") {
		t.Fatalf("table-composed snapshot = %v, want table rejection", err)
	}
	if _, err := Compile(nil, sameMemoryNativeTableTenantModule("A", 20)); err == nil {
		t.Fatal("public compile admitted imported-table same-memory native composition")
	}
	ownerStep, _ := chain.owner.ExportedFunc("step")
	ownerBoom, _ := chain.owner.ExportedFunc("boom")
	ownerGrow, _ := chain.owner.ExportedFunc("grow")
	consumerCompiled := stagedMultiMemoryCompile(t, sameMemoryNativeTableTenantModule("A", 20))
	defer consumerCompiled.Close()
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{
		"A.step": HostFunc(func(HostModule, []uint64, []uint64) {}), "A.boom": ownerBoom, "A.grow": ownerGrow,
		"A.mem": chain.memory, "env.table": chain.table,
	}}); err == nil || !strings.Contains(err.Error(), "retained same-memory InstanceExport") {
		t.Fatalf("table-composed host callback = %v, want retained-native rejection", err)
	}
	foreignCompiled := stagedMultiMemoryCompile(t, sameMemoryNativeOwnerModule())
	defer foreignCompiled.Close()
	foreign, err := instantiateCore(foreignCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	foreignMemory, err := foreign.ExportedMemory("mem")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{
		"A.step": ownerStep, "A.boom": ownerBoom, "A.grow": ownerGrow,
		"A.mem": foreignMemory, "env.table": chain.table,
	}}); err == nil || !strings.Contains(err.Error(), "exact imported memory 0") {
		t.Fatalf("table-composed foreign-memory binding = %v", err)
	}
	returnCall := sameMemoryNativeTableTenantModule("A", 20)
	for i := range returnCall {
		if i+2 < len(returnCall) && returnCall[i] == 0x10 && returnCall[i+1] == 0x00 && returnCall[i+2] == 0x0b {
			returnCall[i] = 0x12
			break
		}
	}
	returnModule, err := wasm.DecodeModule(returnCall)
	if err != nil {
		t.Fatalf("decode table-composed return_call gate module: %v", err)
	}
	if safe, _ := stagedSharedBasedataSafety(returnModule); safe {
		t.Fatal("imported-table return_call incorrectly entered the same-memory serializer")
	}

	if err := chain.tableOwner.Close(); err != nil {
		t.Fatalf("logical composed table owner close: %v", err)
	}
	if err := chain.owner.Close(); err != nil {
		t.Fatalf("logical composed memory owner close: %v", err)
	}
	if err := chain.middle.Close(); err != nil {
		t.Fatalf("logical composed function owner close: %v", err)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(3)); got != 65 {
		t.Fatalf("table-composed call after independent owner closes = %d, want 65", got)
	}
	for name, in := range map[string]*Instance{"memory": chain.owner, "function": chain.middle, "table": chain.tableOwner} {
		in.lifeMu.Lock()
		closed := in.resourcesClosed
		in.lifeMu.Unlock()
		if closed {
			t.Fatalf("table-composed %s resources closed before root release", name)
		}
	}
	if _, err := chain.root.Invoke("clear", I32(0)); err != nil {
		t.Fatalf("clear composed imported table: %v", err)
	}
	if got := tableTestCallI32(t, chain.root, "is_null", I32(0)); got != 1 {
		t.Fatalf("cleared composed imported table entry = %d, want null", got)
	}
	if err := chain.root.Close(); err != nil {
		t.Fatalf("close table-composed root: %v", err)
	}
	for name, in := range map[string]*Instance{"memory": chain.owner, "function": chain.middle, "table": chain.tableOwner, "root": chain.root} {
		in.lifeMu.Lock()
		closed := in.resourcesClosed
		in.lifeMu.Unlock()
		if !closed {
			t.Fatalf("table-composed %s resources remained after root close", name)
		}
	}
}

func TestStagedMultiMemoryNativeSameMemoryImportedGlobalComposition(t *testing.T) {
	chain := instantiateSameMemoryNativeGlobalChain(t)
	defer chain.close()

	if got := tableTestCallI32(t, chain.middle, "step", I32(1)); got != 38 {
		t.Fatalf("imported-global middle re-entry = %d, want 38", got)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(1)); got != 75 {
		t.Fatalf("imported-global nested re-entry = %d, want 75", got)
	}
	if err := chain.counter.Set(I32(9)); err != nil {
		t.Fatalf("set composed imported global: %v", err)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(1)); got != 79 {
		t.Fatalf("updated imported-global nested re-entry = %d, want 79", got)
	}
	if got := tableTestCallI32(t, chain.root, "grow", I32(1)); got != 2 {
		t.Fatalf("composed nested shared memory.grow visibility = %d, want 2", got)
	}
	if got := tableTestCallI32(t, chain.owner, "grow", I32(0)); got != 2 {
		t.Fatalf("composed owner image after grow = %d, want 2", got)
	}
	if _, err := chain.root.Invoke("boom"); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("composed nested trap = %v, want unreachable", err)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(2)); got != 80 {
		t.Fatalf("composed post-trap nested re-entry = %d, want 80", got)
	}

	var wg sync.WaitGroup
	errs := make(chan string, 2)
	for _, tc := range []struct {
		in   *Instance
		want int32
	}{{chain.middle, 46}, {chain.root, 85}} {
		wg.Add(1)
		go func(in *Instance, want int32) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				out, err := in.Invoke("step", I32(7))
				if err != nil || len(out) != 1 || AsI32(out[0]) != want {
					errs <- strings.TrimSpace(strings.Join([]string{"composed call", errorString(err)}, ": "))
					return
				}
			}
		}(tc.in, tc.want)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent imported-global re-entry: %s", err)
	}

	if _, err := chain.middle.c.MarshalBinary(); err != nil {
		t.Fatalf("marshal composed structural module: %v", err)
	}
	if _, err := Capture(chain.middleCompiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "imported or shared") {
		t.Fatalf("composed snapshot = %v, want imported/shared rejection", err)
	}
	if _, err := Compile(nil, sameMemoryNativeGlobalTenantModule("A", 20)); err == nil {
		t.Fatal("public compile admitted imported-global same-memory native composition")
	}
	ownerStep, _ := chain.owner.ExportedFunc("step")
	ownerBoom, _ := chain.owner.ExportedFunc("boom")
	ownerGrow, _ := chain.owner.ExportedFunc("grow")
	consumerCompiled := stagedMultiMemoryCompile(t, sameMemoryNativeGlobalTenantModule("A", 20))
	defer consumerCompiled.Close()
	hostConsumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{
		"A.step": HostFunc(func(_ HostModule, _ []uint64, results []uint64) { results[0] = 0 }), "A.boom": ownerBoom, "A.grow": ownerGrow,
		"A.mem": chain.memory, "env.counter": GlobalImport{Global: chain.counter},
	}})
	if err != nil {
		t.Fatalf("composed host callback binding: %v", err)
	}
	if _, err := hostConsumer.Invoke("step", I32(1)); err != nil {
		t.Fatalf("composed host callback call: %v", err)
	}
	if err := hostConsumer.Close(); err != nil {
		t.Fatal(err)
	}
	foreignCompiled := stagedMultiMemoryCompile(t, sameMemoryNativeOwnerModule())
	defer foreignCompiled.Close()
	foreign, err := instantiateCore(foreignCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	foreignMemory, err := foreign.ExportedMemory("mem")
	if err != nil {
		t.Fatal(err)
	}
	foreignConsumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{
		"A.step": ownerStep, "A.boom": ownerBoom, "A.grow": ownerGrow,
		"A.mem": foreignMemory, "env.counter": GlobalImport{Global: chain.counter},
	}})
	if err != nil {
		t.Fatalf("composed foreign-memory binding: %v", err)
	}
	if _, err := foreignConsumer.Invoke("step", I32(1)); err != nil {
		t.Fatalf("composed foreign-memory call: %v", err)
	}
	if err := foreignConsumer.Close(); err != nil {
		t.Fatal(err)
	}
	returnCall := sameMemoryNativeGlobalTenantModule("A", 20)
	for i := range returnCall {
		if i+2 < len(returnCall) && returnCall[i] == 0x10 && returnCall[i+1] == 0x00 && returnCall[i+2] == 0x0b {
			returnCall[i] = 0x12
			break
		}
	}
	returnModule, err := wasm.DecodeModule(returnCall)
	if err != nil {
		t.Fatalf("decode composed return_call gate module: %v", err)
	}
	features := NewRuntimeConfig().frontendFeatures()
	features.MultiMemory = true
	if _, err := compileWithFrontendFeatures(NewRuntimeConfig(), returnCall, features); err == nil || !strings.Contains(err.Error(), "tail") {
		t.Fatalf("imported-global return_call without staged tail feature = %v, want rejection (module=%d funcs)", err, len(returnModule.Code))
	}

	if err := chain.counter.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
		t.Fatalf("composed global close with live tenants = %v", err)
	}
	if err := chain.owner.Close(); err != nil {
		t.Fatalf("logical composed memory owner close: %v", err)
	}
	if err := chain.middle.Close(); err != nil {
		t.Fatalf("logical composed function owner close: %v", err)
	}
	if got := tableTestCallI32(t, chain.root, "step", I32(3)); got != 81 {
		t.Fatalf("composed call after producer closes = %d, want 81", got)
	}
	for name, in := range map[string]*Instance{"owner": chain.owner, "middle": chain.middle} {
		in.lifeMu.Lock()
		closed := in.resourcesClosed
		in.lifeMu.Unlock()
		if closed {
			t.Fatalf("composed %s resources closed before root release", name)
		}
	}
	if err := chain.counter.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
		t.Fatalf("composed global close with retained root = %v", err)
	}
	if err := chain.root.Close(); err != nil {
		t.Fatalf("close composed root: %v", err)
	}
	for name, in := range map[string]*Instance{"owner": chain.owner, "middle": chain.middle, "root": chain.root} {
		in.lifeMu.Lock()
		closed := in.resourcesClosed
		in.lifeMu.Unlock()
		if !closed {
			t.Fatalf("composed %s resources remained after root close", name)
		}
	}
	if err := chain.counter.Close(); err != nil {
		t.Fatalf("close composed global after tenants: %v", err)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestStagedMultiMemoryNativeContextProductAndGates(t *testing.T) {
	chain := instantiateSameMemoryNativeChain(t)
	defer chain.close()

	blob, err := chain.middle.c.MarshalBinary()
	if err != nil {
		t.Fatalf("codec v26 structural same-memory module: %v", err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("public reload of staged multi-memory module = %v, want feature gate", err)
	}
	if _, err := Capture(chain.middleCompiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "imported or shared") {
		t.Fatalf("same-memory native snapshot = %v, want imported/shared rejection", err)
	}
	if _, err := Compile(nil, sameMemoryNativeTenantModule("A", 20)); err == nil {
		t.Fatal("public default compile admitted staged multi-memory native calls")
	}

	foreignCompiled := stagedMultiMemoryCompile(t, sameMemoryNativeOwnerModule())
	defer foreignCompiled.Close()
	foreign, err := instantiateCore(foreignCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	foreignMemory, err := foreign.ExportedMemory("mem")
	if err != nil {
		t.Fatal(err)
	}
	ownerStep, _ := chain.owner.ExportedFunc("step")
	ownerBoom, _ := chain.owner.ExportedFunc("boom")
	ownerGrow, _ := chain.owner.ExportedFunc("grow")
	consumerCompiled := stagedMultiMemoryCompile(t, sameMemoryNativeTenantModule("A", 20))
	defer consumerCompiled.Close()
	foreignConsumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{
		"A.step": ownerStep, "A.boom": ownerBoom, "A.grow": ownerGrow, "A.mem": foreignMemory,
	}})
	if err != nil {
		t.Fatalf("foreign-memory native binding: %v", err)
	}
	if got := tableTestCallI32(t, foreignConsumer, "step", I32(1)); got != 31 {
		t.Fatalf("foreign-memory native call = %d, want 31", got)
	}
	if err := foreignConsumer.Close(); err != nil {
		t.Fatal(err)
	}
	hostConsumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{
		"A.step": HostFunc(func(_ HostModule, _ []uint64, results []uint64) { results[0] = 0 }), "A.boom": ownerBoom, "A.grow": ownerGrow, "A.mem": chain.memory,
	}})
	if err != nil {
		t.Fatalf("host callback binding: %v", err)
	}
	if got := tableTestCallI32(t, hostConsumer, "step", I32(1)); got != 0 {
		t.Fatalf("host callback call = %d, want 0", got)
	}
	if err := hostConsumer.Close(); err != nil {
		t.Fatal(err)
	}

	returnCall := sameMemoryNativeTenantModule("A", 20)
	for i := range returnCall {
		if i+2 < len(returnCall) && returnCall[i] == 0x10 && returnCall[i+1] == 0x00 && returnCall[i+2] == 0x0b {
			returnCall[i] = 0x12
			break
		}
	}
	returnModule, err := wasm.DecodeModule(returnCall)
	if err != nil {
		t.Fatalf("decode return_call gate module: %v", err)
	}
	features := NewRuntimeConfig().frontendFeatures()
	features.MultiMemory = true
	if _, err := compileWithFrontendFeatures(NewRuntimeConfig(), returnCall, features); err == nil || !strings.Contains(err.Error(), "tail") {
		t.Fatalf("return_call without staged tail feature = %v, want fail-closed rejection (module=%d funcs)", err, len(returnModule.Code))
	}
}

func TestStagedMultiMemoryNativeContextAccounting(t *testing.T) {
	if runtime.InstanceContextBytes != 72 {
		t.Fatalf("native instance context = %d bytes, want 72", runtime.InstanceContextBytes)
	}
	if abi.BasedataSize != 272 {
		t.Fatalf("basedata = %d bytes, want 272", abi.BasedataSize)
	}
}

func BenchmarkStagedMultiMemoryNativeSameMemoryImportedTableNestedCall(b *testing.B) {
	chain := instantiateSameMemoryNativeTableChain(b)
	defer chain.close()
	if _, err := chain.root.Invoke("step", I32(1)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := chain.root.Invoke("step", I32(1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedMultiMemoryNativeSameMemoryImportedGlobalNestedCall(b *testing.B) {
	chain := instantiateSameMemoryNativeGlobalChain(b)
	defer chain.close()
	if _, err := chain.root.Invoke("step", I32(1)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := chain.root.Invoke("step", I32(1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedMultiMemoryNativeSameMemoryNestedCall(b *testing.B) {
	chain := instantiateSameMemoryNativeChain(b)
	defer chain.close()
	if _, err := chain.root.Invoke("step", I32(1)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := chain.root.Invoke("step", I32(1)); err != nil {
			b.Fatal(err)
		}
	}
}
