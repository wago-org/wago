//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/wasmtest"
)

func quotaULEB64(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func quotaMemoryType(min, max uint64, addr64 bool) []byte {
	flags := byte(0x01)
	if addr64 {
		flags |= 0x04
	}
	out := append([]byte{flags}, quotaULEB64(min)...)
	return append(out, quotaULEB64(max)...)
}

func quotaMemoryModule(min, max uint64, addr64 bool) []byte {
	valueType := wasm.I32
	if addr64 {
		valueType = wasm.I64
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{valueType}, []wasm.ValType{valueType}),
			wasmtest.FuncType(nil, []wasm.ValType{valueType}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec(quotaMemoryType(min, max, addr64))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
		)),
	)
}

func quotaMemoryImport(module, name string, min, max uint64, addr64 bool) []byte {
	entry := append(wasmtest.Name(module), wasmtest.Name(name)...)
	entry = append(entry, byte(wasm.ExternMem))
	return append(entry, quotaMemoryType(min, max, addr64)...)
}

func quotaImportedMemoryModule(min, max uint64) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(quotaMemoryImport("env", "memory", min, max, false))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
		)),
	)
}

func quotaMultiMemoryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(quotaMemoryType(1, 4, false), quotaMemoryType(2, 4, false))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow0", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("grow1", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x01, 0x0b}),
		)),
	)
}

func invokeOne(t *testing.T, in *Instance, name string, args ...uint64) uint64 {
	t.Helper()
	values, err := in.Invoke(name, args...)
	if err != nil || len(values) != 1 {
		t.Fatalf("%s(%v) = %v, %v", name, args, values, err)
	}
	return values[0]
}

func TestMemoryPageQuotaExactInitialAndGrowthBoundaries(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(2)
	compiled, err := Compile(cfg, quotaMemoryModule(2, 4, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("initial memory exactly at quota: %v", err)
	}
	defer in.Close()
	if got := invokeOne(t, in, "grow", I32(0)); got != 2 {
		t.Fatalf("zero growth at quota = %d, want old size 2", got)
	}
	if got := invokeOne(t, in, "grow", I32(1)); uint32(got) != ^uint32(0) {
		t.Fatalf("growth beyond quota = %#x, want memory.grow failure", got)
	}
	if got := invokeOne(t, in, "size"); got != 2 {
		t.Fatalf("size after failed growth = %d, want 2", got)
	}

	over, err := Compile(cfg, quotaMemoryModule(3, 4, false))
	if err != nil {
		t.Fatal(err)
	}
	defer over.Close()
	if in, err := Instantiate(over, InstantiateOptions{}); in != nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("initial memory above quota = %v, %v; want resource limit", in, err)
	}
}

