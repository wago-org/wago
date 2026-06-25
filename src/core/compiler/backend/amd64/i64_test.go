//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

func runI64(t *testing.T, m *wasm.Module, args ...int64) int64 {
	t.Helper()
	code, err := CompileFunction(m, 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	for i, a := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], uint64(a))
	}
	if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return int64(binary.LittleEndian.Uint64(results))
}

func TestI64Arith(t *testing.T) {
	cases := []struct {
		name string
		wat  string
		a, b int64
		want int64
	}{
		{"add", `local.get 0 local.get 1 i64.add`, 1 << 40, 1 << 40, 1 << 41},
		{"sub", `local.get 0 local.get 1 i64.sub`, 5, 9, -4},
		{"mul", `local.get 0 local.get 1 i64.mul`, 1 << 20, 1 << 20, 1 << 40},
		{"and", `local.get 0 local.get 1 i64.and`, 0xFF00FF00FF00, 0x0F0F0F0F0F0F, 0x0F000F000F00},
		{"shl", `local.get 0 local.get 1 i64.shl`, 1, 40, 1 << 40},
		{"shr_s", `local.get 0 local.get 1 i64.shr_s`, -(1 << 40), 8, -(1 << 32)},
		{"div_s", `local.get 0 local.get 1 i64.div_s`, -(1 << 40), 1 << 8, -(1 << 32)},
		{"rem_s", `local.get 0 local.get 1 i64.rem_s`, (1 << 40) + 7, 1 << 40, 7},
		{"big_const_add", `local.get 0 i64.const 0x100000000 i64.add`, 5, 0, 0x100000005},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wat := `(module (func (export "f") (param i64 i64) (result i64)` + "\n" + c.wat + "))"
			m := watToModule(t, wat)
			if got := runI64(t, m, c.a, c.b); got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestI64Compare(t *testing.T) {
	// i64 comparisons produce i32; check via an i64-returning extend for the helper.
	cmp := func(op string, a, b int64) int64 {
		wat := `(module (func (export "f") (param i64 i64) (result i64)
			local.get 0 local.get 1 ` + op + ` i64.extend_i32_u))`
		return runI64(t, watToModule(t, wat), a, b)
	}
	if cmp("i64.lt_s", -5, 3) != 1 {
		t.Error("lt_s -5<3")
	}
	if cmp("i64.lt_u", -5, 3) != 0 { // -5 as u64 is huge
		t.Error("lt_u (-5 as u64) < 3 should be 0")
	}
	if cmp("i64.eq", 1<<40, 1<<40) != 1 {
		t.Error("eq")
	}
	if cmp("i64.ge_s", 1<<40, 1<<40) != 1 {
		t.Error("ge_s")
	}
}

func TestI64Conversions(t *testing.T) {
	// i64.extend_i32_s of -1 -> -1 (i64); wrap back -> 0xFFFFFFFF as i32.
	m := watToModule(t, `(module (func (export "f") (param i64) (result i64)
		local.get 0 i32.wrap_i64 i64.extend_i32_s))`)
	if got := runI64(t, m, -1); got != -1 {
		t.Fatalf("wrap then extend_s(-1) = %d, want -1", got)
	}
	// extend_i32_u of a value with high bit set -> zero-extended.
	mu := watToModule(t, `(module (func (export "f") (param i64) (result i64)
		local.get 0 i32.wrap_i64 i64.extend_i32_u))`)
	if got := runI64(t, mu, -1); got != 0xFFFFFFFF {
		t.Fatalf("wrap then extend_u(-1) = %#x, want 0xFFFFFFFF", got)
	}
}

func TestI64Memory(t *testing.T) {
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32 i64) (result i64)
		local.get 0 local.get 1 i64.store
		local.get 0 i64.load))`)
	val := int64(0x0123456789ABCDEF)
	if got := runI64(t, m, 128, val); got != val {
		t.Fatalf("i64 store/load = %#x, want %#x", got, val)
	}
}
