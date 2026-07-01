//go:build linux && amd64

package x64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func f32b(v float32) uint64 { return uint64(math.Float32bits(v)) }
func f64b(v float64) uint64 { return math.Float64bits(v) }

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

// runX64 compiles function 0 with the new x64 backend and runs it through the real
// wago runtime with the given i32 args, returning the first i32 result.
func runX64(t *testing.T, m *wasm.Module, args ...int32) int32 {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("x64 compile: %v", err)
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

// runMemX64 compiles function 0, sets up linear memory via setup, runs it, and
// returns the raw 64-bit result word, a copy of post-run linear memory, and any
// trap error from the call.
func runMemX64(t *testing.T, m *wasm.Module, setup func([]byte), args ...uint64) (uint64, []byte, error) {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("x64 compile: %v", err)
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

// runX64u compiles function 0 and runs it with the given 8-byte-wide args
// (i32/i64), returning the raw 64-bit result word.
func runX64u(t *testing.T, m *wasm.Module, args ...uint64) uint64 {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("x64 compile: %v", err)
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

// TestX64Phase0 proves the new backend end-to-end: it compiles integer const /
// local / ALU expressions and runs them through the real runtime, exercising the
// deferred-tree condense engine and the on-the-fly register allocator.
func TestX64Phase0(t *testing.T) {
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
			if got := runX64(t, m, c.args...); got != c.want {
				t.Fatalf("%s = %d, want %d", c.name, got, c.want)
			}
		})
	}
}

// TestX64HostImportCompile checks that a call to an imported (host) function
// lowers to the log-and-replay sequence without error (end-to-end replay is
// driven by src/wago instantiation).
func TestX64HostImportCompile(t *testing.T) {
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

// TestX64GlobalsCompile checks global.get/set lower without error (end-to-end
// global access is verified at src/wago integration, which populates the runtime
// globals slot-array).
func TestX64GlobalsCompile(t *testing.T) {
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

// TestX64Phase5Floats exercises the f32/f64 ISA end-to-end: arithmetic, sqrt,
// neg, NaN/signed-zero-correct min/max, comparisons, conversions, promote/demote,
// reinterpret, and the trapping float→int truncation.
func TestX64Phase5Floats(t *testing.T) {
	f64 := wasm.F64
	f32 := wasm.F32

	// f64.add
	t.Run("f64.add", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64, f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0xa0, 0x0b})
		got := math.Float64frombits(runX64u(t, m, f64b(1.5), f64b(2.25)))
		if got != 3.75 {
			t.Fatalf("f64.add = %v, want 3.75", got)
		}
	})

	// f32.mul
	t.Run("f32.mul", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f32, f32}, []wasm.ValType{f32}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x94, 0x0b})
		got := math.Float32frombits(uint32(runX64u(t, m, f32b(3), f32b(4))))
		if got != 12 {
			t.Fatalf("f32.mul = %v, want 12", got)
		}
	})

	// f64.sqrt
	t.Run("f64.sqrt", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x9f, 0x0b})
		got := math.Float64frombits(runX64u(t, m, f64b(16)))
		if got != 4 {
			t.Fatalf("f64.sqrt = %v, want 4", got)
		}
	})

	// f64.neg
	t.Run("f64.neg", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x9a, 0x0b})
		got := math.Float64frombits(runX64u(t, m, f64b(3)))
		if got != -3 {
			t.Fatalf("f64.neg = %v, want -3", got)
		}
	})

	// f64.min with NaN → NaN ; f64.max signed zero: max(-0,+0) = +0
	t.Run("f64.min-nan", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64, f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0xa4, 0x0b})
		got := math.Float64frombits(runX64u(t, m, f64b(1), f64b(math.NaN())))
		if !math.IsNaN(got) {
			t.Fatalf("f64.min(1,NaN) = %v, want NaN", got)
		}
	})
	t.Run("f64.max-signed-zero", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64, f64}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0xa5, 0x0b})
		bits := runX64u(t, m, f64b(math.Copysign(0, -1)), f64b(0))
		if bits != 0 { // +0.0 has all-zero bits; -0.0 would be 0x8000...
			t.Fatalf("f64.max(-0,+0) bits = %#x, want +0", bits)
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
			got := uint32(runX64u(t, m, f64b(tc.a), f64b(tc.b)))
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
			got := int32(uint32(runX64u(t, m, f64b(tc.in))))
			if got != tc.want {
				t.Fatalf("trunc(%v) = %d, want %d", tc.in, got, tc.want)
			}
		}
	})

	// f64.convert_i32_s
	t.Run("f64.convert_i32_s", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0xb7, 0x0b})
		got := math.Float64frombits(runX64u(t, m, u64(-5)))
		if got != -5 {
			t.Fatalf("convert = %v, want -5", got)
		}
	})

	// f64.promote_f32 and f32.demote_f64
	t.Run("promote", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f32}, []wasm.ValType{f64}, []byte{0x00,
			0x20, 0x00, 0xbb, 0x0b})
		got := math.Float64frombits(runX64u(t, m, f32b(1.5)))
		if got != 1.5 {
			t.Fatalf("promote = %v, want 1.5", got)
		}
	})

	// reinterpret f64↔i64 (bit-exact)
	t.Run("reinterpret", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{f64}, []wasm.ValType{i64}, []byte{0x00,
			0x20, 0x00, 0xbd, 0x0b}) // i64.reinterpret_f64
		got := runX64u(t, m, f64b(2.5))
		if got != f64b(2.5) {
			t.Fatalf("reinterpret = %#x, want %#x", got, f64b(2.5))
		}
	})

	// trapping trunc: NaN → trap
	t.Run("trunc-nan-trap", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{f64}, []wasm.ValType{i32}, []byte{0x00,
			0x20, 0x00, 0xaa, 0x0b})
		_, _, err := runMemX64(t, m, nil, f64b(math.NaN()))
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
			got := math.Float64frombits(runX64u(t, m, uint64(uint32(tc.c))))
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
		got := math.Float64frombits(runX64u(t, m, f64b(2.5)))
		if got != 5.0 {
			t.Fatalf("f64-call = %v, want 5.0", got)
		}
	})
}

