//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func f32b(v float32) uint64 { return uint64(math.Float32bits(v)) }
func f64b(v float64) uint64 { return math.Float64bits(v) }

func TestDirectBackendUsesSharedCodegenOptions(t *testing.T) {
	var _ codegen.Backend[*wasm.Module] = DirectBackend{}

	m := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{{
			Final: true,
			Comp:  wasm.CompType{Kind: wasm.CompFunc},
		}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Code:      []wasm.Func{{BodyBytes: []byte{0x0b}}},
	}
	obj, err := (DirectBackend{}).CompileModule(m, codegen.Options{
		Runtime: codegen.RuntimeFuncs{},
		Heap:    codegen.NoopHeap{},
	})
	if err != nil {
		t.Fatalf("CompileModule: %v", err)
	}
	if len(obj.Entry) != 1 {
		t.Fatalf("Entry len = %d, want 1", len(obj.Entry))
	}
	if len(obj.Code) == 0 {
		t.Fatal("compiled code is empty")
	}
}

// mod1 builds and decodes a one-function module exporting "f". funcBody is the
// full code entry (local declarations + instruction stream).
func mod1(t *testing.T, params, results []wasm.ValType, funcBody []byte) *wasm.Module {
	t.Helper()
	entry := append(wasmtest.ULEB(uint32(len(funcBody))), funcBody...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// funcDef describes one function in a multi-function test module.
type funcDef struct {
	params, results []wasm.ValType
	body            []byte // local decls + instruction stream (incl. trailing 0x0b)
}

// modFuncs builds a module of several local functions (func 0 exported as "f",
// each function using its own type index), for exercising internal calls.
func modFuncs(t *testing.T, fns ...funcDef) *wasm.Module {
	t.Helper()
	var types, funcs, codes [][]byte
	for i, fn := range fns {
		types = append(types, wasmtest.FuncType(fn.params, fn.results))
		funcs = append(funcs, wasmtest.ULEB(uint32(i)))
		codes = append(codes, append(wasmtest.ULEB(uint32(len(fn.body))), fn.body...))
	}
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// runAmd64 compiles function 0 with the new amd64 backend and runs it through the real
// wago runtime with the given i32 args, returning the first i32 result.
func runAmd64(t *testing.T, m *wasm.Module, args ...int32) int32 {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("amd64 compile: %v", err)
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
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer runtime.Unmap(mem)

	serArgs := ar.Alloc(128)
	results := ar.Alloc(128)
	trap := ar.Alloc(8)
	for i, a := range args {
		binary.LittleEndian.PutUint32(serArgs[i*8:], uint32(a))
	}
	if err := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return int32(binary.LittleEndian.Uint32(results))
}

// modMem builds a one-function module that also declares a linear memory of
// `pages` (so memory opcodes validate/decode).
func modMem(t *testing.T, pages uint32, params, results []wasm.ValType, funcBody []byte) *wasm.Module {
	t.Helper()
	entry := append(wasmtest.ULEB(uint32(len(funcBody))), funcBody...)
	memType := append([]byte{0x00}, wasmtest.ULEB(pages)...) // flags=0 (min only)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memType)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// runMemAmd64 compiles function 0, sets up linear memory via setup, runs it, and
// returns the raw 64-bit result word, a copy of post-run linear memory, and any
// trap error from the call.
func runMemAmd64(t *testing.T, m *wasm.Module, setup func([]byte), args ...uint64) (uint64, []byte, error) {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("amd64 compile: %v", err)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(1 << 16)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	lin := jm.LinearMemory()
	if setup != nil {
		setup(lin)
	}
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(256)
	results := ar.Alloc(256)
	trap := ar.Alloc(8)
	for i, a := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], a)
	}
	callErr := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, lin, trap, results)
	return binary.LittleEndian.Uint64(results), append([]byte(nil), lin...), callErr
}

// runAmd64u compiles function 0 and runs it with the given 8-byte-wide args
// (i32/i64), returning the raw 64-bit result word.
func runAmd64u(t *testing.T, m *wasm.Module, args ...uint64) uint64 {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("amd64 compile: %v", err)
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
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer runtime.Unmap(mem)

	serArgs := ar.Alloc(256)
	results := ar.Alloc(256)
	trap := ar.Alloc(8)
	for i, a := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], a)
	}
	if err := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return binary.LittleEndian.Uint64(results)
}

var (
	i32 = wasm.I32
	i64 = wasm.I64
)

// u64 reinterprets a signed value as its 64-bit two's-complement word (avoids
// constant-overflow errors when writing negative test operands).
func u64(v int64) uint64 { return uint64(v) }

// TestAmd64Phase0 proves the new backend end-to-end: it compiles integer const /
// local / ALU expressions and runs them through the real runtime, exercising the
// deferred-tree condense engine and the on-the-fly register allocator.
func TestAmd64Phase0(t *testing.T) {
	cases := []struct {
		name  string
		decls []byte
		body  []byte
		args  []int32
		want  int32
	}{
		// f(a,b) = a + b
		{"add-params", []byte{0x00}, []byte{
			0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b,
		}, []int32{3, 4}, 7},
		// f(a) = a + 5   (constant folds to immediate in applyALU)
		{"add-const", []byte{0x00}, []byte{
			0x20, 0x00, 0x41, 0x05, 0x6a, 0x0b,
		}, []int32{10}, 15},
		// f(a,b,c) = (a + b) + c   (nested deferred tree, in-place condense)
		{"add-nested", []byte{0x00}, []byte{
			0x20, 0x00, 0x20, 0x01, 0x6a, 0x20, 0x02, 0x6a, 0x0b,
		}, []int32{1, 2, 3}, 6},
		// f(a,b) = (a - b) & 0xff | (a ^ b)   exercises sub/and/or/xor + folding
		{"mixed-ops", []byte{0x00}, []byte{
			0x20, 0x00, 0x20, 0x01, 0x6b, // a - b
			0x41, 0xff, 0x01, 0x71, // & 0xff
			0x20, 0x00, 0x20, 0x01, 0x73, // a ^ b
			0x72, // |
			0x0b,
		}, []int32{0x123, 0x45}, ((0x123 - 0x45) & 0xff) | (0x123 ^ 0x45)},
		// f(x) = local set/get: t = x + x; return t + 1
		{"local-set", []byte{0x01, 0x01, 0x7f}, []byte{
			0x20, 0x00, 0x20, 0x00, 0x6a, 0x21, 0x01, // local 1 = x + x
			0x20, 0x01, 0x41, 0x01, 0x6a, 0x0b, // local1 + 1
		}, []int32{9}, 19},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			params := make([]wasm.ValType, len(c.args))
			for i := range params {
				params[i] = i32
			}
			m := mod1(t, params, []wasm.ValType{i32}, append(append([]byte{}, c.decls...), c.body...))
			if got := runAmd64(t, m, c.args...); got != c.want {
				t.Fatalf("%s = %d, want %d", c.name, got, c.want)
			}
		})
	}
}

// TestAmd64HostImportCompile checks that a call to an imported (host) function
// lowers to the log-and-replay sequence without error (end-to-end replay is
// driven by src/wago instantiation).
func TestAmd64HostImportCompile(t *testing.T) {
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("log")...), 0x00, 0x00) // func, type 0
	body := []byte{0x00, 0x41, 0x05, 0x10, 0x00, 0x0b}                               // i32.const 5; call 0 (import)
	fnBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{i32}, nil), // type 0: import sig
			wasmtest.FuncType(nil, nil),                 // type 1: local func
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))), // func index 1 (import is 0)
		wasmtest.Section(10, wasmtest.Vec(fnBody)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err := CompileModule(m); err != nil {
		t.Fatalf("host import compile: %v", err)
	}
}

// runHostSync compiles m (func index 1 is the export, func 0 the import) and runs
// it via the synchronous host-call protocol with the given i32 args, returning
// the first i32 result and the host-call count.
func runHostSync(t *testing.T, m *wasm.Module, host runtime.HostCall, args ...int32) uint32 {
	t.Helper()
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
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer runtime.Unmap(mem)

	serArgs := ar.Alloc(128)
	results := ar.Alloc(128)
	trap := ar.Alloc(8)
	ctrl := ar.Alloc(runtime.HostCtrlFrameBytes)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&ctrl[0]))) // install the control frame as import ctx
	for i, a := range args {
		binary.LittleEndian.PutUint32(serArgs[i*8:], uint32(a))
	}
	if err := eng.CallWithHost(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results, ctrl, host); err != nil {
		t.Fatalf("CallWithHost: %v", err)
	}
	return binary.LittleEndian.Uint32(results)
}

// hostSyncModule builds a module whose func 0 is an import of type `sig` and func
// 1 (exported "g") has body `body`.
func hostSyncModule(sig []byte, body []byte) *wasm.Module {
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0x00, 0x00) // func, type 0
	fnBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(fnBody)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		panic(err)
	}
	return m
}

// TestAmd64HostImportSyncResult runs a RETURNING host import end-to-end through
// the synchronous re-entry protocol (callHostSync): g(x) calls env.f(x) and
// returns its result; the Go host doubles x. This exercises the full path —
// codegen marshals the arg into the control frame, calls the shared stub, and
// reads the result back.
func TestAmd64HostImportSyncResult(t *testing.T) {
	// type 0: (i32)->(i32). g(x): local.get 0; call 0; end.
	sig := wasmtest.FuncType([]wasm.ValType{i32}, []wasm.ValType{i32})
	m := hostSyncModule(sig, []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b})
	calls := 0
	host := func(imp uint32, args, res []uint64) {
		calls++
		if imp != 0 {
			t.Errorf("host importIdx = %d, want 0", imp)
		}
		res[0] = args[0] * 2
	}
	if got := runHostSync(t, m, host, 21); got != 42 {
		t.Fatalf("result = %d, want 42 (double(21))", got)
	}
	if calls != 1 {
		t.Fatalf("host invoked %d times, want 1", calls)
	}
}

