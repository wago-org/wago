//go:build linux && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func multiMemoryExecTypes() []byte {
	return wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	))
}

func multiMemoryExecFuncs() []byte {
	return wasmtest.Section(3, wasmtest.Vec(
		wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2),
	))
}

func multiMemoryExecExports() []byte {
	return wasmtest.Section(7, wasmtest.Vec(
		wasmtest.ExportEntry("size1", 0, 0),
		wasmtest.ExportEntry("store1", 0, 1),
		wasmtest.ExportEntry("load1", 0, 2),
		wasmtest.ExportEntry("load0", 0, 3),
		wasmtest.ExportEntry("m0", 2, 0),
		wasmtest.ExportEntry("m1", 2, 1),
	))
}

func multiMemoryExecCode() []byte {
	return wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0x3f, 0x01, 0x0b}),
		wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x36, 0x42, 0x01, 0x00, 0x0b}),
		wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x42, 0x01, 0x00, 0x0b}),
		wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}),
	))
}

func localMultiMemoryExecModule() []byte {
	return wasmtest.Module(
		multiMemoryExecTypes(),
		multiMemoryExecFuncs(),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x01}, // memory 0: min=1 max=1
			[]byte{0x01, 0x02, 0x02}, // memory 1: min=2 max=2
		)),
		multiMemoryExecExports(),
		multiMemoryExecCode(),
	)
}

func importedMultiMemoryExecModule() []byte {
	memoryImport := func(name string, min, max byte) []byte {
		entry := append(wasmtest.Name("env"), wasmtest.Name(name)...)
		return append(entry, 0x02, 0x01, min, max)
	}
	return wasmtest.Module(
		multiMemoryExecTypes(),
		wasmtest.Section(2, wasmtest.Vec(memoryImport("m0", 1, 1), memoryImport("m1", 2, 2))),
		multiMemoryExecFuncs(),
		multiMemoryExecExports(),
		multiMemoryExecCode(),
	)
}

func stagedMultiMemoryCompile(t *testing.T, module []byte) *Compiled {
	t.Helper()
	t.Setenv("WAGO_BOUNDS", "explicit")
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.MultiMemory = true
	compiled, err := compileWithFrontendFeatures(cfg, module, features)
	if err != nil {
		t.Fatalf("staged multi-memory compile: %v", err)
	}
	t.Cleanup(func() { _ = compiled.Close() })
	return compiled
}

func exerciseIndexedMemory1(t *testing.T, in *Instance) {
	t.Helper()
	if got := tableTestCallI32(t, in, "size1"); got != 2 {
		t.Fatalf("memory.size 1 = %d, want 2", got)
	}
	if _, err := in.Invoke("store1", I32(32), I32(0x12345678)); err != nil {
		t.Fatalf("memory-1 store: %v", err)
	}
	if got := tableTestCallI32(t, in, "load1", I32(32)); uint32(got) != 0x12345678 {
		t.Fatalf("memory-1 load = %#x, want %#x", uint32(got), uint32(0x12345678))
	}
	if got := tableTestCallI32(t, in, "load0", I32(32)); got != 0 {
		t.Fatalf("memory-0 isolation load = %#x, want 0", uint32(got))
	}
	if _, err := in.Invoke("store1", I32(2*65536-2), I32(7)); err == nil {
		t.Fatal("cross-boundary memory-1 store unexpectedly succeeded")
	}
	if got := tableTestCallI32(t, in, "load1", I32(32)); uint32(got) != 0x12345678 {
		t.Fatalf("trapping store changed prior memory-1 state: %#x", uint32(got))
	}
}

func TestStagedMultiMemoryLocalAndImportedExecution(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		compiled := stagedMultiMemoryCompile(t, localMultiMemoryExecModule())
		in, err := instantiateCore(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate local memories: %v", err)
		}
		defer in.Close()
		exerciseIndexedMemory1(t, in)
		m1, err := in.ExportedMemory("m1")
		if err != nil {
			t.Fatalf("export memory 1: %v", err)
		}
		if got := binary.LittleEndian.Uint32(m1.Bytes()[32:]); got != 0x12345678 {
			t.Fatalf("exported memory-1 bytes = %#x", got)
		}
		if err := validateSnapshotModule(compiled); err == nil || !strings.Contains(err.Error(), "multiple memories") {
			t.Fatalf("snapshot multi-memory error = %v", err)
		}
	})

	t.Run("imported", func(t *testing.T) {
		m0, err := NewMemory(1, 1)
		if err != nil {
			t.Fatalf("NewMemory m0: %v", err)
		}
		defer m0.Close()
		m1, err := NewMemory(2, 2)
		if err != nil {
			t.Fatalf("NewMemory m1: %v", err)
		}
		defer m1.Close()
		compiled := stagedMultiMemoryCompile(t, importedMultiMemoryExecModule())
		in, err := instantiateCore(compiled, InstantiateOptions{Imports: Imports{"env.m0": m0, "env.m1": m1}})
		if err != nil {
			t.Fatalf("instantiate imported memories: %v", err)
		}
		exerciseIndexedMemory1(t, in)
		if err := in.Close(); err != nil {
			t.Fatalf("close imported-memory instance: %v", err)
		}
		if got := binary.LittleEndian.Uint32(m1.Bytes()[32:]); got != 0x12345678 {
			t.Fatalf("imported memory-1 bytes = %#x", got)
		}
	})

	if _, err := Compile(nil, localMultiMemoryExecModule()); err == nil {
		t.Fatal("public multi-memory compile unexpectedly succeeded")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureMultiMemory)
	if err := cfg.Validate(); err == nil {
		t.Fatal("public multi-memory feature bit unexpectedly admitted")
	}
}
