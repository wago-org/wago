//go:build linux && riscv64

package riscv64

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/riscv64spike"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func productionModule1(t *testing.T, params, results []wasm.ValType, body []byte) *wasm.Module {
	t.Helper()
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

type productionFuncDef struct {
	params, results []wasm.ValType
	body            []byte
}

func productionModuleFuncs(t *testing.T, fns ...productionFuncDef) *wasm.Module {
	t.Helper()
	var types, funcs, codes [][]byte
	for i, fn := range fns {
		types = append(types, wasmtest.FuncType(fn.params, fn.results))
		funcs = append(funcs, wasmtest.ULEB(uint32(i)))
		codes = append(codes, append(wasmtest.ULEB(uint32(len(fn.body))), fn.body...))
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func productionMemoryModule(t *testing.T, params, results []wasm.ValType, body []byte) *wasm.Module {
	t.Helper()
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func runProductionInternal2(t *testing.T, m *wasm.Module, a0, a1 uintptr) uintptr {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mem, err := riscv64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	entry := uintptr(unsafe.Pointer(&mem[cm.InternalEntry[0]]))
	return riscv64spike.Call2(entry, a0, a1)
}

func runProductionWrapper(t *testing.T, m *wasm.Module, args ...uint64) (uint64, error) {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	serArgs, results, trap := arena.Alloc(256), arena.Alloc(256), arena.Alloc(8)
	for i, value := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], value)
	}
	err = eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results)
	return binary.LittleEndian.Uint64(results), err
}

func TestProductionWrapperRuntimeExec(t *testing.T) {
	m := productionModule1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b})
	got, err := runProductionWrapper(t, m, 40, 2)
	if err != nil {
		t.Fatal(err)
	}
	if uint32(got) != 42 {
		t.Fatalf("got %d, want 42", got)
	}
}

func TestProductionIntegerControlExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	cases := []struct {
		name         string
		in           []wasm.ValType
		body         []byte
		a0, a1, want uintptr
	}{
		{"add", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}, 40, 2, 42},
		{"sub", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6b, 0x0b}, 100, 58, 42},
		{"mul", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6c, 0x0b}, 6, 7, 42},
		{"lt-s", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x48, 0x0b}, ^uintptr(0), 1, 1},
		{"lt-u", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x49, 0x0b}, ^uintptr(0), 1, 0},
		{"fib", i32, []byte{
			0x01, 0x03, 0x7f, 0x41, 0x00, 0x21, 0x01, 0x41, 0x01, 0x21, 0x02, 0x41, 0x00, 0x21, 0x03,
			0x02, 0x40, 0x03, 0x40, 0x20, 0x03, 0x20, 0x00, 0x4e, 0x0d, 0x01,
			0x20, 0x01, 0x20, 0x02, 0x6a, 0x20, 0x02, 0x21, 0x01, 0x21, 0x02,
			0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03, 0x0c, 0x00, 0x0b, 0x0b, 0x20, 0x01, 0x0b,
		}, 10, 0, 55},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runProductionInternal2(t, productionModule1(t, tc.in, i32, tc.body), tc.a0, tc.a1)
			if uint32(got) != uint32(tc.want) {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}
}