// TestAmd64HostImportSyncMultiParam exercises multi-param marshaling AND a local
// surviving the host round trip: k(a,b) = env.add(a,b) + a. The host adds; the
// caller then re-reads local 0 (a) after the call and adds it.
func TestAmd64HostImportSyncMultiParam(t *testing.T) {
	// type 0: (i32,i32)->(i32).
	sig := wasmtest.FuncType([]wasm.ValType{i32, i32}, []wasm.ValType{i32})
	// k(a,b): local.get 0; local.get 1; call 0; local.get 0; i32.add; end
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x20, 0x00, 0x6a, 0x0b}
	m := hostSyncModule(sig, body)
	host := func(imp uint32, args, res []uint64) { res[0] = args[0] + args[1] }
	if got := runHostSync(t, m, host, 20, 3); got != 43 { // env.add(20,3)=23, +a(20)=43
		t.Fatalf("result = %d, want 43 (add(20,3)+20)", got)
	}
}

// TestAmd64GlobalsCompile checks global.get/set lower without error (end-to-end
// global access is verified at src/wago integration, which populates the runtime
// globals slot-array).
func TestAmd64GlobalsCompile(t *testing.T) {
	// f() = (global.set 0 (i32.const 42)); global.get 0
	body := []byte{0x00,
		0x41, 0x2a, 0x24, 0x00, // i32.const 42; global.set 0
		0x23, 0x00, // global.get 0
		0x0b}
	fnBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	glob := []byte{0x7f, 0x01, 0x41, 0x00, 0x0b} // i32 mutable, init 0
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{i32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(glob)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(fnBody)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err := CompileModule(m); err != nil {
		t.Fatalf("globals compile: %v", err)
	}
}

// TestAmd64Phase5Floats exercises the f32/f64 ISA end-to-end: arithmetic, sqrt,
// neg, NaN/signed-zero-correct min/max, comparisons, conversions, promote/demote,
// reinterpret, and the trapping float→int truncation.
func TestAmd64Phase5Floats(t *testing.T) {
	f64 := wasm.F64
	f32 := wasm.F32

	// f64.add
	t.Run("f64.add", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64, f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0xa0, 0x0b})
		got := math.Float64frombits(runAmd64u(t, m, f64b(1.5), f64b(2.25)))
		if got != 3.75 {
			t.Fatalf("f64.add = %v, want 3.75", got)
		}
	})

	// f32.mul
	t.Run("f32.mul", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f32, f32}, []wasm.ValType{f32}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x94, 0x0b})
		got := math.Float32frombits(uint32(runAmd64u(t, m, f32b(3), f32b(4))))
		if got != 12 {
			t.Fatalf("f32.mul = %v, want 12", got)
		}
	})

	// f64.sqrt
	t.Run("f64.sqrt", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x9f, 0x0b})
		got := math.Float64frombits(runAmd64u(t, m, f64b(16)))
		if got != 4 {
			t.Fatalf("f64.sqrt = %v, want 4", got)
		}
	})

	// f64.neg
	t.Run("f64.neg", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x9a, 0x0b})
		got := math.Float64frombits(runAmd64u(t, m, f64b(3)))
		if got != -3 {
			t.Fatalf("f64.neg = %v, want -3", got)
		}
	})

	// f64/f32 min/max with NaN → NaN; signed zeros obey wasm min(+0,-0)
	// = -0 and max(-0,+0) = +0.
	t.Run("f64.min-nan", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64, f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0xa4, 0x0b})
		bits := runAmd64u(t, m, f64b(1), 0x7ff8000000000001)
		if !math.IsNaN(math.Float64frombits(bits)) {
			t.Fatalf("f64.min(1,NaN) bits = %#x, want NaN", bits)
		}
	})
	t.Run("f32.max-nan", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f32, f32}, []wasm.ValType{f32}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x97, 0x0b})
		bits := uint32(runAmd64u(t, m, f32b(1), 0x7fc00001))
		if !math.IsNaN(float64(math.Float32frombits(bits))) {
			t.Fatalf("f32.max(1,NaN) bits = %#x, want NaN", bits)
		}
	})
	t.Run("f64.max-nan", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64, f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0xa5, 0x0b})
		bits := runAmd64u(t, m, 0x7ff8000000000001, f64b(1))
		if !math.IsNaN(math.Float64frombits(bits)) {
			t.Fatalf("f64.max(NaN,1) bits = %#x, want NaN", bits)
		}
	})
	t.Run("f32.min-signed-zero", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f32, f32}, []wasm.ValType{f32}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x96, 0x0b})
		bits := uint32(runAmd64u(t, m, f32b(0), f32b(float32(math.Copysign(0, -1)))))
		if bits != 0x80000000 {
			t.Fatalf("f32.min(+0,-0) bits = %#x, want -0", bits)
		}
	})
	t.Run("f64.min-signed-zero", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64, f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0xa4, 0x0b})
		bits := runAmd64u(t, m, f64b(0), f64b(math.Copysign(0, -1)))
		if bits != 0x8000000000000000 {
			t.Fatalf("f64.min(+0,-0) bits = %#x, want -0", bits)
		}
	})
	t.Run("f64.max-signed-zero", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64, f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0xa5, 0x0b})
		bits := runAmd64u(t, m, f64b(math.Copysign(0, -1)), f64b(0))
		if bits != 0 { // +0.0 has all-zero bits; -0.0 would be 0x8000...
			t.Fatalf("f64.max(-0,+0) bits = %#x, want +0", bits)
		}
	})
	t.Run("f64.load-add-fold", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{f64}, []byte{
			0x00,
			0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x3f, // f64.const 1.5
			0x20, 0x00, // local.get 0
			0x2b, 0x03, 0x00, // f64.load align=3 offset=0
			0xa0, // f64.add
			0x0b,
		})
		got, _, err := runMemAmd64(t, m, func(mem []byte) {
			binary.LittleEndian.PutUint64(mem, f64b(2.25))
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if v := math.Float64frombits(got); v != 3.75 {
			t.Fatalf("f64.const+load = %v, want 3.75", v)
		}
	})
	t.Run("f64.deferred-load-before-store", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{f64}, []byte{
			0x00,
			0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x3f, // f64.const 1.5
			0x20, 0x00, // local.get 0
			0x2b, 0x03, 0x00, // f64.load align=3 offset=0
			0x20, 0x00, // local.get 0
			0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x22, 0x40, // f64.const 9
			0x39, 0x03, 0x00, // f64.store align=3 offset=0
			0xa0, // f64.add; must use the pre-store loaded value
			0x0b,
		})
		got, mem, err := runMemAmd64(t, m, func(mem []byte) {
			binary.LittleEndian.PutUint64(mem, f64b(2.25))
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if v := math.Float64frombits(got); v != 3.75 {
			t.Fatalf("pre-store f64 load result = %v, want 3.75", v)
		}
		if v := math.Float64frombits(binary.LittleEndian.Uint64(mem)); v != 9 {
			t.Fatalf("post-store memory = %v, want 9", v)
		}
	})
	t.Run("f64.deferred-load-before-local-overwrite", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{f64}, []byte{
			0x00,
			0x20, 0x00, // local.get 0
			0x2b, 0x03, 0x00, // f64.load align=3 offset=0
			0x41, 0x08, // i32.const 8
			0x21, 0x00, // local.set 0; must not redirect the pending load
			0x0b,
		})
		got, _, err := runMemAmd64(t, m, func(mem []byte) {
			binary.LittleEndian.PutUint64(mem, f64b(2.25))
			binary.LittleEndian.PutUint64(mem[8:], f64b(9))
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if v := math.Float64frombits(got); v != 2.25 {
			t.Fatalf("pre-local-set f64 load result = %v, want 2.25", v)
		}
	})
	t.Run("f64.drop-deferred-load-guard-compile", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{}, []byte{
			0x00,
			0x20, 0x00, // local.get 0
			0x2b, 0x03, 0x00, // f64.load align=3 offset=0
			0x1a, // drop
			0x0b,
		})
		if _, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true}); err != nil {
			t.Fatalf("guard compile: %v", err)
		}
	})

	// f64.lt (comparison → i32), NaN-correct
	t.Run("f64.lt", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64, f64}, []wasm.ValType{i32}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x63, 0x0b})
		for _, tc := range []struct {
			a, b float64
			want uint32
		}{{1, 2, 1}, {2, 1, 0}, {1, 1, 0}, {math.NaN(), 1, 0}} {
			got := uint32(runAmd64u(t, m, f64b(tc.a), f64b(tc.b)))
			if got != tc.want {
				t.Fatalf("f64.lt(%v,%v) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		}
	})

	// i32.trunc_f64_s
	t.Run("i32.trunc_f64_s", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64}, []wasm.ValType{i32}, []byte{0x00,
			0x20, 0x00, 0xaa, 0x0b})
		for _, tc := range []struct {
			in   float64
			want int32
		}{{3.7, 3}, {-3.7, -3}, {0, 0}} {
			got := int32(uint32(runAmd64u(t, m, f64b(tc.in))))
			if got != tc.want {
				t.Fatalf("trunc(%v) = %d, want %d", tc.in, got, tc.want)
			}
		}
	})

	// f64.convert_i32_s
	t.Run("f64.convert_i32_s", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0xb7, 0x0b})
		got := math.Float64frombits(runAmd64u(t, m, u64(-5)))
		if got != -5 {
			t.Fatalf("convert = %v, want -5", got)
		}
	})

	// f64.promote_f32 and f32.demote_f64
	t.Run("promote", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f32}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0xbb, 0x0b})
		got := math.Float64frombits(runAmd64u(t, m, f32b(1.5)))
		if got != 1.5 {
			t.Fatalf("promote = %v, want 1.5", got)
		}
	})

	// reinterpret f64↔i64 (bit-exact)
	t.Run("reinterpret", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64}, []wasm.ValType{i64}, []byte{0x00,
			0x20, 0x00, 0xbd, 0x0b}) // i64.reinterpret_f64
		got := runAmd64u(t, m, f64b(2.5))
		if got != f64b(2.5) {
			t.Fatalf("reinterpret = %#x, want %#x", got, f64b(2.5))
		}
	})

	// trapping trunc: NaN → trap
	t.Run("trunc-nan-trap", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{f64}, []wasm.ValType{i32}, []byte{0x00,
			0x20, 0x00, 0xaa, 0x0b})
		_, _, err := runMemAmd64(t, m, nil, f64b(math.NaN()))
		if err == nil {
			t.Fatal("expected trunc-NaN trap, got nil")
		}
	})

	// select on f64 operands (branchy; no float cmov)
	t.Run("f64-select", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{f64}, []byte{0x00,
			0x44, 0, 0, 0, 0, 0, 0, 0xf8, 0x3f, // f64.const 1.5
			0x44, 0, 0, 0, 0, 0, 0, 0x04, 0x40, // f64.const 2.5
			0x20, 0x00, 0x1b, 0x0b}) // local.get 0; select
		for _, tc := range []struct {
			c    int32
			want float64
		}{{1, 1.5}, {0, 2.5}} {
			got := math.Float64frombits(runAmd64u(t, m, uint64(uint32(tc.c))))
			if got != tc.want {
				t.Fatalf("f64.select(c=%d) = %v, want %v", tc.c, got, tc.want)
			}
		}
	})

	// f64 through control flow + a call
	t.Run("f64-call", func(t *testing.T) {
		// func0(x) = func1(x, 2.0) ; func1(a,b) = a*b
		m := modFuncs(t,
			funcDef{[]wasm.ValType{f64}, []wasm.ValType{f64}, []byte{0x00,
				0x20, 0x00, 0x44, 0, 0, 0, 0, 0, 0, 0, 0x40, 0x10, 0x01, 0x0b}}, // f64.const 2.0 = 0x4000000000000000
			funcDef{[]wasm.ValType{f64, f64}, []wasm.ValType{f64}, []byte{0x00,
				0x20, 0x00, 0x20, 0x01, 0xa2, 0x0b}},
		)
		got := math.Float64frombits(runAmd64u(t, m, f64b(2.5)))
		if got != 5.0 {
			t.Fatalf("f64-call = %v, want 5.0", got)
		}
	})

	t.Run("mixed-f64-i32-call", func(t *testing.T) {
		// func0(x, n) = func1(x, n); func1(a, b) = a + f64(b)
		m := modFuncs(t,
			funcDef{[]wasm.ValType{f64, i32}, []wasm.ValType{f64}, []byte{0x00,
				0x20, 0x00, 0x20, 0x01, 0x10, 0x01, 0x0b}},
			funcDef{[]wasm.ValType{f64, i32}, []wasm.ValType{f64}, []byte{0x00,
				0x20, 0x00, 0x20, 0x01, 0xb7, 0xa0, 0x0b}},
		)
		got := math.Float64frombits(runAmd64u(t, m, f64b(1.5), u64(4)))
		if got != 5.5 {
			t.Fatalf("mixed-f64-i32-call = %v, want 5.5", got)
		}
	})
}

