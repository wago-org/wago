//go:build linux && amd64

package x64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

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