func productionImportedCallModule(t *testing.T) *wasm.Module {
	t.Helper()
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	imp := append(wasmtest.Name("env"), wasmtest.Name("double")...)
	imp = append(imp, 0x00)
	imp = append(imp, wasmtest.ULEB(0)...)
	body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x41, 0x01, 0x6a, 0x0b}
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestProductionSyncHostCallExec(t *testing.T) {
	m := productionImportedCallModule(t)
	cm, err := CompileModuleWith(m, CompileOptions{SyncHostCalls: true})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(8192)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	serArgs, results, trap := arena.Alloc(16), arena.Alloc(16), arena.Alloc(8)
	ctrl := arena.Alloc(coreruntime.HostCtrlFrameBytes)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&ctrl[0])))
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	binary.LittleEndian.PutUint32(serArgs, 20)
	calls := 0
	err = eng.CallWithHost(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results, ctrl, func(index uint32, args, results []uint64) {
		calls++
		if index != 0 || len(args) != 1 || len(results) < 1 {
			t.Fatalf("host call shape %d %d/%d", index, len(args), len(results))
		}
		results[0] = args[0] * 2
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	if got := binary.LittleEndian.Uint32(results); got != 41 {
		t.Fatalf("got %d, want 41", got)
	}
}

func TestProductionSyncHostFloatCallExec(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64})
	imp := append(wasmtest.Name("env"), wasmtest.Name("twice")...)
	imp = append(imp, 0x00)
	imp = append(imp, wasmtest.ULEB(0)...)
	body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x44,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xe0, 0x3f, // f64.const 0.5
		0xa0, 0x0b}
	entryBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(entryBody)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileModuleWith(m, CompileOptions{SyncHostCalls: true})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(8192)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	serArgs, results, trap := arena.Alloc(16), arena.Alloc(16), arena.Alloc(8)
	ctrl := arena.Alloc(coreruntime.HostCtrlFrameBytes)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&ctrl[0])))
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	binary.LittleEndian.PutUint64(serArgs, math.Float64bits(1.25))
	err = eng.CallWithHost(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results, ctrl, func(index uint32, args, results []uint64) {
		if index != 0 || len(args) != 1 || len(results) < 1 {
			t.Fatalf("host call shape %d %d/%d", index, len(args), len(results))
		}
		results[0] = math.Float64bits(math.Float64frombits(args[0]) * 2)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := math.Float64frombits(binary.LittleEndian.Uint64(results)); got != 3 {
		t.Fatalf("got %v, want 3", got)
	}
}

func TestProductionSyncHostF32ResultIsNaNBoxed(t *testing.T) {
	importSig := wasmtest.FuncType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32})
	localSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	imp := append(wasmtest.Name("env"), wasmtest.Name("id")...)
	imp = append(imp, 0x00)
	imp = append(imp, wasmtest.ULEB(0)...)
	body := []byte{0x00, 0x20, 0x00, 0xbe, 0x10, 0x00, 0xbc, 0x0b}
	entryBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(importSig, localSig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(entryBody)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileModuleWith(m, CompileOptions{SyncHostCalls: true})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(8192)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	serArgs, results, trap := arena.Alloc(16), arena.Alloc(16), arena.Alloc(8)
	ctrl := arena.Alloc(coreruntime.HostCtrlFrameBytes)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&ctrl[0])))
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	const bits = uint32(0x80000000) // -0, whose boxing failure becomes canonical NaN.
	binary.LittleEndian.PutUint32(serArgs, bits)
	err = eng.CallWithHost(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results, ctrl, func(_ uint32, args, results []uint64) {
		results[0] = args[0]
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(results); got != bits {
		t.Fatalf("f32 host round trip = %#x, want %#x", got, bits)
	}
}

func TestProductionFloatCompareIfPreservesSelectedNaN(t *testing.T) {
	f32x2 := []wasm.ValType{wasm.F32, wasm.F32}
	f32 := []wasm.ValType{wasm.F32}
	body := []byte{0x00,
		0x20, 0x00, 0x20, 0x01, 0x5d, // f32.lt
		0x04, 0x7d, 0x20, 0x00, // if (result f32): first operand
		0x05, 0x20, 0x01, // else: second operand
		0x0b, 0x0b}
	const nan = uint64(0x7fc00000)
	got, err := runProductionWrapper(t, productionModule1(t, f32x2, f32, body), 0, nan)
	if err != nil {
		t.Fatal(err)
	}
	if uint32(got) != uint32(nan) {
		t.Fatalf("unordered if result = %#x, want %#x", uint32(got), uint32(nan))
	}
}

func TestMachineIndexedLoadPreservesScratchBase(t *testing.T) {
	var a machine
	a.MovReg64(X16, X0)
	a.LslImm(X1, X1, 2, false)
	a.LoadIdx(X17, X16, X1, 0, 4, true, true)
	a.Add64(X0, X16, X17)
	a.Ret()
	code, err := riscv64spike.MapExec(a.B)
	if err != nil {
		t.Fatal(err)
	}
	mem, err := riscv64spike.MapRW(4096)
	if err != nil {
		t.Fatal(err)
	}
	base := uintptr(unsafe.Pointer(&mem[0]))
	binary.LittleEndian.PutUint32(mem[4:], 32)
	entry := uintptr(unsafe.Pointer(&code[0]))
	if got := riscv64spike.Call2(entry, base, 1); got != base+32 {
		t.Fatalf("indexed load result = %#x, want %#x", got, base+32)
	}
}

func TestProductionRecursiveThreeArgI64Exec(t *testing.T) {
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	i64 := []wasm.ValType{wasm.I64}
	run := []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x20, 0x01, 0x10, 0x01, 0x0b}
	g := []byte{0x00, 0x20, 0x00, 0x45, 0x04, 0x7e,
		0x20, 0x01, 0xad, 0x05,
		0x20, 0x01, 0xad,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x20, 0x01, 0x41, 0x11, 0x6a, 0x20, 0x02, 0x10, 0x01,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x20, 0x01, 0x41, 0x1f, 0x6a, 0x20, 0x02, 0x10, 0x01,
		0x7c, 0x7c, 0x0b, 0x0b}
	m := productionModuleFuncs(t,
		productionFuncDef{i32x2, i64, run},
		productionFuncDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, i64, g},
	)
	var want func(depth, seed uint64) uint64
	want = func(depth, seed uint64) uint64 {
		if depth == 0 {
			return seed
		}
		return seed + want(depth-1, seed+17) + want(depth-1, seed+31)
	}
	for depth := uint64(0); depth <= 5; depth++ {
		got, err := runProductionWrapper(t, m, depth, 4)
		if err != nil {
			t.Fatal(err)
		}
		if got != want(depth, 1) {
			t.Fatalf("depth=%d got %d want %d", depth, got, want(depth, 1))
		}
	}
}