// TestAmd64Phase4Calls exercises internal (wasm→wasm) direct calls via the wrapper
// ABI: a simple caller/callee, recursion, and callee-trap propagation.
func TestAmd64Phase4Calls(t *testing.T) {
	// func0(x) = func1(x, 10) + 1 ; func1(a,b) = a + b
	t.Run("direct-call", func(t *testing.T) {
		m := modFuncs(t,
			funcDef{[]wasm.ValType{i32}, []wasm.ValType{i32}, []byte{0x00,
				0x20, 0x00, 0x41, 0x0a, 0x10, 0x01, 0x41, 0x01, 0x6a, 0x0b}},
			funcDef{[]wasm.ValType{i32, i32}, []wasm.ValType{i32}, []byte{0x00,
				0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}},
		)
		if got := runAmd64(t, m, 5); got != 16 {
			t.Fatalf("direct-call = %d, want 16", got)
		}
	})

	// recursive factorial: fact(n) = n==0 ? 1 : n*fact(n-1)
	t.Run("recursion", func(t *testing.T) {
		m := modFuncs(t,
			funcDef{[]wasm.ValType{i32}, []wasm.ValType{i32}, []byte{0x00,
				0x20, 0x00, 0x45, // n; eqz
				0x04, 0x7f, // if (result i32)
				0x41, 0x01, // 1
				0x05,                                     // else
				0x20, 0x00, 0x20, 0x00, 0x41, 0x01, 0x6b, // n, n, n-1
				0x10, 0x00, // call self
				0x6c,       // n * fact(n-1)
				0x0b, 0x0b, // end if, end func
			}},
		)
		for _, tc := range []struct{ n, want int32 }{{0, 1}, {1, 1}, {5, 120}, {6, 720}} {
			if got := runAmd64(t, m, tc.n); got != tc.want {
				t.Fatalf("fact(%d) = %d, want %d", tc.n, got, tc.want)
			}
		}
	})

	// call_indirect compiles (end-to-end table dispatch is verified at src/wago
	// integration, which sets up the runtime table from the elem section).
	t.Run("call_indirect-compiles", func(t *testing.T) {
		body := []byte{0x00,
			0x41, 0x00, // i32.const 0 (table index)
			0x11, 0x00, 0x00, // call_indirect type 0 table 0
			0x0b}
		fnBody := append(wasmtest.ULEB(uint32(len(body))), body...)
		tableType := []byte{0x70, 0x00, 0x01} // funcref, min 1
		b := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{i32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(4, wasmtest.Vec(tableType)),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(fnBody)),
		)
		m, err := wasm.DecodeModule(b)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, err := CompileModule(m); err != nil {
			t.Fatalf("call_indirect compile: %v", err)
		}
	})

	// callee trap (div by zero via unreachable) propagates through the caller
	t.Run("trap-propagation", func(t *testing.T) {
		m := modFuncs(t,
			funcDef{nil, []wasm.ValType{i32}, []byte{0x00, 0x10, 0x01, 0x0b}}, // func0 = call func1
			funcDef{nil, []wasm.ValType{i32}, []byte{0x00, 0x00, 0x0b}},       // func1 = unreachable
		)
		// Build the module through modFuncs but run via the trap-capturing harness.
		cm, err := CompileModule(m)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		eng, _ := runtime.NewEngine()
		defer eng.Close()
		jm, _ := runtime.NewJobMemory(65536)
		defer jm.Close()
		ar, _ := runtime.NewArena(4096)
		defer ar.Close()
		mem, entry, err := runtime.MapCode(cm.Code)
		if err != nil {
			t.Fatal(err)
		}
		defer runtime.Unmap(mem)
		res := ar.Alloc(64)
		trap := ar.Alloc(8)
		err = eng.Call(entry+uintptr(cm.Entry[0]), ar.Alloc(64), jm.LinearMemory(), trap, res)
		if err == nil {
			t.Fatal("expected trap to propagate through caller, got nil")
		}
	})
}

