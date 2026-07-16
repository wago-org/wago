//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"math"
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

func stagedMultiMemoryCompile(t testing.TB, module []byte) *Compiled {
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

func indexedScalarMemoryModule(param, result wasm.ValType, storeOp, loadOp, align byte) []byte {
	body := []byte{
		0x20, 0x00, 0x20, 0x01, storeOp, align | 0x40, 0x01, 0x00,
		0x20, 0x00, loadOp, align | 0x40, 0x01, 0x00, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, param}, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x01},
			[]byte{0x01, 0x01, 0x02},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 0),
			wasmtest.ExportEntry("m1", 2, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func indexedGrowModule(imported bool) []byte {
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		)),
	}
	if imported {
		memoryImport := func(name string, min, max byte) []byte {
			entry := append(wasmtest.Name("env"), wasmtest.Name(name)...)
			return append(entry, 0x02, 0x01, min, max)
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(memoryImport("m0", 1, 1), memoryImport("m1", 1, 3))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x01},
			[]byte{0x01, 0x01, 0x03},
		)))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow1", 0, 0),
			wasmtest.ExportEntry("size1", 0, 1),
			wasmtest.ExportEntry("store1", 0, 2),
			wasmtest.ExportEntry("m1", 2, 1),
			wasmtest.ExportEntry("m1alias", 2, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x36, 0x42, 0x01, 0x00, 0x0b}),
		)),
	)
	return wasmtest.Module(sections...)
}