func TestMemoryPageQuotaAllowsGrowthToLimit(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(2)
	compiled, err := Compile(cfg, quotaMemoryModule(1, 4, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := invokeOne(t, in, "grow", I32(1)); got != 1 {
		t.Fatalf("growth to quota = %d, want old size 1", got)
	}
	if got := invokeOne(t, in, "grow", I32(1)); uint32(got) != ^uint32(0) {
		t.Fatalf("growth past quota = %#x, want failure", got)
	}
}

func TestMemoryPageQuotaImportedMemory(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(2)
	compiled, err := Compile(cfg, quotaImportedMemoryModule(1, 4))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, err := NewMemory(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.memory": memory}})
	if err != nil {
		t.Fatal(err)
	}
	if got := invokeOne(t, in, "grow", I32(1)); got != 1 {
		t.Fatalf("imported growth to quota = %d, want 1", got)
	}
	if got := invokeOne(t, in, "grow", I32(1)); uint32(got) != ^uint32(0) {
		t.Fatalf("imported growth past quota = %#x, want failure", got)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}

	tooLarge, err := NewMemory(3, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer tooLarge.Close()
	if in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.memory": tooLarge}}); in != nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("oversized imported live memory = %v, %v; want resource limit", in, err)
	}
}

func TestMemoryPageQuotaImportedMemoryUsesColdPerInstanceDirectory(t *testing.T) {
	compile := func(limit uint32) *Compiled {
		t.Helper()
		compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(limit), quotaImportedMemoryModule(1, 4))
		if err != nil {
			t.Fatal(err)
		}
		return compiled
	}
	policyPtr := func(instance *Instance) uintptr {
		t.Helper()
		if instance.memoryDir != nil {
			t.Fatal("single-memory quota installed indexed-memory hot-path state")
		}
		ptr := instance.jm.CaptureInstanceContext().MemoryDirPtr
		if ptr == 0 {
			t.Fatal("single-memory quota has no native policy directory")
		}
		return ptr
	}

	lowMemory, err := NewMemory(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer lowMemory.Close()
	highMemory, err := NewMemory(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer highMemory.Close()
	low := compile(2)
	defer low.Close()
	lowInstance, err := Instantiate(low, InstantiateOptions{Imports: Imports{"env.memory": lowMemory}})
	if err != nil {
		t.Fatal(err)
	}
	defer lowInstance.Close()
	lowPtr := policyPtr(lowInstance)

	high := compile(4)
	defer high.Close()
	highInstance, err := Instantiate(high, InstantiateOptions{Imports: Imports{"env.memory": highMemory}})
	if err != nil {
		t.Fatal(err)
	}
	defer highInstance.Close()
	highPtr := policyPtr(highInstance)
	if lowPtr == highPtr {
		t.Fatalf("per-instance native policy directories alias at %#x", lowPtr)
	}
	if got := invokeOne(t, lowInstance, "grow", I32(1)); got != 1 {
		t.Fatalf("low policy growth = %d, want 1", got)
	}
	if got := invokeOne(t, lowInstance, "grow", I32(1)); uint32(got) != ^uint32(0) {
		t.Fatalf("low policy growth past quota = %#x", got)
	}
	if got := invokeOne(t, highInstance, "grow", I32(1)); got != 1 {
		t.Fatalf("high policy first growth = %d, want 1", got)
	}
	if got := invokeOne(t, highInstance, "grow", I32(1)); got != 2 {
		t.Fatalf("high policy second growth = %d, want 2", got)
	}
	if got := invokeOne(t, highInstance, "grow", I32(1)); got != 3 {
		t.Fatalf("high policy third growth = %d, want 3", got)
	}
	if got := invokeOne(t, highInstance, "grow", I32(1)); uint32(got) != ^uint32(0) {
		t.Fatalf("high policy growth past quota = %#x", got)
	}
}

func TestMemoryPageQuotaMultiMemory(t *testing.T) {
	requireCompleteCore3Backend(t)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(2)
	compiled, err := Compile(cfg, quotaMultiMemoryModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := invokeOne(t, in, "grow0", I32(1)); got != 1 {
		t.Fatalf("memory 0 growth to quota = %d, want 1", got)
	}
	if got := invokeOne(t, in, "grow0", I32(1)); uint32(got) != ^uint32(0) {
		t.Fatalf("memory 0 growth past quota = %#x", got)
	}
	if got := invokeOne(t, in, "grow1", I32(1)); uint32(got) != ^uint32(0) {
		t.Fatalf("memory 1 growth past quota = %#x", got)
	}
}

func TestMemoryPageQuotaMemory64(t *testing.T) {
	requireCompleteCore3Backend(t)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(2)
	compiled, err := Compile(cfg, quotaMemoryModule(1, 4, true))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := invokeOne(t, in, "grow", I64(1)); got != 1 {
		t.Fatalf("memory64 growth to quota = %d, want 1", got)
	}
	if got := invokeOne(t, in, "grow", I64(1)); got != ^uint64(0) {
		t.Fatalf("memory64 growth past quota = %#x, want failure", got)
	}
}

func TestMemoryPageQuotaZeroMeansUnlimited(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(0)
	compiled, err := Compile(cfg, quotaMemoryModule(1, 3, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := invokeOne(t, in, "grow", I32(2)); got != 1 {
		t.Fatalf("unlimited growth to declared maximum = %d, want 1", got)
	}
}

func TestCompileByteAndNativeCodeQuotas(t *testing.T) {
	source := quotaMemoryModule(1, 1, false)
	if _, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMaxModuleBytes(uint64(len(source)-1)), source); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("module-byte quota error = %v, want resource limit", err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), source)
	if err != nil {
		t.Fatal(err)
	}
	codeBytes := uint64(len(compiled.code))
	compiled.Close()
	if codeBytes == 0 {
		t.Fatal("quota fixture generated no native code")
	}
	exact, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMaxNativeCodeBytes(codeBytes), source)
	if err != nil {
		t.Fatalf("exact native-code quota: %v", err)
	}
	exact.Close()
	if _, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMaxNativeCodeBytes(codeBytes-1), source); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("native-code quota error = %v, want resource limit", err)
	}

	compiled, err = Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMaxNativeCodeBytes(codeBytes - 1)))
	defer rt.Close()
	if mod, err := rt.Module(compiled); mod != nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("decoded artifact native-code quota = %v, %v; want resource limit", mod, err)
	}
}

func TestMemoryPageQuotaDirectoryCountsTowardMetadataQuota(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), quotaMemoryModule(1, 4, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if err := compiled.validateCached(); err != nil {
		t.Fatal(err)
	}
	exact := uint64(compiled.executionView().instantiateArenaNeed + abi.MemoryDirEntryBytes)
	instantiate := func(limit uint64) error {
		rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(2).WithMaxInstanceMetadataBytes(limit)))
		defer rt.Close()
		mod, err := rt.Module(compiled)
		if err != nil {
			return err
		}
		defer mod.Close()
		instance, err := rt.Instantiate(context.Background(), mod)
		if instance != nil {
			defer instance.Close()
		}
		return err
	}
	if err := instantiate(exact); err != nil {
		t.Fatalf("exact quota including memory policy directory: %v", err)
	}
	if err := instantiate(exact - 1); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("quota excluding one policy-directory byte = %v, want resource limit", err)
	}
}

func manyGlobalsModule(count uint32) []byte {
	payload := append([]byte{}, wasmtest.ULEB(count)...)
	entry := []byte{wasm.MustEncodeValType(wasm.I64), 0x00, 0x42, 0x00, 0x0b}
	for i := uint32(0); i < count; i++ {
		payload = append(payload, entry...)
	}
	return wasmtest.Module(wasmtest.Section(6, payload))
}

func TestInstanceMetadataArenaCanExceedOneMiBAndQuotaIsExact(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), manyGlobalsModule(65536))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if err := compiled.validateCached(); err != nil {
		t.Fatal(err)
	}
	if compiled.executionView().instantiateArenaNeed <= 1<<20 {
		t.Fatalf("metadata need = %d, want above old 1 MiB ceiling", compiled.executionView().instantiateArenaNeed)
	}

	exact := uint64(compiled.executionView().instantiateArenaNeed)
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMaxInstanceMetadataBytes(exact)))
	mod, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("exact metadata quota: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}

	rt = NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMaxInstanceMetadataBytes(exact - 1)))
	defer rt.Close()
	mod, err = rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	if in, err := rt.Instantiate(context.Background(), mod); in != nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("metadata quota below exact need = %v, %v; want resource limit", in, err)
	}
}