// TestAmd64BulkAndSat exercises bulk memory (memory.copy/fill) through the runtime
// and the saturating float→int truncations.
func TestAmd64BulkAndSat(t *testing.T) {
	// data.drop mutates only the per-instance passive-data descriptor length. These
	// low-level railshot checks install a minimal descriptor array and verify the
	// opcode stays stack-transparent while execution continues.
	t.Run("data.drop", func(t *testing.T) {
		passiveSegment := func(init ...byte) []byte {
			seg := append([]byte{0x01}, wasmtest.ULEB(uint32(len(init)))...)
			return append(seg, init...)
		}
		module := func(ft []byte, body []byte, segments ...[]byte) *wasm.Module {
			b := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(ft)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(12, wasmtest.ULEB(uint32(len(segments)))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
				wasmtest.Section(11, wasmtest.Vec(segments...)),
			)
			m, err := wasm.DecodeModule(b)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			return m
		}
		runWithPassiveData := func(t *testing.T, m *wasm.Module, segCount int, args ...uint64) uint64 {
			t.Helper()
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatalf("amd64 compile: %v", err)
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
			ar, err := runtime.NewArena(4096)
			if err != nil {
				t.Fatal(err)
			}
			defer ar.Close()
			desc := ar.Alloc(runtime.PassiveDataDescBytes * segCount)
			jm.SetPassiveDataPtr(uintptr(unsafe.Pointer(&desc[0])))
			mem, entry, err := runtime.MapCode(cm.Code)
			if err != nil {
				t.Fatalf("map: %v", err)
			}
			defer runtime.Unmap(mem)
			serArgs := ar.Alloc(256)
			results := ar.Alloc(256)
			trap := ar.Alloc(8)
			for i, a := range args {
				binary.LittleEndian.PutUint64(serArgs[i*8:], a)
			}
			if err := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results); err != nil {
				t.Fatalf("call: %v", err)
			}
			return binary.LittleEndian.Uint64(results)
		}

		t.Run("continues_after_drop", func(t *testing.T) {
			m := module(wasmtest.FuncType(nil, []wasm.ValType{i32}), []byte{0xfc, 0x09, 0x00, 0x41, 0x2a, 0x0b}, passiveSegment())
			if got := runWithPassiveData(t, m, 1); got != 42 {
				t.Fatalf("data.drop result = %d, want 42", got)
			}
		})

		t.Run("does_not_pop_operands", func(t *testing.T) {
			m := module(wasmtest.FuncType([]wasm.ValType{i32}, []wasm.ValType{i32}), []byte{0x20, 0x00, 0xfc, 0x09, 0x00, 0x0b}, passiveSegment('x'))
			if got := runWithPassiveData(t, m, 1, 123); got != 123 {
				t.Fatalf("data.drop stack result = %d, want 123", got)
			}
		})

		t.Run("repeated_segments", func(t *testing.T) {
			m := module(wasmtest.FuncType(nil, []wasm.ValType{i32}), []byte{0xfc, 0x09, 0x00, 0xfc, 0x09, 0x01, 0x41, 0x2a, 0x0b}, passiveSegment(), passiveSegment('a', 'b'))
			if got := runWithPassiveData(t, m, 2); got != 42 {
				t.Fatalf("repeated data.drop result = %d, want 42", got)
			}
		})
	})

	// memory.fill: fill n bytes at dst with val
	t.Run("memory.fill", func(t *testing.T) {
		// f(dst, val, n) { memory.fill }
		body := []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x20, 0x02, // dst, val, n
			0xfc, 0x0b, 0x00, // memory.fill, mem 0
			0x41, 0x00, // return 0 (dummy i32)
			0x0b}
		m := modMem(t, 1, []wasm.ValType{i32, i32, i32}, []wasm.ValType{i32}, body)
		_, lin, err := runMemAmd64(t, m, nil, 16, 0xAB, 8)
		if err != nil {
			t.Fatal(err)
		}
		for i := 16; i < 24; i++ {
			if lin[i] != 0xAB {
				t.Fatalf("fill byte %d = %#x, want 0xAB", i, lin[i])
			}
		}
		if lin[24] == 0xAB {
			t.Fatal("fill overran")
		}
	})

	// memory.copy: copy n bytes src→dst
	t.Run("memory.copy", func(t *testing.T) {
		body := []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x20, 0x02, // dst, src, n
			0xfc, 0x0a, 0x00, 0x00, // memory.copy, mem 0,0
			0x41, 0x00, 0x0b}
		m := modMem(t, 1, []wasm.ValType{i32, i32, i32}, []wasm.ValType{i32}, body)
		_, lin, err := runMemAmd64(t, m, func(l []byte) {
			for i := 0; i < 8; i++ {
				l[100+i] = byte(i + 1)
			}
		}, 200, 100, 8)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 8; i++ {
			if lin[200+i] != byte(i+1) {
				t.Fatalf("copy byte %d = %#x, want %#x", i, lin[200+i], i+1)
			}
		}
	})

	// trunc_sat: all scalar non-trapping conversion opcodes pin NaN→0,
	// negative unsigned→0, and overflow→clamp semantics.
	t.Run("trunc_sat_scalar_opcodes", func(t *testing.T) {
		tests := []struct {
			name   string
			param  wasm.ValType
			result wasm.ValType
			opcode byte
			cases  []struct {
				arg  uint64
				want uint64
			}
		}{
			{name: "i32.trunc_sat_f32_s", param: wasm.F32, result: i32, opcode: 0x00, cases: []struct {
				arg  uint64
				want uint64
			}{
				{f32b(3.9), 3}, {f32b(-3.9), 0xfffffffd}, {f32b(float32(math.NaN())), 0},
				{f32b(float32(math.Inf(1))), 0x7fffffff}, {f32b(float32(math.Inf(-1))), 0x80000000},
			}},
			{name: "i32.trunc_sat_f32_u", param: wasm.F32, result: i32, opcode: 0x01, cases: []struct {
				arg  uint64
				want uint64
			}{
				{f32b(3.9), 3}, {f32b(-1.9), 0}, {f32b(float32(math.NaN())), 0}, {f32b(float32(math.Inf(1))), 0xffffffff},
			}},
			{name: "i32.trunc_sat_f64_s", param: wasm.F64, result: i32, opcode: 0x02, cases: []struct {
				arg  uint64
				want uint64
			}{
				{f64b(3.9), 3}, {f64b(-3.9), 0xfffffffd}, {f64b(math.NaN()), 0},
				{f64b(math.Inf(1)), 0x7fffffff}, {f64b(math.Inf(-1)), 0x80000000},
			}},
			{name: "i32.trunc_sat_f64_u", param: wasm.F64, result: i32, opcode: 0x03, cases: []struct {
				arg  uint64
				want uint64
			}{
				{f64b(3.9), 3}, {f64b(-1.9), 0}, {f64b(math.NaN()), 0}, {f64b(math.Inf(1)), 0xffffffff},
			}},
			{name: "i64.trunc_sat_f32_s", param: wasm.F32, result: i64, opcode: 0x04, cases: []struct {
				arg  uint64
				want uint64
			}{
				{f32b(3.9), 3}, {f32b(-3.9), u64(-3)}, {f32b(float32(math.NaN())), 0},
				{f32b(float32(math.Inf(1))), 0x7fffffffffffffff}, {f32b(float32(math.Inf(-1))), 0x8000000000000000},
			}},
			{name: "i64.trunc_sat_f32_u", param: wasm.F32, result: i64, opcode: 0x05, cases: []struct {
				arg  uint64
				want uint64
			}{
				{f32b(3.9), 3}, {f32b(-1.9), 0}, {f32b(float32(math.NaN())), 0}, {f32b(float32(math.Inf(1))), ^uint64(0)},
			}},
			{name: "i64.trunc_sat_f64_s", param: wasm.F64, result: i64, opcode: 0x06, cases: []struct {
				arg  uint64
				want uint64
			}{
				{f64b(3.9), 3}, {f64b(-3.9), u64(-3)}, {f64b(math.NaN()), 0},
				{f64b(math.Inf(1)), 0x7fffffffffffffff}, {f64b(math.Inf(-1)), 0x8000000000000000},
			}},
			{name: "i64.trunc_sat_f64_u", param: wasm.F64, result: i64, opcode: 0x07, cases: []struct {
				arg  uint64
				want uint64
			}{
				{f64b(3.9), 3}, {f64b(-1.9), 0}, {f64b(math.NaN()), 0}, {f64b(math.Inf(1)), ^uint64(0)},
			}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				m := mod1(t, []wasm.ValType{tt.param}, []wasm.ValType{tt.result}, []byte{0x00,
					0x20, 0x00, 0xfc, tt.opcode, 0x0b})
				for _, tc := range tt.cases {
					got := runAmd64u(t, m, tc.arg)
					if wasm.EqualValType(tt.result, i64) {
						if got != tc.want {
							t.Fatalf("%s(%#x) = %#x, want %#x", tt.name, tc.arg, got, tc.want)
						}
					} else if uint32(got) != uint32(tc.want) {
						t.Fatalf("%s(%#x) = %#x, want %#x", tt.name, tc.arg, uint32(got), uint32(tc.want))
					}
				}
			})
		}
	})
}

// TestAmd64Phase3Control exercises the control-flow constructs and traps: if/else,
// block+br, loop+br_if, br_table, early return, and unreachable — all through the
// real runtime via the canonical-slot reconciliation model.
func TestAmd64Phase3Control(t *testing.T) {
	// if/else returning a value
	t.Run("if-else", func(t *testing.T) {
		body := []byte{0x00,
			0x20, 0x00, // local.get 0
			0x04, 0x7f, // if (result i32)
			0x41, 0x0a, // i32.const 10
			0x05,       // else
			0x41, 0x14, // i32.const 20
			0x0b, // end if
			0x0b, // end func
		}
		for _, tc := range []struct{ x, want int32 }{{1, 10}, {0, 20}, {7, 10}} {
			m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
			if got := runAmd64(t, m, tc.x); got != tc.want {
				t.Fatalf("if-else(%d) = %d, want %d", tc.x, got, tc.want)
			}
		}
	})

	// if without else: cond-false path yields the pre-pushed value unchanged
	t.Run("if-no-else", func(t *testing.T) {
		// f(x) = (i32.const 5) + (if x then +100 else +0) via a block value:
		// x!=0 → 105, x==0 → 5
		body := []byte{0x00,
			0x41, 0x05, // i32.const 5
			0x20, 0x00, // local.get 0
			0x04, 0x40, // if (void)
			0x41, 0x64, 0x6a, // ... but stack under if is [5]; can't touch. use a local instead
			0x0b,
			0x0b,
		}
		_ = body // replaced below by a local-based form
		// Cleaner: use a result local.
		body = []byte{0x01, 0x01, 0x7f, // 1 local i32 (local1)
			0x41, 0x05, 0x21, 0x01, // local1 = 5
			0x20, 0x00, // local.get 0
			0x04, 0x40, // if (void)
			0x20, 0x01, 0x41, 0xe4, 0x00, 0x6a, 0x21, 0x01, // local1 = local1 + 100 (SLEB 100)
			0x0b,       // end if
			0x20, 0x01, // local.get 1
			0x0b, // end func
		}
		for _, tc := range []struct{ x, want int32 }{{1, 105}, {0, 5}} {
			m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
			if got := runAmd64(t, m, tc.x); got != tc.want {
				t.Fatalf("if-no-else(%d) = %d, want %d", tc.x, got, tc.want)
			}
		}
	})

	// block + br: br 0 exits the block carrying its result
	t.Run("block-br", func(t *testing.T) {
		body := []byte{0x00,
			0x02, 0x7f, // block (result i32)
			0x41, 0x05, // i32.const 5
			0x0c, 0x00, // br 0
			0x41, 0x63, // i32.const 99 (dead)
			0x0b, // end block
			0x0b, // end func
		}
		m := mod1(t, nil, []wasm.ValType{i32}, body)
		if got := runAmd64(t, m); got != 5 {
			t.Fatalf("block-br = %d, want 5", got)
		}
	})

	// loop + br_if: sum 0..n-1
	t.Run("loop-sum", func(t *testing.T) {
		body := []byte{0x01, 0x02, 0x7f, // 2 locals i32 (i=local1, acc=local2)
			0x02, 0x40, // block (void)
			0x03, 0x40, // loop (void)
			0x20, 0x01, 0x20, 0x00, 0x4e, 0x0d, 0x01, // if i >= n: br 1 (exit block)
			0x20, 0x02, 0x20, 0x01, 0x6a, 0x21, 0x02, // acc += i
			0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, // i += 1
			0x0c, 0x00, // br 0 (loop)
			0x0b,       // end loop
			0x0b,       // end block
			0x20, 0x02, // local.get acc
			0x0b, // end func
		}
		for _, tc := range []struct{ n, want int32 }{{5, 10}, {10, 45}, {0, 0}, {1, 0}} {
			m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
			if got := runAmd64(t, m, tc.n); got != tc.want {
				t.Fatalf("loop-sum(%d) = %d, want %d", tc.n, got, tc.want)
			}
		}
	})

	// early return from inside an if
	t.Run("return", func(t *testing.T) {
		body := []byte{0x00,
			0x20, 0x00, // local.get 0
			0x04, 0x40, // if (void)
			0x41, 0xe3, 0x00, 0x0f, // i32.const 99 (SLEB); return
			0x0b,       // end if
			0x41, 0x01, // i32.const 1
			0x0b, // end func
		}
		for _, tc := range []struct{ x, want int32 }{{1, 99}, {0, 1}} {
			m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
			if got := runAmd64(t, m, tc.x); got != tc.want {
				t.Fatalf("return(%d) = %d, want %d", tc.x, got, tc.want)
			}
		}
	})

	// br_table dispatch: x==0 → 42, else → 0 (result held in a local)
	t.Run("br_table", func(t *testing.T) {
		body := []byte{0x01, 0x01, 0x7f, // 1 local i32 (local1, init 0)
			0x02, 0x40, // block L1 (void)
			0x02, 0x40, // block L0 (void)
			0x20, 0x00, // local.get 0
			0x0e, 0x01, 0x00, 0x01, // br_table [0] default 1
			0x0b,                   // end L0  (x==0 continues here)
			0x41, 0x2a, 0x21, 0x01, // local1 = 42
			0x0c, 0x00, // br 0 (to L1 end)
			0x0b,       // end L1
			0x20, 0x01, // local.get 1
			0x0b, // end func
		}
		for _, tc := range []struct{ x, want int32 }{{0, 42}, {5, 0}, {1, 0}} {
			m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
			if got := runAmd64(t, m, tc.x); got != tc.want {
				t.Fatalf("br_table(%d) = %d, want %d", tc.x, got, tc.want)
			}
		}
	})

	t.Run("zero-unpinned-local-through-if", func(t *testing.T) {
		body := []byte{0x01, 0x08, 0x7f} // 8 locals i32; local8 is below pinning priority
		for x := byte(1); x <= 4; x++ {
			for i := 0; i < 4; i++ {
				body = append(body, 0x20, x, 0x1a) // local.get x; drop
			}
		}
		body = append(body,
			0x20, 0x00, // local.get cond
			0x04, 0x40, // if
			0x41, 0x07, // i32.const 7
			0x21, 0x08, // local.set 8
			0x0b,       // end if
			0x20, 0x08, // local.get 8
			0x0b, // end func
		)
		m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
		if got := runAmd64(t, m, 0); got != 0 {
			t.Fatalf("lazy-zero false path = %d, want 0", got)
		}
		if got := runAmd64(t, m, 1); got != 7 {
			t.Fatalf("lazy-zero true path = %d, want 7", got)
		}
	})

	// unreachable traps
	t.Run("unreachable", func(t *testing.T) {
		m := modMem(t, 1, nil, []wasm.ValType{i32}, []byte{0x00, 0x00, 0x0b})
		_, _, err := runMemAmd64(t, m, nil)
		if err == nil {
			t.Fatal("expected unreachable trap, got nil")
		}
	})
}

