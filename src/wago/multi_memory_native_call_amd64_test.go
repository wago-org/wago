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

type sameMemoryNativeChain struct {
	owner, middle, root *Instance
	ownerCompiled       *Compiled
	middleCompiled      *Compiled
	rootCompiled        *Compiled
	memory              *Memory
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
