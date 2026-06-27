//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// watToModule compiles WAT text to a decoded+validated Module using wat2wasm.
func watToModule(t *testing.T, wat string) *wasm.Module {
	t.Helper()
	w2w, err := exec.LookPath("wat2wasm")
	if err != nil {
		t.Skip("wat2wasm (wabt) not on PATH")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "m.wat")
	out := filepath.Join(dir, "m.wasm")
	if err := os.WriteFile(src, []byte(wat), 0o644); err != nil {
		t.Fatal(err)
	}
	if o, err := exec.Command(w2w, src, "-o", out).CombinedOutput(); err != nil {
		t.Fatalf("wat2wasm: %v\n%s", err, o)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
	return m
}

func TestCompileSkipsOverlongBulkMemoryImmediatesInUnreachableCode(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0xfc, 0x0a, 0x80, 0x00, 0x80, 0x00, 0x0b}))),
	)
	m, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
	if _, err := CompileModule(m); err != nil {
		t.Fatalf("CompileModule: %v", err)
	}
}

// runI32 compiles function 0, executes it on the jit engine with the given i32
// args, and returns the first i32 result.
func runI32(t *testing.T, m *wasm.Module, args ...int32) int32 {
	t.Helper()
	code, err := CompileFunction(m, 0)
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
	mem, entry, err := runtime.MapCode(code)
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
	if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return int32(binary.LittleEndian.Uint32(results))
}

func TestCompileRejectsHugeLocalRunsBeforeExpansion(t *testing.T) {
	m := &wasm.Module{
		Types:     []wasm.FuncType{{}},
		Functions: []uint32{0},
		Code: []wasm.Code{{
			Locals: []wasm.LocalRun{{Count: maxCompiledLocals + 1, Type: wasm.I32}},
			Body:   []byte{0x0b},
		}},
	}
	_, err := CompileModule(m)
	if err == nil || !strings.Contains(err.Error(), "local count") {
		t.Fatalf("CompileModule huge local run error = %v, want local count", err)
	}
}

func TestI32EndToEnd(t *testing.T) {
	cases := []struct {
		name string
		wat  string
		args []int32
		want int32
	}{
		{
			"add",
			`(module (func (export "f") (param i32 i32) (result i32) local.get 0 local.get 1 i32.add))`,
			[]int32{40, 2}, 42,
		},
		{
			"sub_mul",
			`(module (func (export "f") (param i32 i32) (result i32)
				local.get 0 local.get 1 i32.sub i32.const 3 i32.mul))`,
			[]int32{10, 3}, 21, // (10-3)*3
		},
		{
			"const_only",
			`(module (func (export "f") (result i32) i32.const 1234))`,
			nil, 1234,
		},
		{
			"locals_and_tee",
			`(module (func (export "f") (param i32) (result i32) (local i32)
				local.get 0 i32.const 7 i32.add local.tee 1 local.get 1 i32.mul))`,
			[]int32{3}, 100, // (3+7)=10 -> 10*10
		},
		{
			"bitwise_shift",
			`(module (func (export "f") (param i32 i32) (result i32)
				local.get 0 local.get 1 i32.shl i32.const 255 i32.and))`,
			[]int32{1, 4}, 16, // (1<<4)&255
		},
		{
			"cmp_lt_s",
			`(module (func (export "f") (param i32 i32) (result i32) local.get 0 local.get 1 i32.lt_s))`,
			[]int32{-5, 3}, 1,
		},
		{
			"cmp_ge_u",
			`(module (func (export "f") (param i32 i32) (result i32) local.get 0 local.get 1 i32.ge_u))`,
			[]int32{3, 3}, 1,
		},
		{
			"eqz",
			`(module (func (export "f") (param i32) (result i32) local.get 0 i32.eqz))`,
			[]int32{0}, 1,
		},
		{
			"shr_s_negative",
			`(module (func (export "f") (param i32) (result i32) local.get 0 i32.const 1 i32.shr_s))`,
			[]int32{-8}, -4, // arithmetic shift keeps sign
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := watToModule(t, c.wat)
			got := runI32(t, m, c.args...)
			if got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}