// TestAmd64Phase2Memory exercises linear-memory loads/stores (all widths, signed &
// unsigned, i32/i64), memarg offset folding, memory.size, and the bounds-check
// trap — all through the real runtime's guarded linear memory.
func TestAmd64Phase2Memory(t *testing.T) {
	// f(ptr,val) { store val at ptr; return load at ptr }  (i32 roundtrip)
	t.Run("i32.store-load", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x36, 0x02, 0x00, // i32.store [ptr]=val
			0x20, 0x00, 0x28, 0x02, 0x00, // i32.load [ptr]
			0x0b})
		got, lin, err := runMemAmd64(t, m, nil, 256, 0x1234ABCD)
		if err != nil {
			t.Fatal(err)
		}
		if uint32(got) != 0x1234ABCD {
			t.Fatalf("load = %#x, want 0x1234ABCD", uint32(got))
		}
		if binary.LittleEndian.Uint32(lin[256:]) != 0x1234ABCD {
			t.Fatalf("memory not written")
		}
	})

	// i64 roundtrip
	t.Run("i64.store-load", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32, i64}, []wasm.ValType{i64}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x37, 0x03, 0x00, // i64.store
			0x20, 0x00, 0x29, 0x03, 0x00, // i64.load
			0x0b})
		got, _, err := runMemAmd64(t, m, nil, 8, 0x1122334455667788)
		if err != nil {
			t.Fatal(err)
		}
		if got != 0x1122334455667788 {
			t.Fatalf("i64 load = %#x", got)
		}
	})

	// sub-width loads with sign/zero extension
	subCases := []struct {
		name string
		op   []byte // load opcode + memarg
		set  func([]byte)
		want uint32
	}{
		{"load8_u", []byte{0x2d, 0x00, 0x00}, func(l []byte) { l[10] = 0xFF }, 0xFF},
		{"load8_s", []byte{0x2c, 0x00, 0x00}, func(l []byte) { l[10] = 0xFF }, 0xFFFFFFFF},
		{"load16_u", []byte{0x2f, 0x01, 0x00}, func(l []byte) { binary.LittleEndian.PutUint16(l[10:], 0x8000) }, 0x8000},
		{"load16_s", []byte{0x2e, 0x01, 0x00}, func(l []byte) { binary.LittleEndian.PutUint16(l[10:], 0x8000) }, 0xFFFF8000},
	}
	for _, c := range subCases {
		t.Run(c.name, func(t *testing.T) {
			body := append([]byte{0x00, 0x41, 0x0a}, c.op...) // i32.const 10, <load>
			body = append(body, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{i32}, body)
			got, _, err := runMemAmd64(t, m, c.set)
			if err != nil {
				t.Fatal(err)
			}
			if uint32(got) != c.want {
				t.Fatalf("%s = %#x, want %#x", c.name, uint32(got), c.want)
			}
		})
	}

	// memarg static offset folding: load at [base + 4] with offset=4
	t.Run("offset-fold", func(t *testing.T) {
		m := modMem(t, 1, nil, []wasm.ValType{i32}, []byte{0x00,
			0x41, 0x08, 0x28, 0x02, 0x04, 0x0b}) // i32.const 8; i32.load offset=4
		got, _, err := runMemAmd64(t, m, func(l []byte) { binary.LittleEndian.PutUint32(l[12:], 0xCAFEF00D) })
		if err != nil {
			t.Fatal(err)
		}
		if uint32(got) != 0xCAFEF00D {
			t.Fatalf("offset load = %#x", uint32(got))
		}
	})

	// memory.size = declared pages
	t.Run("memory.size", func(t *testing.T) {
		m := modMem(t, 1, nil, []wasm.ValType{i32}, []byte{0x00, 0x3f, 0x00, 0x0b})
		got, _, err := runMemAmd64(t, m, nil)
		if err != nil {
			t.Fatal(err)
		}
		if uint32(got) != 1 {
			t.Fatalf("memory.size = %d, want 1", uint32(got))
		}
	})

	// deferred-load folding: load(0) + load(4) → add reg, [mem]
	t.Run("load-fold-add", func(t *testing.T) {
		m := modMem(t, 1, nil, []wasm.ValType{i32}, []byte{0x00,
			0x41, 0x00, 0x28, 0x02, 0x00, // i32.load [0]
			0x41, 0x00, 0x28, 0x02, 0x04, // i32.load [0] offset 4  → mem[4]
			0x6a, 0x0b}) // i32.add
		got, _, err := runMemAmd64(t, m, func(l []byte) {
			binary.LittleEndian.PutUint32(l[0:], 10)
			binary.LittleEndian.PutUint32(l[4:], 20)
		})
		if err != nil {
			t.Fatal(err)
		}
		if uint32(got) != 30 {
			t.Fatalf("load-fold-add = %d, want 30", uint32(got))
		}
	})

	// load/store aliasing: a deferred load must read the value BEFORE a later store
	// to the same address. f(p,v) = { t = load(p); store(p, v); t }
	t.Run("load-store-aliasing", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, []byte{0x00,
			0x20, 0x00, 0x28, 0x02, 0x00, // load(p)   [deferred]
			0x20, 0x00, 0x20, 0x01, 0x36, 0x02, 0x00, // store(p, v)
			0x0b}) // ...leaving the loaded value as the result
		got, _, err := runMemAmd64(t, m, func(l []byte) {
			binary.LittleEndian.PutUint32(l[64:], 111) // mem[64] = 111 (old value)
		}, 64, 999)
		if err != nil {
			t.Fatal(err)
		}
		if uint32(got) != 111 {
			t.Fatalf("load-store-aliasing = %d, want 111 (pre-store value)", uint32(got))
		}
	})

	// load folded into a compare
	t.Run("load-fold-cmp", func(t *testing.T) {
		m := modMem(t, 1, nil, []wasm.ValType{i32}, []byte{0x00,
			0x41, 0x00, 0x28, 0x02, 0x00, // load(0)
			0x41, 0x05, 0x46, 0x0b}) // == 5
		got, _, err := runMemAmd64(t, m, func(l []byte) { binary.LittleEndian.PutUint32(l[0:], 5) })
		if err != nil {
			t.Fatal(err)
		}
		if uint32(got) != 1 {
			t.Fatalf("load-fold-cmp = %d, want 1", uint32(got))
		}
	})

	// out-of-bounds load traps (offset 65536 in a 1-page memory)
	t.Run("oob-trap", func(t *testing.T) {
		m := modMem(t, 1, nil, []wasm.ValType{i32}, []byte{0x00,
			0x41, 0x80, 0x80, 0x04, 0x28, 0x02, 0x00, 0x0b}) // i32.const 65536; i32.load
		_, _, err := runMemAmd64(t, m, nil)
		if err == nil {
			t.Fatal("expected out-of-bounds trap, got nil")
		}
	})
}