func TestStagedMultiMemoryScalarWidthsAndGrow(t *testing.T) {
	tests := []struct {
		name            string
		param, result   wasm.ValType
		store, load     byte
		align           byte
		input, wantBits uint64
		wantSigned32    int32
		wantSigned64    int64
		compareSigned32 bool
		compareSigned64 bool
	}{
		{name: "i32", param: wasm.I32, result: wasm.I32, store: 0x36, load: 0x28, align: 2, input: 0x89abcdef, wantBits: 0x89abcdef},
		{name: "i64", param: wasm.I64, result: wasm.I64, store: 0x37, load: 0x29, align: 3, input: 0x0123456789abcdef, wantBits: 0x0123456789abcdef},
		{name: "f32", param: wasm.F32, result: wasm.F32, store: 0x38, load: 0x2a, align: 2, input: uint64(math.Float32bits(-13.25)), wantBits: uint64(math.Float32bits(-13.25))},
		{name: "f64", param: wasm.F64, result: wasm.F64, store: 0x39, load: 0x2b, align: 3, input: math.Float64bits(123.5), wantBits: math.Float64bits(123.5)},
		{name: "i32.load8_s", param: wasm.I32, result: wasm.I32, store: 0x3a, load: 0x2c, align: 0, input: 0x80, wantSigned32: -128, compareSigned32: true},
		{name: "i32.load8_u", param: wasm.I32, result: wasm.I32, store: 0x3a, load: 0x2d, align: 0, input: 0x80, wantBits: 0x80},
		{name: "i32.load16_s", param: wasm.I32, result: wasm.I32, store: 0x3b, load: 0x2e, align: 1, input: 0x8001, wantSigned32: -32767, compareSigned32: true},
		{name: "i32.load16_u", param: wasm.I32, result: wasm.I32, store: 0x3b, load: 0x2f, align: 1, input: 0x8001, wantBits: 0x8001},
		{name: "i64.load8_s", param: wasm.I64, result: wasm.I64, store: 0x3c, load: 0x30, align: 0, input: 0x81, wantSigned64: -127, compareSigned64: true},
		{name: "i64.load8_u", param: wasm.I64, result: wasm.I64, store: 0x3c, load: 0x31, align: 0, input: 0x81, wantBits: 0x81},
		{name: "i64.load16_s", param: wasm.I64, result: wasm.I64, store: 0x3d, load: 0x32, align: 1, input: 0x8002, wantSigned64: -32766, compareSigned64: true},
		{name: "i64.load16_u", param: wasm.I64, result: wasm.I64, store: 0x3d, load: 0x33, align: 1, input: 0x8002, wantBits: 0x8002},
		{name: "i64.load32_s", param: wasm.I64, result: wasm.I64, store: 0x3e, load: 0x34, align: 2, input: 0x80000003, wantSigned64: -2147483645, compareSigned64: true},
		{name: "i64.load32_u", param: wasm.I64, result: wasm.I64, store: 0x3e, load: 0x35, align: 2, input: 0x80000003, wantBits: 0x80000003},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiled := stagedMultiMemoryCompile(t, indexedScalarMemoryModule(tc.param, tc.result, tc.store, tc.load, tc.align))
			in, err := instantiateCore(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			defer in.Close()
			got, err := in.Invoke("run", I32(17), tc.input)
			if err != nil {
				t.Fatalf("invoke: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("result count = %d, want 1", len(got))
			}
			switch {
			case tc.compareSigned32:
				if AsI32(got[0]) != tc.wantSigned32 {
					t.Fatalf("result = %d, want %d", AsI32(got[0]), tc.wantSigned32)
				}
			case tc.compareSigned64:
				if AsI64(got[0]) != tc.wantSigned64 {
					t.Fatalf("result = %d, want %d", AsI64(got[0]), tc.wantSigned64)
				}
			case got[0] != tc.wantBits:
				t.Fatalf("result bits = %#x, want %#x", got[0], tc.wantBits)
			}
		})
	}

	exerciseGrow := func(t *testing.T, in *Instance) {
		t.Helper()
		m1, err := in.ExportedMemory("m1")
		if err != nil {
			t.Fatalf("export m1: %v", err)
		}
		alias, err := in.ExportedMemory("m1alias")
		if err != nil {
			t.Fatalf("export m1alias: %v", err)
		}
		if got := tableTestCallI32(t, in, "size1"); got != 1 {
			t.Fatalf("initial memory.size 1 = %d, want 1", got)
		}
		if got := tableTestCallI32(t, in, "grow1", I32(1)); got != 1 {
			t.Fatalf("memory.grow 1 = %d, want old size 1", got)
		}
		if got := tableTestCallI32(t, in, "size1"); got != 2 {
			t.Fatalf("grown memory.size 1 = %d, want 2", got)
		}
		if len(m1.Bytes()) != 2*65536 || len(alias.Bytes()) != 2*65536 {
			t.Fatalf("exported alias lengths = %d,%d, want %d", len(m1.Bytes()), len(alias.Bytes()), 2*65536)
		}
		if m1.Bytes()[65536] != 0 {
			t.Fatalf("grown page first byte = %d, want zero", m1.Bytes()[65536])
		}
		if _, err := in.Invoke("store1", I32(65536), I32(0x55aa)); err != nil {
			t.Fatalf("store in grown page: %v", err)
		}
		for _, delta := range []uint64{I32(2), I32(-1)} {
			if got := tableTestCallI32(t, in, "grow1", delta); got != -1 {
				t.Fatalf("failing memory.grow(%#x) = %d, want -1", delta, got)
			}
			if got := tableTestCallI32(t, in, "size1"); got != 2 {
				t.Fatalf("size after failed grow = %d, want 2", got)
			}
			if len(m1.Bytes()) != 2*65536 || binary.LittleEndian.Uint32(m1.Bytes()[65536:]) != 0x55aa {
				t.Fatalf("failed grow changed exported memory state")
			}
		}
	}

	t.Run("grow local", func(t *testing.T) {
		compiled := stagedMultiMemoryCompile(t, indexedGrowModule(false))
		in, err := instantiateCore(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		defer in.Close()
		exerciseGrow(t, in)
	})
	t.Run("grow imported re-export", func(t *testing.T) {
		m0, err := NewMemory(1, 1)
		if err != nil {
			t.Fatal(err)
		}
		defer m0.Close()
		m1, err := NewMemory(1, 3)
		if err != nil {
			t.Fatal(err)
		}
		defer m1.Close()
		compiled := stagedMultiMemoryCompile(t, indexedGrowModule(true))
		in, err := instantiateCore(compiled, InstantiateOptions{Imports: Imports{"env.m0": m0, "env.m1": m1}})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		exerciseGrow(t, in)
		if err := in.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if len(m1.Bytes()) != 2*65536 {
			t.Fatalf("host import length after close = %d, want %d", len(m1.Bytes()), 2*65536)
		}
	})

	t.Run("memory zero code unchanged", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}))),
		)
		t.Setenv("WAGO_BOUNDS", "explicit")
		cfg := NewRuntimeConfig()
		baseFeatures := cfg.frontendFeatures()
		base, err := compileWithFrontendFeatures(cfg, module, baseFeatures)
		if err != nil {
			t.Fatalf("compile baseline: %v", err)
		}
		defer base.Close()
		stagedFeatures := baseFeatures
		stagedFeatures.MultiMemory = true
		staged, err := compileWithFrontendFeatures(cfg, module, stagedFeatures)
		if err != nil {
			t.Fatalf("compile staged: %v", err)
		}
		defer staged.Close()
		if string(base.Code) != string(staged.Code) {
			t.Fatal("enabling staged multi-memory changed ordinary memory-0 code bytes")
		}
	})
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
		if err := validateSnapshotModule(compiled); err != nil {
			t.Fatalf("owned local multi-memory snapshot admission = %v", err)
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
	if err := cfg.Validate(); err != nil {
		t.Fatalf("public multi-memory feature validation = %v", err)
	}
	public, err := Compile(cfg, localMultiMemoryExecModule())
	if err != nil {
		t.Fatalf("public opt-in multi-memory compile = %v", err)
	}
	_ = public.Close()
}