func TestProductionTwoCallsPreserveI64BelowOperand(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	i64 := []wasm.ValType{wasm.I64}
	m := productionModuleFuncs(t,
		productionFuncDef{i32, i64, []byte{0x00,
			0x42, 0xe8, 0x07,
			0x20, 0x00, 0x41, 0x11, 0x6a, 0x10, 0x01,
			0x20, 0x00, 0x41, 0x1f, 0x6a, 0x10, 0x01,
			0x7c, 0x7c, 0x0b}},
		productionFuncDef{i32, i64, []byte{0x00, 0x20, 0x00, 0xad, 0x0b}},
	)
	for _, x := range []uintptr{0, 1, 17, 100} {
		want := uint64(1000 + 2*x + 48)
		if got := uint64(runProductionInternal2(t, m, x, 0)); got != want {
			t.Fatalf("x=%d got %d want %d", x, got, want)
		}
	}
}

func TestProductionDirectCallExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := productionModuleFuncs(t,
		productionFuncDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x41, 0x01, 0x6a, 0x0b}},
		productionFuncDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x00, 0x6a, 0x0b}},
	)
	for _, x := range []uintptr{0, 1, 5, 100} {
		if got := runProductionInternal2(t, m, x, 0); uint32(got) != uint32(2*x+1) {
			t.Fatalf("f(%d)=%d, want %d", x, got, 2*x+1)
		}
	}
}

func TestProductionMixedAndMultiResultCallsExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	t.Run("mixed-float-int", func(t *testing.T) {
		m := productionModuleFuncs(t,
			productionFuncDef{i32, i32, []byte{0x00, 0x20, 0x00, 0xb7, 0x20, 0x00, 0x10, 0x01, 0xaa, 0x0b}},
			productionFuncDef{[]wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xb7, 0xa0, 0x0b}},
		)
		for _, x := range []uint64{0, 1, 7, 100} {
			got, err := runProductionWrapper(t, m, x)
			if err != nil {
				t.Fatal(err)
			}
			if uint32(got) != uint32(2*x) {
				t.Fatalf("f(%d)=%d, want %d", x, got, 2*x)
			}
		}
	})
	t.Run("two-integer-results", func(t *testing.T) {
		m := productionModuleFuncs(t,
			productionFuncDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x6a, 0x0b}},
			productionFuncDef{i32, []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
		)
		for _, x := range []uint64{0, 1, 7, 100} {
			got, err := runProductionWrapper(t, m, x)
			if err != nil {
				t.Fatal(err)
			}
			if uint32(got) != uint32(2*x+1) {
				t.Fatalf("f(%d)=%d, want %d", x, got, 2*x+1)
			}
		}
	})
}

func TestProductionFloatPipelineExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{0x00, 0x20, 0x00, 0xb2, 0x43, 0x00, 0x00, 0x00, 0x40, 0x92, 0xa8, 0x0b}
	m := productionModule1(t, i32, i32, body)
	for _, x := range []uintptr{0, 5, 40} {
		if got := runProductionInternal2(t, m, x, 0); uint32(got) != uint32(x+2) {
			t.Fatalf("f(%d)=%d, want %d", x, got, x+2)
		}
	}
}

func TestProductionMemoryExplicitExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{0x00, 0x20, 0x00, 0x41, 0xb9, 0xe0, 0x00, 0x36, 0x02, 0x04, 0x20, 0x00, 0x28, 0x02, 0x04, 0x0b}
	m := productionMemoryModule(t, i32, i32, body)
	got, err := runProductionWrapper(t, m, 16)
	if err != nil {
		t.Fatal(err)
	}
	if uint32(got) != 12345 {
		t.Fatalf("got %d", got)
	}
}

func TestProductionMemoryLargeOffsetExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{0x00,
		0x20, 0x00, 0x41, 0xb9, 0xe0, 0x00, 0x36, 0x02, 0x80, 0x80, 0x02,
		0x20, 0x00, 0x28, 0x02, 0x80, 0x80, 0x02, 0x0b}
	m := productionMemoryModule(t, i32, i32, body)
	cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true})
	if err != nil {
		t.Fatal(err)
	}
	code, err := riscv64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	const head = 4096
	mem, err := riscv64spike.MapRW(head + 65536)
	if err != nil {
		t.Fatal(err)
	}
	lin := uintptr(unsafe.Pointer(&mem[head]))
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
	if got := riscv64spike.Call3(entry, 16, 0, lin); uint32(got) != 12345 {
		t.Fatalf("got %d", got)
	}
	if got := binary.LittleEndian.Uint32(mem[head+16+32768:]); got != 12345 {
		t.Fatalf("memory=%d", got)
	}
}

func TestProductionBulkMemoryExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{0x00, 0x41}
	body = append(body, wasmtest.SLEB32(100)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(0x5a)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(137)...)
	body = append(body, 0xfc, 0x0b, 0x00) // memory.fill
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(200)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(100)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(137)...)
	body = append(body, 0xfc, 0x0a, 0x00, 0x00) // memory.copy
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(336)...)
	body = append(body, 0x2d, 0x00, 0x00, 0x0b) // i32.load8_u
	m := productionMemoryModule(t, nil, i32, body)
	got, err := runProductionWrapper(t, m)
	if err != nil {
		t.Fatal(err)
	}
	if uint32(got) != 0x5a {
		t.Fatalf("got %#x, want 0x5a", got)
	}
}

