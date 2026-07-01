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

var (
	i32 = wasm.I32
)

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