// TestX64Phase4Calls exercises internal (wasm→wasm) direct calls via the wrapper
// ABI: a simple caller/callee, recursion, and callee-trap propagation.
func TestX64Phase4Calls(t *testing.T) {
	// func0(x) = func1(x, 10) + 1 ; func1(a,b) = a + b
	t.Run("direct-call", func(t *testing.T) {
		m := modFuncs(t,
			funcDef{[]wasm.ValType{i32}, []wasm.ValType{i32}, []byte{0x00,
				0x20, 0x00, 0x41, 0x0a, 0x10, 0x01, 0x41, 0x01, 0x6a, 0x0b}},
			funcDef{[]wasm.ValType{i32, i32}, []wasm.ValType{i32}, []byte{0x00,
				0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}},
		)
		if got := runX64(t, m, 5); got != 16 {
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
			if got := runX64(t, m, tc.n); got != tc.want {
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

// TestX64BulkAndSat exercises bulk memory (memory.copy/fill) through the runtime
// and the saturating float→int truncations.
func TestX64BulkAndSat(t *testing.T) {
	// memory.fill: fill n bytes at dst with val
	t.Run("memory.fill", func(t *testing.T) {
		// f(dst, val, n) { memory.fill }
		body := []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x20, 0x02, // dst, val, n
			0xfc, 0x0b, 0x00, // memory.fill, mem 0
			0x41, 0x00, // return 0 (dummy i32)
			0x0b}
		m := modMem(t, 1, []wasm.ValType{i32, i32, i32}, []wasm.ValType{i32}, body)
		_, lin, err := runMemX64(t, m, nil, 16, 0xAB, 8)
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
		_, lin, err := runMemX64(t, m, func(l []byte) {
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

	// trunc_sat: NaN→0, overflow→clamp
	t.Run("i32.trunc_sat_f64_s", func(t *testing.T) {
		m := mod1(t, []wasm.ValType{wasm.F64}, []wasm.ValType{i32}, []byte{0x00,
			0x20, 0x00, 0xfc, 0x02, 0x0b})
		for _, tc := range []struct {
			in   float64
			want int32
		}{{3.9, 3}, {-3.9, -3}, {math.NaN(), 0}, {1e300, 0x7FFFFFFF}, {-1e300, -0x80000000}} {
			got := int32(uint32(runX64u(t, m, f64b(tc.in))))
			if got != tc.want {
				t.Fatalf("trunc_sat(%v) = %d, want %d", tc.in, got, tc.want)
			}
		}
	})
}

// TestX64Phase3Control exercises the control-flow constructs and traps: if/else,
// block+br, loop+br_if, br_table, early return, and unreachable — all through the
// real runtime via the canonical-slot reconciliation model.
func TestX64Phase3Control(t *testing.T) {
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
			if got := runX64(t, m, tc.x); got != tc.want {
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
			if got := runX64(t, m, tc.x); got != tc.want {
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
		if got := runX64(t, m); got != 5 {
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
			if got := runX64(t, m, tc.n); got != tc.want {
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
			if got := runX64(t, m, tc.x); got != tc.want {
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
			if got := runX64(t, m, tc.x); got != tc.want {
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
		if got := runX64(t, m, 0); got != 0 {
			t.Fatalf("lazy-zero false path = %d, want 0", got)
		}
		if got := runX64(t, m, 1); got != 7 {
			t.Fatalf("lazy-zero true path = %d, want 7", got)
		}
	})

	// unreachable traps
	t.Run("unreachable", func(t *testing.T) {
		m := modMem(t, 1, nil, []wasm.ValType{i32}, []byte{0x00, 0x00, 0x0b})
		_, _, err := runMemX64(t, m, nil)
		if err == nil {
			t.Fatal("expected unreachable trap, got nil")
		}
	})
}

// TestX64Phase2Memory exercises linear-memory loads/stores (all widths, signed &
// unsigned, i32/i64), memarg offset folding, memory.size, and the bounds-check
// trap — all through the real runtime's guarded linear memory.
func TestX64Phase2Memory(t *testing.T) {
	// f(ptr,val) { store val at ptr; return load at ptr }  (i32 roundtrip)
	t.Run("i32.store-load", func(t *testing.T) {
		m := modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, []byte{0x00,
			0x20, 0x00, 0x20, 0x01, 0x36, 0x02, 0x00, // i32.store [ptr]=val
			0x20, 0x00, 0x28, 0x02, 0x00, // i32.load [ptr]
			0x0b})
		got, lin, err := runMemX64(t, m, nil, 256, 0x1234ABCD)
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
		got, _, err := runMemX64(t, m, nil, 8, 0x1122334455667788)
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
			got, _, err := runMemX64(t, m, c.set)
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
		got, _, err := runMemX64(t, m, func(l []byte) { binary.LittleEndian.PutUint32(l[12:], 0xCAFEF00D) })
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
		got, _, err := runMemX64(t, m, nil)
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
		got, _, err := runMemX64(t, m, func(l []byte) {
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
		got, _, err := runMemX64(t, m, func(l []byte) {
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
		got, _, err := runMemX64(t, m, func(l []byte) { binary.LittleEndian.PutUint32(l[0:], 5) })
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
		_, _, err := runMemX64(t, m, nil)
		if err == nil {
			t.Fatal("expected out-of-bounds trap, got nil")
		}
	})
}

// TestX64Phase1 exercises the full scalar integer ISA: mul, div/rem (signed &
// unsigned), shifts & rotates (const and variable count), clz/ctz/popcnt, all
// relational compares + eqz, and constant folding — for both i32 and i64.
func TestX64Phase1(t *testing.T) {
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
			[]byte{0x20, 0x00, 0x41, 0x03, 0x6c, 0x0b}, []uint64{5}, 15},
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

		// --- width conversions / sign extension ---
		{"i32.wrap_i64", []wasm.ValType{i64}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0xa7, 0x0b}, []uint64{0xFFFFFFFF_00000005}, 5},
		{"i64.extend_i32_s", []wasm.ValType{i32}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0xac, 0x0b}, []uint64{uint64(uint32(0xFFFFFFFF))}, u64(-1)},
		{"i64.extend_i32_u", []wasm.ValType{i32}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0xad, 0x0b}, []uint64{uint64(uint32(0xFFFFFFFF))}, 0xFFFFFFFF},
		{"i32.extend8_s", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0xc0, 0x0b}, []uint64{0xFF}, uint64(uint32(0xFFFFFFFF))}, // 0xFF -> -1
		{"i32.extend16_s", []wasm.ValType{i32}, []wasm.ValType{i32},
			[]byte{0x20, 0x00, 0xc1, 0x0b}, []uint64{0x8000}, uint64(uint32(0xFFFF8000))},
		{"i64.extend32_s", []wasm.ValType{i64}, []wasm.ValType{i64},
			[]byte{0x20, 0x00, 0xc4, 0x0b}, []uint64{0x80000000}, u64(-0x80000000)},

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
			got := runX64u(t, m, c.args...)
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
