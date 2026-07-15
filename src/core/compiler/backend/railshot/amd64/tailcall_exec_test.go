//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

func runTailRaw(t *testing.T, m *wasm.Module, args ...uint64) ([]byte, error) {
	t.Helper()
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(code)
	argBuf := arena.Alloc(128)
	resultBuf := arena.Alloc(128)
	trap := arena.Alloc(8)
	for i, arg := range args {
		binary.LittleEndian.PutUint64(argBuf[i*8:], arg)
	}
	err = eng.Call(entry+uintptr(cm.Entry[0]), argBuf, jm.LinearMemory(), trap, resultBuf)
	return append([]byte(nil), resultBuf[:32]...), err
}

func TestReturnCallDirectReusesFrameForDeepRecursion(t *testing.T) {
	// (func (param i32) (result i32)
	//   local.get 0; i32.eqz
	//   if (result i32) i32.const 7
	//   else local.get 0; i32.const 1; i32.sub; return_call 0 end)
	m := modFuncs(t, funcDef{
		params:  []wasm.ValType{wasm.I32},
		results: []wasm.ValType{wasm.I32},
		body:    []byte{0x00, 0x20, 0x00, 0x45, 0x04, 0x7f, 0x41, 0x07, 0x05, 0x20, 0x00, 0x41, 0x01, 0x6b, 0x12, 0x00, 0x0b, 0x0b},
	})
	out, err := runTailRaw(t, m, 1_000_000)
	if err != nil {
		t.Fatalf("million-deep tail recursion trapped: %v", err)
	}
	if got := binary.LittleEndian.Uint32(out); got != 7 {
		t.Fatalf("result = %d, want 7", got)
	}

	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if len(stats.Funcs) != 1 || stats.Funcs[0].Calls["tail-direct"] != 1 {
		t.Fatalf("tail-call stats = %+v", stats.Funcs)
	}
	if stats.Funcs[0].FrameBytes == 0 {
		t.Fatal("test requires a real frame so the tail jump proves frame teardown")
	}
}

func TestReturnCallDirectPreservesResultsAndTraps(t *testing.T) {
	t.Run("two integer results", func(t *testing.T) {
		m := modFuncs(t,
			funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32, wasm.I64}, body: []byte{0x00, 0x20, 0x00, 0x12, 0x01, 0x0b}},
			funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32, wasm.I64}, body: []byte{0x00, 0x20, 0x00, 0x42, 0x09, 0x0b}},
		)
		out, err := runTailRaw(t, m, 42)
		if err != nil {
			t.Fatal(err)
		}
		if got0, got1 := binary.LittleEndian.Uint32(out), binary.LittleEndian.Uint64(out[8:]); got0 != 42 || got1 != 9 {
			t.Fatalf("results = (%d,%d), want (42,9)", got0, got1)
		}
	})

	t.Run("float register bank", func(t *testing.T) {
		m := modFuncs(t,
			funcDef{params: []wasm.ValType{wasm.F64}, results: []wasm.ValType{wasm.F64}, body: []byte{0x00, 0x20, 0x00, 0x12, 0x01, 0x0b}},
			funcDef{params: []wasm.ValType{wasm.F64}, results: []wasm.ValType{wasm.F64}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
		)
		want := math.Float64bits(3.5)
		out, err := runTailRaw(t, m, want)
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint64(out); got != want {
			t.Fatalf("f64 bits = %#x, want %#x", got, want)
		}
	})

	t.Run("callee trap", func(t *testing.T) {
		m := modFuncs(t,
			funcDef{body: []byte{0x00, 0x12, 0x01, 0x0b}},
			funcDef{body: []byte{0x00, 0x00, 0x0b}},
		)
		_, err := runTailRaw(t, m)
		if err == nil || !strings.Contains(err.Error(), "unreachable") {
			t.Fatalf("trap = %v, want unreachable", err)
		}
	})
}

func TestReturnCallDirectWrapperReusesBoundedTailBank(t *testing.T) {
	m := modFuncs(t, funcDef{
		params:  []wasm.ValType{wasm.I32, wasm.FuncRef},
		results: []wasm.ValType{wasm.I32},
		body: []byte{
			0x00,
			0x20, 0x00, 0x45,
			0x04, 0x7f,
			0x41, 0x07,
			0x05,
			0x20, 0x00, 0x41, 0x01, 0x6b,
			0x20, 0x01,
			0x12, 0x00,
			0x0b, 0x0b,
		},
	})
	out, err := runTailRaw(t, m, 1_000_000, 0)
	if err != nil {
		t.Fatalf("million-deep wrapper tail recursion trapped: %v", err)
	}
	if got := binary.LittleEndian.Uint32(out); got != 7 {
		t.Fatalf("result = %d, want 7", got)
	}
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if len(stats.Funcs) != 1 || stats.Funcs[0].Calls["tail-direct-wrapper"] != 1 {
		t.Fatalf("wrapper tail-call stats = %+v", stats.Funcs)
	}
	if stats.Funcs[0].FrameBytes == 0 {
		t.Fatal("test requires a real wrapper frame so teardown is measured")
	}
}

func TestReturnCallDirectRejectsOversizedWrapperTailBank(t *testing.T) {
	params := make([]wasm.ValType, 17)
	for i := range params {
		params[i] = wasm.I32
	}
	body := []byte{0x00}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x12, 0x01, 0x0b)
	target := []byte{0x00, 0x0b}
	m := modFuncs(t,
		funcDef{params: params, body: body},
		funcDef{params: params, body: target},
	)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileModule(m); err == nil || !strings.Contains(err.Error(), "requires 17 wrapper argument slots, limit 16") {
		t.Fatalf("compile error = %v", err)
	}
}