// TestAmd64Phase1 exercises the full scalar integer ISA: mul, div/rem (signed &
// unsigned), shifts & rotates (const and variable count), clz/ctz/popcnt, all
// relational compares + eqz, and constant folding — for both i32 and i64.
func TestAmd64Phase1(t *testing.T) {
	noDecl := []byte{0x00}
	cases := []struct {
		name    string
		params  []wasm.ValType
		results []wasm.ValType
		body    []byte
		args    []uint64
		want    uint64 // compared masked to result width
	}{
		// --- i32 arithmetic ---
		{"i32.mul", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x6c, 0x0b}, []uint64{6, 7}, 42},
		{"i32.mul-imm", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x41, 0x03, 0x6c, 0x0b}, []uint64{5}, 15}, // x*3 → lea [x+x*2]
		{"i32.mul5-lea", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x41, 0x05, 0x6c, 0x0b}, []uint64{5}, 25}, // x*5 → lea [x+x*4]
		{"i32.mul9-lea", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x41, 0x09, 0x6c, 0x0b}, []uint64{5}, 45}, // x*9 → lea [x+x*8]
		{"i32.mul7-imul", []wasm.ValType{i32}, []wasm.ValType{i32}, // not 3/5/9 → IMUL fall-through
			[]byte{0x20, 0x00, 0x41, 0x07, 0x6c, 0x0b}, []uint64{5}, 35},
		{"i32.mul3-wrap", []wasm.ValType{i32}, []wasm.ValType{i32}, // 32-bit LEA wraps mod 2^32
			[]byte{0x20, 0x00, 0x41, 0x03, 0x6c, 0x0b}, []uint64{0x55555556}, 2},
		{"i32.mul3-deferred-left", []wasm.ValType{i32}, []wasm.ValType{i32}, // (x+1)*3, deferred left → IMUL
			[]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x41, 0x03, 0x6c, 0x0b}, []uint64{4}, 15},
		{"i64.mul5-lea", []wasm.ValType{i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0x42, 0x05, 0x7e, 0x0b}, []uint64{5}, 25}, // i64 x*5 → lea [x+x*4]
		{"i32.div_s", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x6d, 0x0b}, []uint64{uint64(uint32(0xFFFFFFEC)), 3}, uint64(uint32(0xFFFFFFFA))}, // -20/3=-6
		{"i32.div_u", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x6e, 0x0b}, []uint64{20, 3}, 6},
		{"i32.rem_s", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x6f, 0x0b}, []uint64{uint64(uint32(0xFFFFFFEC)), 3}, uint64(uint32(0xFFFFFFFE))}, // -20%3=-2
		{"i32.rem_u", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x70, 0x0b}, []uint64{20, 3}, 2},

		// --- i32 shifts / rotates ---
		{"i32.shl-const", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x41, 0x04, 0x74, 0x0b}, []uint64{3}, 48},
		{"i32.shr_u-const", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x41, 0x04, 0x76, 0x0b}, []uint64{uint64(uint32(0xFFFFFFF0))}, 0x0FFFFFFF},
		{"i32.shr_s-const", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x41, 0x02, 0x75, 0x0b}, []uint64{uint64(uint32(0xFFFFFFF0))}, uint64(uint32(0xFFFFFFFC))}, // -16>>2=-4
		{"i32.shl-var", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x74, 0x0b}, []uint64{1, 5}, 32},
		{"i32.rotl-var", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x77, 0x0b}, []uint64{0x12345678, 8}, 0x34567812},
		{"i32.rotr-var", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x78, 0x0b}, []uint64{0x12345678, 8}, 0x78123456},

		// --- i32 unary bit ops ---
		{"i32.clz", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x67, 0x0b}, []uint64{1}, 31},
		{"i32.ctz", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x68, 0x0b}, []uint64{8}, 3},
		{"i32.popcnt", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x69, 0x0b}, []uint64{0xFF}, 8},

		// --- i32 compares / eqz (result i32 bool) ---
		{"i32.eqz-true", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x45, 0x0b}, []uint64{0}, 1},
		{"i32.eqz-false", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x45, 0x0b}, []uint64{5}, 0},
		{"i32.eq", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x46, 0x0b}, []uint64{4, 4}, 1},
		{"i32.lt_s", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x48, 0x0b}, []uint64{uint64(uint32(0xFFFFFFFF)), 1}, 1}, // -1 < 1
		{"i32.lt_u", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x49, 0x0b}, []uint64{uint64(uint32(0xFFFFFFFF)), 1}, 0}, // 0xFFFFFFFF <u 1 = false
		{"i32.gt_u", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x4b, 0x0b}, []uint64{uint64(uint32(0xFFFFFFFF)), 1}, 1},

		// --- constant folding (no runtime op emitted) ---
		{"fold-add", nil, []wasm.ValType{i32},
			[]byte{0x41, 0x02, 0x41, 0x03, 0x6a, 0x0b}, nil, 5},
		{"fold-shl", nil, []wasm.ValType{i32},
			[]byte{0x41, 0x01, 0x41, 0x04, 0x74, 0x0b}, nil, 16},
		{"fold-mul-add", nil, []wasm.ValType{i32}, // (3*4)+5 all folded
			[]byte{0x41, 0x03, 0x41, 0x04, 0x6c, 0x41, 0x05, 0x6a, 0x0b}, nil, 17},

		// --- i64 ---
		{"i64.mul", []wasm.ValType{i64, i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x7e, 0x0b}, []uint64{0x100000000, 3}, 0x300000000},
		{"i64.div_s", []wasm.ValType{i64, i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x7f, 0x0b}, []uint64{u64(-100), 7}, u64(-14)},
		{"i64.shl-var", []wasm.ValType{i64, i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x86, 0x0b}, []uint64{1, 40}, 1 << 40},
		{"i64.clz", []wasm.ValType{i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0x79, 0x0b}, []uint64{1}, 63},
		{"i64.eq", []wasm.ValType{i64, i64}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x51, 0x0b}, []uint64{0x100000000, 0x100000000}, 1},

		// --- select ---
		{"select-true", []wasm.ValType{i32, i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1b, 0x0b}, []uint64{11, 22, 1}, 11},
		{"select-false", []wasm.ValType{i32, i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1b, 0x0b}, []uint64{11, 22, 0}, 22},
		{"select-i64", []wasm.ValType{i64, i64, i32}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1b, 0x0b}, []uint64{0x700000000, 0x900000000, 0}, 0x900000000},
		// select on a fusable compare condition (trySelectOnFlags): min(a,b) via
		// select(a, b, a<b). Exercises the flags path (no materialized boolean).
		{"select-cmp-true", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x00, 0x20, 0x01, 0x48, 0x1b, 0x0b}, []uint64{11, 22}, 11}, // 11<22 → a
		{"select-cmp-false", []wasm.ValType{i32, i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x00, 0x20, 0x01, 0x48, 0x1b, 0x0b}, []uint64{22, 11}, 11}, // 22<11 false → b
		{"select-cmp-i64", []wasm.ValType{i64, i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x00, 0x20, 0x01, 0x53, 0x1b, 0x0b}, []uint64{0x900000000, 0x700000000}, 0x700000000}, // min i64

		// --- width conversions / sign extension ---
		{"i32.wrap_i64", []wasm.ValType{i64}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0xa7, 0x0b}, []uint64{0xFFFFFFFF_00000005}, 5},
		{"i64.extend_i32_s", []wasm.ValType{i32}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0xac, 0x0b}, []uint64{uint64(uint32(0xFFFFFFFF))}, u64(-1)},
		{"i64.extend_i32_u", []wasm.ValType{i32}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0xad, 0x0b}, []uint64{uint64(uint32(0xFFFFFFFF))}, 0xFFFFFFFF},
		{"i32.extend8_s", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0xc0, 0x0b}, []uint64{0x12345680}, uint64(uint32(0xFFFFFF80))},
		{"i32.extend16_s", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0xc1, 0x0b}, []uint64{0x12348000}, uint64(uint32(0xFFFF8000))},
		{"i64.extend8_s", []wasm.ValType{i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0xc2, 0x0b}, []uint64{0x123456789abcde80}, u64(-0x80)},
		{"i64.extend16_s", []wasm.ValType{i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0xc3, 0x0b}, []uint64{0x123456789abc8000}, u64(-0x8000)},
		{"i64.extend32_s", []wasm.ValType{i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0xc4, 0x0b}, []uint64{0x1234567880000000}, u64(-0x80000000)},

		// --- combined expression exercising the allocator + folding ---
		// f(x) = (x*x) - (x<<1) + 7
		{"i32.combined", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{
				0x20, 0x00, 0x20, 0x00, 0x6c, // x*x
				0x20, 0x00, 0x41, 0x01, 0x74, // x<<1
				0x6b,             // -
				0x41, 0x07, 0x6a, // +7
				0x0b,
			}, []uint64{5}, 5*5 - 5*2 + 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := mod1(t, c.params, c.results, append(append([]byte{}, noDecl...), c.body...))
			got := runAmd64u(t, m, c.args...)
			wide := len(c.results) == 1 && wasm.EqualValType(c.results[0], i64)
			if wide {
				if got != c.want {
					t.Fatalf("%s = %#x, want %#x", c.name, got, c.want)
				}
			} else if uint32(got) != uint32(c.want) {
				t.Fatalf("%s = %#x, want %#x", c.name, uint32(got), uint32(c.want))
			}
		})
	}
}