func TestProductionUnaryBitsExec(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	for _, tc := range []struct {
		name     string
		op       byte
		in, want uint64
	}{
		{"clz-zero", 0x79, 0, 64}, {"clz", 0x79, 1, 63},
		{"ctz-zero", 0x7a, 0, 64}, {"ctz", 0x7a, 0x1000, 12},
		{"popcnt", 0x7b, 0xf0f000000000000f, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := productionModule1(t, i64, i64, []byte{0x00, 0x20, 0x00, tc.op, 0x0b})
			if got := uint64(runProductionInternal2(t, m, uintptr(tc.in), 0)); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestProductionFloatRoundingExec(t *testing.T) {
	f64 := []wasm.ValType{wasm.F64}
	for _, tc := range []struct {
		name     string
		op       byte
		in, want float64
	}{
		{"ceil-pos", 0x9b, 0.3, 1}, {"ceil-neg", 0x9b, -0.3, math.Copysign(0, -1)},
		{"floor-pos", 0x9c, 0.3, 0}, {"floor-neg", 0x9c, -0.3, -1},
		{"trunc-neg", 0x9d, -1.9, -1}, {"nearest-even-low", 0x9e, 2.5, 2}, {"nearest-even-high", 0x9e, 3.5, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := productionModule1(t, f64, f64, []byte{0x00, 0x20, 0x00, tc.op, 0x0b})
			got, err := runProductionWrapper(t, m, math.Float64bits(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if got != math.Float64bits(tc.want) {
				t.Fatalf("got %v (%#x), want %v (%#x)", math.Float64frombits(got), got, tc.want, math.Float64bits(tc.want))
			}
		})
	}
}

func TestProductionTrapsExec(t *testing.T) {
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	i32 := []wasm.ValType{wasm.I32}
	cases := []struct {
		name string
		m    *wasm.Module
		args []uint64
		code coreruntime.TrapCode
	}{
		{"div-zero", productionModule1(t, i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6d, 0x0b}), []uint64{1, 0}, coreruntime.TrapDivZero},
		{"div-overflow", productionModule1(t, i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6d, 0x0b}), []uint64{0x80000000, 0xffffffff}, coreruntime.TrapDivOverflow},
		{"unreachable", productionModule1(t, nil, nil, []byte{0x00, 0x00, 0x0b}), nil, coreruntime.TrapUnreachable},
		{"memory-oob", productionMemoryModule(t, i32, i32, []byte{0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}), []uint64{65536}, coreruntime.TrapLinMemOutOfBounds},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runProductionWrapper(t, tc.m, tc.args...)
			var trap *coreruntime.TrapError
			if !errors.As(err, &trap) || trap.Code != tc.code {
				t.Fatalf("error=%v, want trap %v", err, tc.code)
			}
		})
	}
}

func TestProductionI32CanonicalizationExec(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	body := []byte{0x00, 0x41}
	body = append(body, wasmtest.SLEB32(0x7fffffff)...)
	body = append(body, 0x41, 0x01, 0x6a, 0xad, 0x0b) // add wraps to 0x80000000; extend_i32_u
	got, err := runProductionWrapper(t, productionModule1(t, nil, i64, body))
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x80000000 {
		t.Fatalf("zero-extended wrapped i32 = %#x, want %#x", got, uint64(0x80000000))
	}
}

func TestProductionF64DeclaredLocalStartsAtPositiveZero(t *testing.T) {
	f64 := []wasm.ValType{wasm.F64}
	body := []byte{0x01, 0x01, 0x7c, 0x20, 0x00, 0x0b}
	got, err := runProductionWrapper(t, productionModule1(t, nil, f64, body))
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("f64 local bits = %#x, want +0", got)
	}
}

func TestProductionCallFreePinPoolHasUniquePhysicalRegisters(t *testing.T) {
	seen := make(map[Reg]bool)
	for _, reg := range gpPinPool(false, 0, true) {
		if seen[reg] {
			t.Fatalf("pin pool aliases physical register %v: %v", reg, gpPinPool(false, 0, true))
		}
		seen[reg] = true
	}
}

func TestProductionI64SignExtendExec(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	for _, tc := range []struct {
		name     string
		op       byte
		in, want uint64
	}{
		{"extend8", 0xc2, 0xff, ^uint64(0)},
		{"extend16", 0xc3, 0x8000, 0xffffffffffff8000},
		{"extend32", 0xc4, 0x80000000, 0xffffffff80000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := uint64(runProductionInternal2(t, productionModule1(t, i64, i64, []byte{0x00, 0x20, 0x00, tc.op, 0x0b}), uintptr(tc.in), 0))
			if got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}
}