// TestExecIfElseLocalMerge is the regression test for the opElse merge-edge
// reconcile (the root cause of #68): the then-branch of an if/else jumps to the
// if's end, and that edge must converge pinned-local state (STACK_REG) like any
// branch. Two variants:
//   - dirty: the then-branch writes a pinned local and jumps; without the
//     reconcile the merge believes reg==slot, a later call skips the dirty store,
//     and the lazy reload reads the stale slot.
//   - stale-reg: the then-branch makes a call (clobbering pinned regs) and jumps;
//     without the reconcile the merge believes the register is valid and a later
//     read uses the callee's leftover garbage.
//
// clobber3 is a call-free callee with three pinned locals, guaranteeing the
// caller's pinned registers are deterministically overwritten.
func TestExecIfElseLocalMerge(t *testing.T) {
	clobber3 := funcDef{nil, []wasm.ValType{i32}, []byte{
		0x01, 0x03, 0x7f, // 3 × i32 locals
		0x41, 0x01, 0x21, 0x00, // a = 1
		0x41, 0x02, 0x21, 0x01, // b = 2
		0x41, 0x03, 0x21, 0x02, // c = 3
		0x20, 0x00, 0x20, 0x01, 0x6a, 0x20, 0x02, 0x6a, // a+b+c
		0x0b,
	}}

	t.Run("dirty-then-edge", func(t *testing.T) {
		// f(x): l=7; if x { l=13 } else { nop }; call clobber3; return l
		m := modFuncs(t,
			funcDef{[]wasm.ValType{i32}, []wasm.ValType{i32}, []byte{
				0x01, 0x01, 0x7f, // 1 × i32 local (idx 1)
				0x41, 0x07, 0x21, 0x01, // l = 7
				0x20, 0x00, // x
				0x04, 0x40, // if (void)
				0x41, 0x0d, 0x21, 0x01, // l = 13
				0x05, 0x01, // else; nop
				0x0b,             // end
				0x10, 0x01, 0x1a, // call clobber3; drop
				0x20, 0x01, // l
				0x0b,
			}},
			clobber3,
		)
		if got := uint32(runAmd64u(t, m, 1)); got != 13 {
			t.Fatalf("then path: l = %d, want 13", got)
		}
		if got := uint32(runAmd64u(t, m, 0)); got != 7 {
			t.Fatalf("else path: l = %d, want 7", got)
		}
	})

	t.Run("stale-reg-then-edge", func(t *testing.T) {
		// f(x): l=7; if x { call clobber3; drop } else { nop }; return l
		m := modFuncs(t,
			funcDef{[]wasm.ValType{i32}, []wasm.ValType{i32}, []byte{
				0x01, 0x01, 0x7f, // 1 × i32 local (idx 1)
				0x41, 0x07, 0x21, 0x01, // l = 7
				0x20, 0x00, // x
				0x04, 0x40, // if (void)
				0x10, 0x01, 0x1a, // call clobber3; drop
				0x05, 0x01, // else; nop
				0x0b,       // end
				0x20, 0x01, // l
				0x0b,
			}},
			clobber3,
		)
		if got := uint32(runAmd64u(t, m, 1)); got != 7 {
			t.Fatalf("then path: l = %d, want 7", got)
		}
		if got := uint32(runAmd64u(t, m, 0)); got != 7 {
			t.Fatalf("else path: l = %d, want 7", got)
		}
	})
}

// TestExecAlgebraicSimplify covers the pushBinOp constant-RHS identities and
// strength reductions (P4): results must match the unsimplified semantics,
// including unsigned div/rem edge values and shift-count masking.
func TestExecAlgebraicSimplify(t *testing.T) {
	g0 := []byte{0x20, 0x00} // local.get 0
	cases := []struct {
		name string
		body []byte
		arg  uint64
		want uint32
	}{
		{"add0", append(g0, 0x41, 0x00, 0x6a, 0x0b), 7, 7},
		{"sub0", append(g0, 0x41, 0x00, 0x6b, 0x0b), 7, 7},
		{"or0", append(g0, 0x41, 0x00, 0x72, 0x0b), 7, 7},
		{"xor0", append(g0, 0x41, 0x00, 0x73, 0x0b), 7, 7},
		{"and-1", append(g0, 0x41, 0x7f, 0x71, 0x0b), 7, 7},
		{"and0", append(g0, 0x41, 0x00, 0x71, 0x0b), 7, 0},
		{"mul1", append(g0, 0x41, 0x01, 0x6c, 0x0b), 7, 7},
		{"mul0", append(g0, 0x41, 0x00, 0x6c, 0x0b), 7, 0},
		{"mul8-shl", append(g0, 0x41, 0x08, 0x6c, 0x0b), 5, 40},
		{"mul5-lea", append(g0, 0x41, 0x05, 0x6c, 0x0b), 5, 25},
		{"mul9-lea", append(g0, 0x41, 0x09, 0x6c, 0x0b), 7, 63},
		{"divu8-shr", append(g0, 0x41, 0x08, 0x6e, 0x0b), 100, 12},
		{"divu8-shr-big", append(g0, 0x41, 0x08, 0x6e, 0x0b), 0xFFFFFFF0, 0x1FFFFFFE},
		{"divu1", append(g0, 0x41, 0x01, 0x6e, 0x0b), 100, 100},
		{"remu8-and", append(g0, 0x41, 0x08, 0x70, 0x0b), 100, 4},
		{"remu1", append(g0, 0x41, 0x01, 0x70, 0x0b), 100, 0},
		{"shl0", append(g0, 0x41, 0x00, 0x74, 0x0b), 7, 7},
		{"shl32-masked", append(g0, 0x41, 0x20, 0x74, 0x0b), 7, 7},
		{"sub-self", append(append([]byte{}, g0...), append(g0, 0x6b, 0x0b)...), 9, 0},
		{"xor-self", append(append([]byte{}, g0...), append(g0, 0x73, 0x0b)...), 9, 0},
		{"and-self", append(append([]byte{}, g0...), append(g0, 0x71, 0x0b)...), 9, 9},
		{"or-self", append(append([]byte{}, g0...), append(g0, 0x72, 0x0b)...), 9, 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := append([]byte{0x00}, c.body...) // no local decls
			m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
			if got := uint32(runAmd64u(t, m, c.arg)); got != c.want {
				t.Fatalf("%s(%d) = %d, want %d", c.name, c.arg, got, c.want)
			}
		})
	}
}

// TestExecScaledIndexAdd covers the add(x, shl(y,k)) → LEA scaled-index fusion:
// both operand orders, i32/i64, k inside and outside the encodable 1..3 range,
// and the memory-address shape (fused ea feeding a bounds-checked load).
func TestExecScaledIndexAdd(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		args []uint64
		want uint64
	}{
		// f(b,i) = b + (i<<3)
		{"i32-b-plus-i-shl3", []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x41, 0x03, 0x74, 0x6a, 0x0b}, []uint64{100, 5}, 140},
		// f(b,i) = (i<<2) + b  (shl on the left)
		{"i32-shl-left", []byte{0x00,
			0x20, 0x01, 0x41, 0x02, 0x74, 0x20, 0x00, 0x6a, 0x0b}, []uint64{100, 5}, 120},
		// k=4 (not encodable): falls back, still correct
		{"i32-shl4-fallback", []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x41, 0x04, 0x74, 0x6a, 0x0b}, []uint64{100, 5}, 180},
		// i32 wrap-around: (0xFFFFFFFF<<1)+2 = 0x1_FFFFFFFE+2 mod 2^32 = 0
		{"i32-wrap", []byte{0x00,
			0x41, 0x02, 0x20, 0x00, 0x41, 0x01, 0x74, 0x6a, 0x0b}, []uint64{0xFFFFFFFF}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			params := []wasm.ValType{i32}
			if len(c.args) == 2 {
				params = []wasm.ValType{i32, i32}
			}
			m := mod1(t, params, []wasm.ValType{i32}, c.body)
			if got := uint32(runAmd64u(t, m, c.args...)); got != uint32(c.want) {
				t.Fatalf("%s = %d, want %d", c.name, got, uint32(c.want))
			}
		})
	}

	// i64: f(b,i) = b + (i<<3)
	m := mod1(t, []wasm.ValType{i64, i64}, []wasm.ValType{i64}, []byte{0x00,
		0x20, 0x00, 0x20, 0x01, 0x42, 0x03, 0x86, 0x7c, 0x0b})
	if got := runAmd64u(t, m, 1<<40, 5); got != (1<<40)+40 {
		t.Fatalf("i64 scaled add = %d, want %d", got, uint64(1<<40)+40)
	}
}

// TestExecLazyMergeLocals covers the per-frame merge-state agreement
// (convergeEdgeTo): edges may leave a call-clobbered pinned local in its slot
// across a merge (lsMem target) as long as every edge and the merge agree; a
// stronger edge (register still valid) must not fool the merge, and asymmetric
// call placement must converge correctly in both directions.
func TestExecLazyMergeLocals(t *testing.T) {
	clobber3 := funcDef{nil, []wasm.ValType{i32}, []byte{
		0x01, 0x03, 0x7f,
		0x41, 0x01, 0x21, 0x00,
		0x41, 0x02, 0x21, 0x01,
		0x41, 0x03, 0x21, 0x02,
		0x20, 0x00, 0x20, 0x01, 0x6a, 0x20, 0x02, 0x6a,
		0x0b,
	}}
	run := func(t *testing.T, body []byte, arg uint64, want uint32) {
		t.Helper()
		m := modFuncs(t,
			funcDef{[]wasm.ValType{i32}, []wasm.ValType{i32}, body},
			clobber3,
		)
		if got := uint32(runAmd64u(t, m, arg)); got != want {
			t.Fatalf("f(%d) = %d, want %d", arg, got, want)
		}
	}

	// call in BOTH branches: both edges arrive lsMem; merge stays lazy; the read
	// after the merge must reload from the slot.
	bothCall := []byte{
		0x01, 0x01, 0x7f, // 1 local (idx 1)
		0x41, 0x07, 0x21, 0x01, // l = 7
		0x20, 0x00, // x
		0x04, 0x40, // if
		0x10, 0x01, 0x1a, // call; drop
		0x05,                   // else
		0x41, 0x09, 0x21, 0x01, // l = 9
		0x10, 0x01, 0x1a, // call; drop
		0x0b,
		0x20, 0x01, // l
		0x0b,
	}
	t.Run("both-branches-call", func(t *testing.T) {
		run(t, bothCall, 1, 7)
		run(t, bothCall, 0, 9)
	})

	// call in THEN only: the then edge fixes the target as lsMem; the else edge
	// arrives stronger (register valid) — merge must still read via the slot.
	thenCall := []byte{
		0x01, 0x01, 0x7f,
		0x41, 0x07, 0x21, 0x01,
		0x20, 0x00,
		0x04, 0x40,
		0x10, 0x01, 0x1a, // then: call; drop
		0x05,
		0x41, 0x09, 0x21, 0x01, // else: l = 9 (dirty, stored at the edge)
		0x0b,
		0x20, 0x01,
		0x0b,
	}
	t.Run("then-call-only", func(t *testing.T) {
		run(t, thenCall, 1, 7)
		run(t, thenCall, 0, 9)
	})

	// call in ELSE only: the then edge fixes lsStackReg; the else edge must LOAD
	// to converge.
	elseCall := []byte{
		0x01, 0x01, 0x7f,
		0x41, 0x07, 0x21, 0x01,
		0x20, 0x00,
		0x04, 0x40,
		0x41, 0x09, 0x21, 0x01, // then: l = 9
		0x05,
		0x10, 0x01, 0x1a, // else: call; drop
		0x0b,
		0x20, 0x01,
		0x0b,
	}
	t.Run("else-call-only", func(t *testing.T) {
		run(t, elseCall, 1, 9)
		run(t, elseCall, 0, 7)
	})

	// if WITHOUT else + call in then: the cond-false edge takes the stub path;
	// both outcomes must read the right value.
	noElse := []byte{
		0x01, 0x01, 0x7f,
		0x41, 0x07, 0x21, 0x01,
		0x20, 0x00,
		0x04, 0x40,
		0x41, 0x09, 0x21, 0x01, // l = 9
		0x10, 0x01, 0x1a, // call; drop
		0x0b,
		0x20, 0x01,
		0x0b,
	}
	t.Run("no-else-then-call", func(t *testing.T) {
		run(t, noElse, 1, 9)
		run(t, noElse, 0, 7)
	})

	// conditional RETURN (br_if to the function label) must not disturb the
	// fall-through's local state.
	condRet := []byte{
		0x01, 0x01, 0x7f,
		0x41, 0x07, 0x21, 0x01, // l = 7
		0x10, 0x01, 0x1a, // call; drop (l now slot-only)
		0x41, 0x05, // 5 (result if returning)
		0x20, 0x00, // x
		0x0d, 0x00, // br_if 0 → return 5
		0x1a,       // drop the 5
		0x20, 0x01, // l
		0x0b,
	}
	t.Run("cond-return", func(t *testing.T) {
		run(t, condRet, 1, 5)
		run(t, condRet, 0, 7)
	})
}

// TestExecCallSetFusion covers the `call f; local.set x` result-hint fusion
// (RAX straight into the pinned local) including a later read after another call.
func TestExecCallSetFusion(t *testing.T) {
	// f(x): l = double(x); call clobber3; return l + double(l)
	m := modFuncs(t,
		funcDef{[]wasm.ValType{i32}, []wasm.ValType{i32}, []byte{
			0x01, 0x01, 0x7f, // 1 local (idx 1)
			0x20, 0x00, 0x10, 0x01, 0x21, 0x01, // l = double(x)   ← fused set
			0x10, 0x02, 0x1a, // call clobber3; drop
			0x20, 0x01, 0x20, 0x01, 0x10, 0x01, 0x6a, // l + double(l)
			0x0b,
		}},
		funcDef{[]wasm.ValType{i32}, []wasm.ValType{i32}, []byte{0x00,
			0x20, 0x00, 0x20, 0x00, 0x6a, 0x0b}}, // double
		funcDef{nil, []wasm.ValType{i32}, []byte{
			0x01, 0x03, 0x7f,
			0x41, 0x01, 0x21, 0x00, 0x41, 0x02, 0x21, 0x01, 0x41, 0x03, 0x21, 0x02,
			0x20, 0x00, 0x20, 0x01, 0x6a, 0x20, 0x02, 0x6a, 0x0b}}, // clobber3
	)
	if got := uint32(runAmd64u(t, m, 5)); got != 30 { // l=10; 10+20
		t.Fatalf("fused call+set = %d, want 30", got)
	}
}

// TestExecConstBulkMem covers the unrolled small-constant memory.fill/copy
// paths: chunk tails, overlapping copies (memmove semantics both directions),
// pattern replication for const and dynamic fill values, n=0, and the n>32
// fallback to rep movs/stos.
func TestExecConstBulkMem(t *testing.T) {
	fillBody := func(n byte) []byte { // f(dst, val) { fill(dst, val, n) }
		return []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x41, n,
			0xfc, 0x0b, 0x00,
			0x41, 0x00, 0x0b}
	}
	for _, n := range []byte{0, 1, 3, 5, 8, 13, 16, 21, 32, 33} {
		t.Run(fmt.Sprintf("fill-n%d", n), func(t *testing.T) {
			m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, fillBody(n))
			_, lin, err := runMemAmd64(t, m, nil, 64, 0x5A)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < int(n); i++ {
				if lin[64+i] != 0x5A {
					t.Fatalf("byte %d = %#x, want 0x5A", i, lin[64+i])
				}
			}
			if lin[64+int(n)] == 0x5A {
				t.Fatal("fill overran")
			}
		})
	}
	t.Run("fill-dynamic-val-masks-low-byte", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, fillBody(9))
		_, lin, err := runMemAmd64(t, m, nil, 64, 0x1CD) // only 0xCD may be written
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 9; i++ {
			if lin[64+i] != 0xCD {
				t.Fatalf("byte %d = %#x, want 0xCD", i, lin[64+i])
			}
		}
	})
	t.Run("fill-oob-traps", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, fillBody(16))
		if _, _, err := runMemAmd64(t, m, nil, 65536-8, 1); err == nil {
			t.Fatal("expected OOB trap")
		}
	})

	copyBody := func(n byte) []byte { // f(dst, src) { copy(dst, src, n) }
		return []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x41, n,
			0xfc, 0x0a, 0x00, 0x00,
			0x41, 0x00, 0x0b}
	}
	seq := func(l []byte) {
		for i := 0; i < 64; i++ {
			l[100+i] = byte(i + 1)
		}
	}
	for _, n := range []byte{0, 1, 3, 5, 8, 13, 16, 21, 32, 33} {
		t.Run(fmt.Sprintf("copy-n%d", n), func(t *testing.T) {
			m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, copyBody(n))
			_, lin, err := runMemAmd64(t, m, seq, 300, 100)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < int(n); i++ {
				if lin[300+i] != byte(i+1) {
					t.Fatalf("byte %d = %#x, want %#x", i, lin[300+i], i+1)
				}
			}
		})
	}
	// Overlapping copies must behave like memmove in both directions.
	t.Run("copy-overlap-fwd", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, copyBody(16))
		_, lin, err := runMemAmd64(t, m, seq, 104, 100) // dst > src, overlap 12
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 16; i++ {
			if lin[104+i] != byte(i+1) {
				t.Fatalf("overlap-fwd byte %d = %#x, want %#x", i, lin[104+i], i+1)
			}
		}
	})
	t.Run("copy-overlap-bwd", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, copyBody(16))
		_, lin, err := runMemAmd64(t, m, seq, 100, 104) // dst < src
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 16; i++ {
			if lin[100+i] != byte(i+5) {
				t.Fatalf("overlap-bwd byte %d = %#x, want %#x", i, lin[100+i], i+5)
			}
		}
	})
	t.Run("copy-oob-traps", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, copyBody(16))
		if _, _, err := runMemAmd64(t, m, nil, 65536-8, 0); err == nil {
			t.Fatal("expected OOB trap (dst)")
		}
		if _, _, err := runMemAmd64(t, m, nil, 0, 65536-8); err == nil {
			t.Fatal("expected OOB trap (src)")
		}
	})
}

// TestExecDynamicBulkMem covers the hybrid dynamic memory.copy/fill lowering:
// the small inline chunk-loop path (n < 96) in both overlap directions, the
// large rep path, and the boundary sizes.
func TestExecDynamicBulkMem(t *testing.T) {
	copyBody := []byte{0x00,
		0x20, 0x00, 0x20, 0x01, 0x20, 0x02, // dst, src, n (all dynamic)
		0xfc, 0x0a, 0x00, 0x00,
		0x41, 0x00, 0x0b}
	fillBody := []byte{0x00,
		0x20, 0x00, 0x20, 0x01, 0x20, 0x02,
		0xfc, 0x0b, 0x00,
		0x41, 0x00, 0x0b}
	seq := func(l []byte) {
		for i := 0; i < 256; i++ {
			l[1000+i] = byte(i + 1)
		}
	}
	params := []wasm.ValType{i32, i32, i32}
	for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 63, 95, 96, 97, 200} {
		t.Run(fmt.Sprintf("copy-n%d", n), func(t *testing.T) {
			m := modMem(t, 1, params, []wasm.ValType{i32}, copyBody)
			_, lin, err := runMemAmd64(t, m, seq, 2000, 1000, uint64(n))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < n; i++ {
				if lin[2000+i] != byte(i+1) {
					t.Fatalf("byte %d = %#x, want %#x", i, lin[2000+i], byte(i+1))
				}
			}
			if n < 256 && lin[2000+n] == byte(n+1) {
				t.Fatal("copy overran")
			}
		})
		t.Run(fmt.Sprintf("copy-overlap-fwd-n%d", n), func(t *testing.T) {
			m := modMem(t, 1, params, []wasm.ValType{i32}, copyBody)
			_, lin, err := runMemAmd64(t, m, seq, 1004, 1000, uint64(n)) // dst > src
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < n; i++ {
				if lin[1004+i] != byte(i+1) {
					t.Fatalf("fwd-overlap byte %d = %#x, want %#x", i, lin[1004+i], byte(i+1))
				}
			}
		})
		t.Run(fmt.Sprintf("copy-overlap-bwd-n%d", n), func(t *testing.T) {
			m := modMem(t, 1, params, []wasm.ValType{i32}, copyBody)
			_, lin, err := runMemAmd64(t, m, seq, 1000, 1004, uint64(n)) // dst < src
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < n; i++ {
				if lin[1000+i] != byte(i+5) {
					t.Fatalf("bwd-overlap byte %d = %#x, want %#x", i, lin[1000+i], byte(i+5))
				}
			}
		})
		t.Run(fmt.Sprintf("fill-n%d", n), func(t *testing.T) {
			m := modMem(t, 1, params, []wasm.ValType{i32}, fillBody)
			_, lin, err := runMemAmd64(t, m, nil, 3000, 0x1A7, uint64(n)) // low byte 0xA7
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < n; i++ {
				if lin[3000+i] != 0xA7 {
					t.Fatalf("fill byte %d = %#x, want 0xA7", i, lin[3000+i])
				}
			}
			if lin[3000+n] == 0xA7 {
				t.Fatal("fill overran")
			}
		})
	}
}
